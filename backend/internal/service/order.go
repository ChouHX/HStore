package service

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

// PlaceOrderOptions 是统一下单入参。
type PlaceOrderOptions struct {
	User      *model.User
	GoodsID   int
	Count     int
	Source    string // admin | user | guest
	Payment   string // balance | code | external（已通过支付宝/USDT 收款，不再扣余额）
	Contact   string // 联系方式，游客必填，用于订单查询
	Attach    []map[string]string
	Remark    string
	OuterNum  string
	SaleMoney *float64 // 游客直购等场景固定成交金额，避免支付后改价
}

// PlaceOrderResult 是下单结果。
type PlaceOrderResult struct {
	OK      bool
	Order   *model.LocalOrder
	Message string
	Status  int
}

func genOuterNumber() string {
	return fmt.Sprintf("FAKA%d%04d", time.Now().UnixMilli(), rand.Intn(9000)+1000)
}

// PlaceOrder 校验商品 → 计算价格 →（可选）扣余额 → 调上游下单 → 落库订单/卡券。
func PlaceOrder(opts PlaceOrderOptions) PlaceOrderResult {
	user := opts.User
	if user == nil {
		return PlaceOrderResult{Message: "请先登录", Status: 401}
	}
	count := opts.Count
	if count < 1 {
		count = 1
	}
	source := opts.Source
	if source == "" {
		source = "user"
	}
	payment := opts.Payment
	if payment == "" {
		payment = "balance"
	}
	if opts.GoodsID == 0 {
		return PlaceOrderResult{Message: "缺少商品编号", Status: 400}
	}

	product := store.GetProduct(opts.GoodsID)
	if product == nil {
		return PlaceOrderResult{Message: "商品不存在", Status: 400}
	}
	if source == "user" && product.Enabled != 1 {
		return PlaceOrderResult{Message: "商品已下架", Status: 400}
	}
	if (source == "user" || source == "guest") && user.Status != "active" {
		return PlaceOrderResult{Message: "账号已被冻结", Status: 400}
	}

	detail, e := upstream.GetDetail(opts.GoodsID)
	if e != nil {
		msg := "获取商品详情失败"
		if e != nil {
			msg = e.Error()
		}
		return PlaceOrderResult{Message: msg, Status: 500}
	}
	if detail.Status != 1 {
		return PlaceOrderResult{Message: "商品当前不可售", Status: 400}
	}
	minQ, maxQ := detail.MinQuantity, detail.MaxQuantity
	if minQ < 1 {
		minQ = 1
	}
	if maxQ < 1 {
		maxQ = 999999
	}
	if count < minQ || count > maxQ {
		return PlaceOrderResult{
			Message: fmt.Sprintf("购买数量需在 %d ~ %d 之间", minQ, maxQ),
			Status:  400,
		}
	}
	if detail.StockCount > 0 && count > detail.StockCount {
		return PlaceOrderResult{Message: "库存不足", Status: 400}
	}

	discount := 1.0
	if source == "user" || source == "guest" {
		discount = store.GetLevelDiscount(user.LevelID)
	}
	unitPrice := round4(product.FinalPrice * discount)
	saleMoney := round4(unitPrice * float64(count))
	if opts.SaleMoney != nil {
		saleMoney = round4(*opts.SaleMoney)
	}
	if payment == "code" {
		saleMoney = 0
	}
	costPrice := detail.SalesPrice
	if costPrice == 0 {
		costPrice = product.CostPrice
	}
	costMoney := round4(costPrice * float64(count))

	deducted := false
	if payment == "balance" && (source == "user" || source == "guest") {
		ok, msg, _, _ := store.Consume(user.UID, saleMoney, "")
		if !ok {
			return PlaceOrderResult{Message: msg, Status: 400}
		}
		deducted = true
	}

	outerNumber := opts.OuterNum
	if outerNumber == "" {
		outerNumber = genOuterNumber()
	}

	result, e := upstream.CreateOrder(upstream.CreateOrderParams{
		GoodsID: opts.GoodsID, Count: count, OuterNumber: outerNumber,
		SafePrice: costMoney, Attach: opts.Attach,
	})
	if e != nil {
		if deducted {
			store.RefundConsume(user.UID, saleMoney, "", "上游下单失败退回")
		}
		msg := "下单失败"
		if e != nil {
			msg = e.Error()
		}
		return PlaceOrderResult{Message: msg, Status: 500}
	}

	status := 1
	if len(result.Cards) > 0 {
		status = 3
	}
	remark := opts.Remark
	if remark == "" {
		remark = "下单成功"
	}
	store.UpsertOrder(store.UpsertOrderInput{
		OrderNumber: result.OrderNumber,
		OuterNumber: outerNumber,
		GoodsID:     opts.GoodsID,
		GoodsName:   detail.Name,
		Count:       count,
		Money:       saleMoney,
		Status:      status,
		Result:      remark,
		Platform:    product.Platform,
		UserUID:     &user.UID,
		SaleMoney:   saleMoney,
		CostMoney:   costMoney,
		Source:      source,
		Contact:     opts.Contact,
	})
	if len(result.Cards) > 0 {
		store.UpsertCards(result.OrderNumber, result.Cards)
	}

	// 结算：只有真实扣款的自助订单才累计消费并触发自动升级
	if payment == "balance" && (source == "user" || source == "guest") && len(result.Cards) > 0 {
		store.SettleConsumption(user.UID, saleMoney, result.OrderNumber)
		store.MarkSettled(result.OrderNumber)
	}

	return PlaceOrderResult{OK: true, Order: store.GetOrder(result.OrderNumber), Status: 200}
}

func round4(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 4, 64), 64)
	return f
}
