package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
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
	GoodsID        int               `json:"goodsId"`
	ProductName    string            `json:"productName"`
	GoodsCount     int               `json:"goodsCount"`
	AmountCny      float64           `json:"amountCny"`
	CreatedAt      int64             `json:"createdAt"`
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

func guestProduct(goodsID, count int, discount float64) (*model.Product, *model.GoodsDetail, float64, error) {
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
	if discount <= 0 || discount > 1 {
		discount = 1
	}
	amount := round4(product.FinalPrice * discount * float64(count))
	if amount <= 0 {
		return nil, nil, 0, errors.New("商品价格异常")
	}
	return product, detail, amount, nil
}

// ValidateContact 校验下单必填的联系方式：手机号 / 邮箱 / QQ 均可。
func ValidateContact(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", errors.New("请填写联系方式，便于查询订单")
	}
	if len([]rune(v)) > 64 {
		return "", errors.New("联系方式最多 64 个字符")
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("联系方式包含非法字符")
		}
	}
	return v, nil
}

// ceilToCents 把金额转成「分」，不足 1 分向上取整：0.064 → 7 分。
//
// 不能直接 math.Ceil(amount*100)：0.06*100 在 float64 里是 6.000000000000001，
// 会被误判成有余数、白白多收 1 分。所以先四舍五入，再判断余量是否超过浮点误差。
func ceilToCents(amount float64) int64 {
	raw := amount * 100
	cents := int64(math.Round(raw))
	if raw-float64(cents) > 1e-6 {
		cents++
	}
	return cents
}

// MinUsdtCountForGuest 返回 USDT 直购至少需要买几件，才能凑够 1 USDT。
// 例如单价 ¥6、汇率 7.2 → 需要 2 件（¥12 ≈ 1.67 USDT）。
func MinUsdtCountForGuest(unitPrice, rate float64) int {
	if unitPrice <= 0 || rate <= 0 {
		return 1
	}
	n := int(math.Ceil(rate / unitPrice))
	if n < 1 {
		n = 1
	}
	return n
}

// CreateGuestPurchase 创建直购订单；支付宝使用商品精确金额，不加价。
// user 为空时是真正的游客单（自动建 guest 账号）；登录用户传自己的账号，
// 订单履约后会直接落在该用户名下，同时仍保留游客 token 支付页。
func CreateGuestPurchase(user *model.User, goodsID, count int, attach []map[string]string, channel, chain, contact string) (*model.GuestPurchase, *model.AlipayOrder, *model.CryptoOrder, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != "alipay" && channel != "usdt" {
		return nil, nil, nil, errors.New("请选择支付渠道")
	}
	if user != nil && user.Status != "active" {
		return nil, nil, nil, errors.New("账号已被冻结")
	}
	contact, err := ValidateContact(contact)
	if err != nil {
		return nil, nil, nil, err
	}
	discount := 1.0
	if user != nil {
		discount = store.GetLevelDiscount(user.LevelID)
	}
	product, detail, amount, err := guestProduct(goodsID, count, discount)
	if err != nil {
		return nil, nil, nil, err
	}
	unitPrice := round4(product.FinalPrice * discount)
	if channel == "usdt" {
		// USDT 单笔到账太少无法核对（还要加唯一尾数），自动把数量提到至少 1 USDT。
		cfg := store.GetCryptoConfig()
		minCount := MinUsdtCountForGuest(unitPrice, cfg.Rate)
		if minCount > count {
			maxQ := detail.MaxQuantity
			if maxQ < 1 {
				maxQ = 999999
			}
			if minCount > maxQ {
				return nil, nil, nil, fmt.Errorf(
					"USDT 支付至少需要 %d 件（约 1 USDT），超过单笔限购 %d 件，请改用支付宝", minCount, maxQ)
			}
			if detail.StockCount > 0 && minCount > detail.StockCount {
				return nil, nil, nil, fmt.Errorf("USDT 支付至少需要 %d 件（约 1 USDT），当前库存不足", minCount)
			}
			count = minCount
			amount = round4(product.FinalPrice * discount * float64(count))
		}
	}
	if channel == "alipay" {
		// 支付宝只能支付到「分」，不足 1 分向上取整：0.064 → 0.07
		cents := ceilToCents(amount)
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
	// 支付订单始终挂在临时 guest 账号上：到账结算会给该账号加余额，
	// 真正的订单归属放在 gp.UID，避免登录用户既拿到余额又拿到商品。
	payer, e := store.CreateGuestUser()
	if e != nil {
		return nil, nil, nil, e
	}
	ownerUID := payer.UID
	guestFlag := 1
	if user != nil {
		ownerUID = user.UID
		guestFlag = 0
	}

	var alipayOrder *model.AlipayOrder
	var cryptoOrder *model.CryptoOrder
	switch channel {
	case "alipay":
		alipayOrder, err = CreateGuestAlipayOrder(payer, amount)
	case "usdt":
		cryptoOrder, err = CreateGuestCryptoOrder(payer.UID, amount, chain)
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
		UID:            ownerUID,
		GuestUser:      guestFlag,
		Channel:        channel,
		PaymentOrderID: paymentID,
		GoodsID:        goodsID,
		GoodsName:      goodsName,
		GoodsCount:     count,
		AttachJSON:     string(attachJSON),
		AmountCny:      amount,
		Status:         "waiting",
		OuterNumber:    genOuterNumber(),
		Contact:        contact,
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

// 同一笔游客单同时只跑一个同步任务，避免前端轮询把上游/支付宝查询打成并发风暴。
var (
	guestSyncMu  sync.Mutex
	guestSyncing = map[int]bool{}
)

// syncGuestPurchaseAsync 在后台查支付状态并尝试发货。
//
// 这里必须异步：查支付宝账单、向上游下单都是秒级网络请求，放在 HTTP 请求里
// 会让前端每 4 秒一次的轮询不断堆积，浏览器 6 个连接被占满后整页卡死。
func syncGuestPurchaseAsync(gp *model.GuestPurchase) {
	if gp == nil {
		return
	}
	guestSyncMu.Lock()
	if guestSyncing[gp.ID] {
		guestSyncMu.Unlock()
		return
	}
	guestSyncing[gp.ID] = true
	guestSyncMu.Unlock()

	go func() {
		defer func() {
			guestSyncMu.Lock()
			delete(guestSyncing, gp.ID)
			guestSyncMu.Unlock()
			if r := recover(); r != nil {
				log.Printf("[hstore] 游客订单同步 panic: %v", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		syncGuestPurchase(ctx, gp)
	}()
}

// GetGuestOrder 返回游客订单视图。
// refresh=true 时只在后台触发一次同步，HTTP 请求本身立刻返回当前状态。
func GetGuestOrder(ctx context.Context, token string, refresh bool) (*GuestOrderView, error) {
	store.ExpireAlipayOrders()
	store.ExpireCryptoOrders()
	gp := store.GetGuestPurchaseByToken(token)
	if gp == nil {
		return nil, errors.New("游客订单不存在或凭证已失效")
	}
	if refresh {
		syncGuestPurchaseAsync(gp)
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
		GoodsID: gp.GoodsID, ProductName: gp.GoodsName, GoodsCount: gp.GoodsCount,
		AmountCny: gp.AmountCny, CreatedAt: gp.CreatedAt,
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
	// 款项已经通过支付宝 / USDT 收到，不能再扣余额（游客余额恒为 0，登录用户余额不动）。
	remark := "在线直购"
	source := "user"
	if gp.GuestUser == 1 {
		remark = "游客购买"
		source = "guest"
	}
	res := PlaceOrder(PlaceOrderOptions{
		User:      user,
		GoodsID:   gp.GoodsID,
		Count:     gp.GoodsCount,
		Source:    source,
		Payment:   "external",
		Attach:    attach,
		Remark:    remark,
		OuterNum:  gp.OuterNumber,
		SaleMoney: &gp.AmountCny,
		Contact:   gp.Contact,
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
