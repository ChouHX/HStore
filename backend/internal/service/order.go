package service

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
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
	// 兑换码核销不受上下架影响：迁移过来的自营商品默认下架，只用来兑码
	if source == "user" && product.Enabled != 1 && payment != "code" {
		return PlaceOrderResult{Message: "商品已下架", Status: 400}
	}
	if (source == "user" || source == "guest") && user.Status != "active" {
		return PlaceOrderResult{Message: "账号已被冻结", Status: 400}
	}
	if product.Fulfillment == "self" {
		return placeSelfOrder(opts, product, count, source, payment)
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

	// 订单号由系统自己生成，与上游（天启）单号完全独立：
	// 用户只看到 HS 开头的本地单号，上游单号存库仅供管理员排查。
	orderNumber := store.NewSystemOrderNo()
	status := 1
	if len(result.Cards) > 0 {
		status = 3
	}
	remark := opts.Remark
	if remark == "" {
		remark = "下单成功"
	}
	store.UpsertOrder(store.UpsertOrderInput{
		OrderNumber:     orderNumber,
		UpstreamOrderNo: result.OrderNumber,
		OuterNumber:     outerNumber,
		GoodsID:         opts.GoodsID,
		GoodsName:       detail.Name,
		Count:           count,
		Money:           saleMoney,
		Status:          status,
		Result:          remark,
		Platform:        product.Platform,
		UserUID:         &user.UID,
		SaleMoney:       saleMoney,
		CostMoney:       costMoney,
		Source:          source,
		Contact:         opts.Contact,
	})
	if len(result.Cards) > 0 {
		store.UpsertCards(orderNumber, result.Cards)
	}

	// 结算：只有真实扣款的自助订单才累计消费并触发自动升级
	if payment == "balance" && (source == "user" || source == "guest") && len(result.Cards) > 0 {
		store.SettleConsumption(user.UID, saleMoney, orderNumber)
		store.MarkSettled(orderNumber)
	}

	return PlaceOrderResult{OK: true, Order: store.GetOrder(orderNumber), Status: 200}
}

// placeSelfOrder 处理自营商品：优先发本地库存；库存不足时（仅兑换码场景）
// 可选用 fallback_goods_id 向上游采购差额，最终合并成一个订单。
func placeSelfOrder(opts PlaceOrderOptions, product *model.Product, count int, source, payment string) PlaceOrderResult {
	user := opts.User
	if count < 1 {
		count = 1
	}
	if count > 999999 {
		return PlaceOrderResult{Message: "购买数量过大", Status: 400}
	}
	available := store.SelfStockCount(opts.GoodsID)
	isCode := payment == "code"

	// 兑换码允许「本地 + 上游采购」补齐；直接购买只认本地库存。
	if !isCode && count > available {
		return PlaceOrderResult{Message: "库存不足", Status: 400}
	}
	fallbackID := product.FallbackGoodsID
	upstreamNeed := 0
	if isCode && count > available {
		if fallbackID <= 0 {
			notifySelfStockShortage(product, count, available)
			return PlaceOrderResult{Message: "自营库存不足，请联系管理员补货", Status: 400}
		}
		upstreamNeed = count - available
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
	if isCode {
		saleMoney = 0
	}

	// 自营部分的成本按商品成本价；上游差额按兜底商品成本价。
	localCount := count - upstreamNeed
	costMoney := round4(product.CostPrice * float64(localCount))

	orderNumber := store.NewSystemOrderNo()
	outerNumber := opts.OuterNum
	if outerNumber == "" {
		outerNumber = genOuterNumber()
	}

	var reserved []model.SelfStock
	if localCount > 0 {
		cards, e := store.ReserveSelfStock(opts.GoodsID, localCount, orderNumber)
		if e != nil {
			return PlaceOrderResult{Message: e.Error(), Status: 400}
		}
		reserved = cards
	}

	deducted := false
	if payment == "balance" && (source == "user" || source == "guest") {
		ok, msg, _, _ := store.Consume(user.UID, saleMoney, "")
		if !ok {
			store.ReleaseSelfStock(orderNumber)
			return PlaceOrderResult{Message: msg, Status: 400}
		}
		deducted = true
	}

	// card_id 是 cards 表主键，自营卡密必须逐条生成唯一 ID，
	// 否则 ON CONFLICT 会把多条压缩成一条。
	allCards := make([]model.Card, 0, count)
	for _, it := range reserved {
		allCards = append(allCards, model.Card{CardID: fmt.Sprintf("SELF-%d", it.ID), CardNo: it.Content, CardPwd: ""})
	}

	upstreamOrderNo := ""
	if upstreamNeed > 0 {
		detail, e := upstream.GetDetail(fallbackID)
		if e != nil {
			store.ReleaseSelfStock(orderNumber)
			if deducted {
				store.RefundConsume(user.UID, saleMoney, "", "兜底商品信息获取失败退回")
			}
			return PlaceOrderResult{Message: "获取兜底商品失败：" + e.Error(), Status: 500}
		}
		if detail.Status != 1 {
			store.ReleaseSelfStock(orderNumber)
			if deducted {
				store.RefundConsume(user.UID, saleMoney, "", "兜底商品不可售退回")
			}
			return PlaceOrderResult{Message: "兜底商品当前不可售", Status: 400}
		}
		if detail.StockCount > 0 && upstreamNeed > detail.StockCount {
			store.ReleaseSelfStock(orderNumber)
			if deducted {
				store.RefundConsume(user.UID, saleMoney, "", "兜底商品库存不足退回")
			}
			return PlaceOrderResult{Message: "兜底商品库存不足", Status: 400}
		}
		res, e := upstream.CreateOrder(upstream.CreateOrderParams{
			GoodsID: fallbackID, Count: upstreamNeed, OuterNumber: outerNumber,
			SafePrice: round4(detail.SalesPrice * float64(upstreamNeed)), Attach: opts.Attach,
		})
		if e != nil {
			store.ReleaseSelfStock(orderNumber)
			if deducted {
				store.RefundConsume(user.UID, saleMoney, "", "上游采购失败退回")
			}
			return PlaceOrderResult{Message: "上游采购失败：" + e.Error(), Status: 500}
		}
		upstreamOrderNo = res.OrderNumber
		allCards = append(allCards, res.Cards...)
		costMoney = round4(costMoney + detail.SalesPrice*float64(upstreamNeed))
	}

	status := 1
	if len(allCards) >= count {
		status = 3
	}
	remark := opts.Remark
	if remark == "" {
		if upstreamNeed > 0 {
			remark = fmt.Sprintf("自营 %d 件 + 上游采购 %d 件", localCount, upstreamNeed)
		} else {
			remark = "自营发货"
		}
	}
	store.UpsertOrder(store.UpsertOrderInput{
		OrderNumber:     orderNumber,
		UpstreamOrderNo: upstreamOrderNo,
		OuterNumber:     outerNumber,
		GoodsID:         opts.GoodsID,
		GoodsName:       productDisplayName(product),
		Count:           count,
		Money:           saleMoney,
		Status:          status,
		Result:          remark,
		Platform:        product.Platform,
		UserUID:         &user.UID,
		SaleMoney:       saleMoney,
		CostMoney:       costMoney,
		Source:          source,
		Contact:         opts.Contact,
	})
	if len(allCards) > 0 {
		store.UpsertCards(orderNumber, allCards)
	}
	store.FinishSelfStock(orderNumber)

	if payment == "balance" && (source == "user" || source == "guest") && len(allCards) > 0 {
		store.SettleConsumption(user.UID, saleMoney, orderNumber)
		store.MarkSettled(orderNumber)
	}

	return PlaceOrderResult{OK: true, Order: store.GetOrder(orderNumber), Status: 200}
}

func productDisplayName(p *model.Product) string {
	if p.CustomName != nil && strings.TrimSpace(*p.CustomName) != "" {
		return strings.TrimSpace(*p.CustomName)
	}
	return p.Name
}

func round4(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 4, 64), 64)
	return f
}
