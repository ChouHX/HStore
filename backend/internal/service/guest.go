package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

// GuestOrderResult 是公开给游客的脱敏订单信息，不包含成本、余额等内部字段。
type GuestOrderResult struct {
	OrderNumber string       `json:"orderNumber"`
	GoodsName   string       `json:"goodsName"`
	Count       int          `json:"count"`
	Status      int          `json:"status"`
	Result      string       `json:"result"`
	CreatedAt   int64        `json:"createdAt"`
	Cards       []model.Card `json:"cards"`
}

// GuestOrderView 是游客购买订单的公开视图。token 即访问凭证。
type GuestOrderView struct {
	Token          string            `json:"token"`
	Status         string            `json:"status"` // waiting / paid / fulfilling / fulfilled / failed / expired / closed
	Channel        string            `json:"channel"`
	ProductName    string            `json:"productName"`
	GoodsCount     int               `json:"goodsCount"`
	AmountCny      float64           `json:"amountCny"`
	PaymentStatus  string            `json:"paymentStatus"`
	ExpiresAt      int64             `json:"expiresAt"`
	PayableCents   int64             `json:"payableCents"`
	CollectionCode string            `json:"collectionCode"`
	Address        string            `json:"address"`
	UsdtAmount     string            `json:"usdtAmount"`
	Chain          string            `json:"chain"`
	ChainLabel     string            `json:"chainLabel"`
	Rate           float64           `json:"rate"`
	CreditCny      float64           `json:"creditCny"`
	Order          *GuestOrderResult `json:"order"`
	Error          string            `json:"error,omitempty"`
}

func newGuestToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("guest-%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func guestProduct(goodsID, count int) (*model.Product, *model.GoodsDetail, float64, error) {
	if goodsID <= 0 {
		return nil, nil, 0, errors.New("缺少商品编号")
	}
	if count < 1 {
		count = 1
	}
	product := store.GetProduct(goodsID)
	if product == nil {
		return nil, nil, 0, errors.New("商品不存在")
	}
	if product.Enabled != 1 {
		return nil, nil, 0, errors.New("商品已下架")
	}
	detail, err := upstream.GetDetail(goodsID)
	if err != nil {
		return nil, nil, 0, err
	}
	if detail.Status != 1 {
		return nil, nil, 0, errors.New("商品当前不可售")
	}
	minQ, maxQ := detail.MinQuantity, detail.MaxQuantity
	if minQ < 1 {
		minQ = 1
	}
	if maxQ < 1 {
		maxQ = 999999
	}
	if count < minQ || count > maxQ {
		return nil, nil, 0, fmt.Errorf("购买数量需在 %d ~ %d 之间", minQ, maxQ)
	}
	if detail.StockCount > 0 && count > detail.StockCount {
		return nil, nil, 0, errors.New("库存不足")
	}
	amount := round4(product.FinalPrice * float64(count))
	if amount <= 0 {
		return nil, nil, 0, errors.New("商品价格异常")
	}
	return product, detail, amount, nil
}

// CreateGuestPurchase 创建游客直购订单；支付宝使用商品精确金额，不加价。
func CreateGuestPurchase(goodsID, count int, attach []map[string]string, channel, chain string) (*model.GuestPurchase, *model.AlipayOrder, *model.CryptoOrder, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != "alipay" && channel != "usdt" {
		return nil, nil, nil, errors.New("请选择支付渠道")
	}
	product, detail, amount, err := guestProduct(goodsID, count)
	if err != nil {
		return nil, nil, nil, err
	}
	if channel == "alipay" {
		// 支付宝只能支付到分；按四舍五入到分，不再额外加价。
		cents := int64(math.Round(amount * 100))
		if cents < 1 {
			return nil, nil, nil, errors.New("商品金额过低，暂不支持支付宝支付")
		}
		amount = float64(cents) / 100
	}
	goodsName := detail.Name
	if product.CustomName != nil && strings.TrimSpace(*product.CustomName) != "" {
		goodsName = strings.TrimSpace(*product.CustomName)
	}
	if count < 1 {
		count = 1
	}
	guestUser, err := store.CreateGuestUser()
	if err != nil {
		return nil, nil, nil, err
	}

	var alipayOrder *model.AlipayOrder
	var cryptoOrder *model.CryptoOrder
	switch channel {
	case "alipay":
		alipayOrder, err = CreateGuestAlipayOrder(guestUser, amount)
	case "usdt":
		cryptoOrder, err = CreateGuestCryptoOrder(guestUser.UID, amount, chain)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	paymentID := 0
	if alipayOrder != nil {
		paymentID = alipayOrder.ID
	}
	if cryptoOrder != nil {
		paymentID = cryptoOrder.ID
	}
	attachJSON, _ := json.Marshal(attach)
	token := newGuestToken()
	gp := model.GuestPurchase{
		Token:          token,
		UID:            guestUser.UID,
		GuestUser:      1,
		Channel:        channel,
		PaymentOrderID: paymentID,
		GoodsID:        goodsID,
		GoodsName:      goodsName,
		GoodsCount:     count,
		AttachJSON:     string(attachJSON),
		AmountCny:      amount,
		Status:         "waiting",
		OuterNumber:    genOuterNumber(),
	}
	id, err := store.InsertGuestPurchase(gp)
	if err != nil {
		if alipayOrder != nil {
			_, _ = store.CloseAlipayOrder(alipayOrder.ID)
		}
		if cryptoOrder != nil {
			store.CancelCryptoOrder(cryptoOrder.ID)
		}
		return nil, nil, nil, err
	}
	gp = *store.GetGuestPurchase(id)
	return &gp, alipayOrder, cryptoOrder, nil
}

// GetGuestOrder 返回游客订单视图；refresh=true 时会立即查一次支付状态并尝试发货。
func GetGuestOrder(ctx context.Context, token string, refresh bool) (*GuestOrderView, error) {
	store.ExpireAlipayOrders()
	store.ExpireCryptoOrders()
	gp := store.GetGuestPurchaseByToken(token)
	if gp == nil {
		return nil, errors.New("游客订单不存在或凭证已失效")
	}
	if refresh {
		syncGuestPurchase(ctx, gp)
		gp = store.GetGuestPurchase(gp.ID)
	}
	if gp == nil {
		return nil, errors.New("游客订单不存在")
	}
	return guestOrderView(gp)
}

func guestOrderView(gp *model.GuestPurchase) (*GuestOrderView, error) {
	view := &GuestOrderView{
		Token: gp.Token, Status: gp.Status, Channel: gp.Channel,
		ProductName: gp.GoodsName, GoodsCount: gp.GoodsCount, AmountCny: gp.AmountCny,
		Error: gp.LastError,
	}
	if gp.Status == "fulfilled" {
		view.Status = "fulfilled"
	}
	if gp.UpstreamOrderNo != "" {
		if order := store.GetOrder(gp.UpstreamOrderNo); order != nil {
			view.Order = &GuestOrderResult{
				OrderNumber: order.OrderNumber, GoodsName: order.GoodsName, Count: order.Count,
				Status: order.Status, Result: order.Result, CreatedAt: order.CreatedAt, Cards: order.Cards,
			}
		}
	}

	switch gp.Channel {
	case "alipay":
		order := store.GetAlipayOrder(gp.PaymentOrderID)
		if order == nil {
			return nil, errors.New("支付订单不存在")
		}
		view.PaymentStatus = order.Status
		view.ExpiresAt = order.ExpiresAt
		view.PayableCents = order.PayableCents
		view.CollectionCode = order.CollectionCode
		if gp.Status == "waiting" {
			view.Status = order.Status
		}
	case "usdt":
		order := store.GetCryptoOrder(gp.PaymentOrderID)
		if order == nil {
			return nil, errors.New("支付订单不存在")
		}
		view.PaymentStatus = order.Status
		view.ExpiresAt = order.ExpiresAt
		view.Address = order.Address
		view.UsdtAmount = order.UsdtAmount
		view.Chain = order.Chain
		view.ChainLabel = order.ChainLabel
		view.Rate = order.Rate
		view.CreditCny = order.CreditCny
		if order.Credited != nil {
			view.CreditCny = *order.Credited
		}
		if gp.Status == "waiting" {
			view.Status = order.Status
		}
	}
	return view, nil
}

func syncGuestPurchase(ctx context.Context, gp *model.GuestPurchase) {
	if gp == nil || gp.Status == "fulfilled" || gp.Status == "fulfilling" {
		return
	}
	if status := terminalGuestPaymentStatus(gp); status != "" {
		store.MarkGuestPurchaseExpired(gp.ID, status)
		return
	}
	switch gp.Channel {
	case "alipay":
		order := store.GetAlipayOrder(gp.PaymentOrderID)
		if order != nil && order.PaymentStatus == "UNPAID" && order.CheckoutStatus == "OPEN" {
			_, _ = QuickCheckAlipayOrder(ctx, gp.PaymentOrderID)
		}
	case "usdt":
		order := store.GetCryptoOrder(gp.PaymentOrderID)
		if order != nil && order.Status == "waiting" {
			CheckCryptoOrder(gp.PaymentOrderID)
		}
	}
	if _, paid := guestPaymentPaid(gp); paid {
		_ = FulfillGuestPurchase(gp.ID)
	}
}

func guestPaymentPaid(gp *model.GuestPurchase) (*int64, bool) {
	if gp == nil {
		return nil, false
	}
	if gp.Channel == "alipay" {
		order := store.GetAlipayOrder(gp.PaymentOrderID)
		if order != nil && order.PaymentStatus == "CONFIRMED" {
			return order.PaidAt, true
		}
	}
	if gp.Channel == "usdt" {
		order := store.GetCryptoOrder(gp.PaymentOrderID)
		if order != nil && order.Status == "paid" {
			return order.PaidAt, true
		}
	}
	return nil, false
}

// FulfillGuestPurchase 在游客支付到账后向上游下单并发货。使用 outer_number 保证重试幂等。
func FulfillGuestPurchase(id int) error {
	gp := store.GetGuestPurchase(id)
	if gp == nil || gp.Status == "fulfilled" {
		return nil
	}
	if gp.Status == "failed" && time.Since(time.UnixMilli(gp.UpdatedAt)) < 30*time.Second {
		return nil
	}
	paidAt, paid := guestPaymentPaid(gp)
	if !paid {
		return nil
	}
	if !store.ClaimGuestPurchase(id) {
		return nil
	}
	user := store.GetUser(gp.UID)
	if user == nil {
		err := errors.New("游客账户不存在")
		store.MarkGuestPurchaseFailed(id, err.Error())
		return err
	}
	var attach []map[string]string
	_ = json.Unmarshal([]byte(gp.AttachJSON), &attach)
	res := PlaceOrder(PlaceOrderOptions{
		User:      user,
		GoodsID:   gp.GoodsID,
		Count:     gp.GoodsCount,
		Source:    "guest",
		Payment:   "balance",
		Attach:    attach,
		Remark:    "游客购买",
		OuterNum:  gp.OuterNumber,
		SaleMoney: &gp.AmountCny,
	})
	if !res.OK {
		store.MarkGuestPurchaseFailed(id, res.Message)
		return errors.New(res.Message)
	}
	store.MarkGuestPurchaseFulfilled(id, res.Order.OrderNumber, paidAt)
	return nil
}

// FulfillGuestPurchaseByPayment 在支付结算后立即触发履约，降低游客等待时间。
func FulfillGuestPurchaseByPayment(channel string, paymentOrderID int) error {
	gp := store.GetGuestPurchaseByPayment(channel, paymentOrderID)
	if gp == nil {
		return nil
	}
	return FulfillGuestPurchase(gp.ID)
}

// terminalGuestPaymentStatus 返回已结束且未支付的支付状态。
func terminalGuestPaymentStatus(gp *model.GuestPurchase) string {
	if gp == nil {
		return ""
	}
	if gp.Channel == "alipay" {
		order := store.GetAlipayOrder(gp.PaymentOrderID)
		if order != nil && order.PaymentStatus == "UNPAID" && order.CheckoutStatus != "OPEN" {
			return strings.ToLower(order.CheckoutStatus)
		}
	}
	if gp.Channel == "usdt" {
		order := store.GetCryptoOrder(gp.PaymentOrderID)
		if order != nil && (order.Status == "expired" || order.Status == "cancelled") {
			return order.Status
		}
	}
	return ""
}

// FulfillPendingGuestPurchases 由后台轮询调用，处理已到账但尚未履约的游客订单。
func FulfillPendingGuestPurchases(limit int) {
	for _, gp := range store.ListPendingGuestPurchases(limit) {
		if _, paid := guestPaymentPaid(&gp); paid {
			_ = FulfillGuestPurchase(gp.ID)
			continue
		}
		if status := terminalGuestPaymentStatus(&gp); status != "" {
			store.MarkGuestPurchaseExpired(gp.ID, status)
		}
	}
}
