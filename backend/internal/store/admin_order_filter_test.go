package store

import (
	"testing"

	"hstore/backend/internal/db"
)

// 后台订单列表必须能按「是否兑换码订单」筛选。
//
// 兑换码核销单在 orders 表里 source 也是 user、也不写 contact，
// 唯一可靠的区分方式是看有没有兑换码指向它（redeem_codes.order_number）。
// 线上曾经因为后台只查支付单，导致这批订单完全不可见。
func TestAdminOrderFilterByKind(t *testing.T) {
	if _, e := db.Open(); e != nil {
		t.Fatal(e)
	}
	EnsureSelfCategory()
	goodsID, e := CreateSelfProduct(SelfProductInput{GoodsID: 940601, Name: "订单筛选商品", Price: 0.5, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = DeleteSelfProduct(goodsID) })

	gid := goodsID
	CreateCodes(CreateCodesInput{FaceValue: 1, GoodsID: &gid, GoodsCount: 1, Count: 1})
	codes := ListCodes(CodeFilter{GoodsID: gid}, 10, 0)
	if len(codes) != 1 {
		t.Fatalf("应生成 1 条兑换码，实际 %d", len(codes))
	}

	// 兑换码核销单：没有 contact，靠 redeem_codes.order_number 关联。
	const redeemOrder = "HSTESTKIND0001"
	UpsertOrder(UpsertOrderInput{
		OrderNumber: redeemOrder, GoodsID: gid, GoodsName: "兑换码核销单",
		Count: 1, Status: 3, Result: "兑换码取件", Platform: "self", Source: "user",
	})
	FinishCode(codes[0].ID, redeemOrder)

	// 普通余额购买单：有 contact。
	const normalOrder = "HSTESTKIND0002"
	UpsertOrder(UpsertOrderInput{
		OrderNumber: normalOrder, GoodsID: gid, GoodsName: "余额购买单",
		Count: 2, Status: 3, Result: "下单成功", Platform: "self", Source: "user",
		Contact: "buyer@example.com",
	})
	t.Cleanup(func() {
		_, _ = db.Must().Exec("DELETE FROM cards WHERE order_number IN (?,?)", redeemOrder, normalOrder)
		_, _ = db.Must().Exec("DELETE FROM orders WHERE order_number IN (?,?)", redeemOrder, normalOrder)
	})

	// 按兑换码订单筛选：只应命中兑换码单。
	redeemList := ListOrders(OrderFilter{Kind: "redeem", Limit: 1000})
	var hasRedeem, hasNormal bool
	for _, o := range redeemList {
		if o.OrderNumber == redeemOrder {
			hasRedeem = true
		}
		if o.OrderNumber == normalOrder {
			hasNormal = true
		}
	}
	if !hasRedeem {
		t.Fatalf("兑换码订单 %s 应出现在 kind=redeem 结果里", redeemOrder)
	}
	if hasNormal {
		t.Fatalf("普通订单 %s 不应出现在 kind=redeem 结果里", normalOrder)
	}

	// 按非兑换码订单筛选：反过来。
	normalList := ListOrders(OrderFilter{Kind: "normal", Limit: 1000})
	hasRedeem, hasNormal = false, false
	for _, o := range normalList {
		if o.OrderNumber == redeemOrder {
			hasRedeem = true
		}
		if o.OrderNumber == normalOrder {
			hasNormal = true
		}
	}
	if hasRedeem {
		t.Fatalf("兑换码订单 %s 不应出现在 kind=normal 结果里", redeemOrder)
	}
	if !hasNormal {
		t.Fatalf("普通订单 %s 应出现在 kind=normal 结果里", normalOrder)
	}

	// 总数必须和列表口径一致（同一份 buildOrderWhere）。
	if got, want := CountOrders(OrderFilter{Kind: "redeem"}), len(redeemList); got != want {
		t.Fatalf("kind=redeem 总数 %d 与列表长度 %d 不一致", got, want)
	}
}

// 后台订单列表要能按商品、来源筛选，关键词要能命中用户名与联系方式。
func TestAdminOrderFilterByGoodsSourceKeyword(t *testing.T) {
	if _, e := db.Open(); e != nil {
		t.Fatal(e)
	}
	const (
		orderA = "HSTESTFILTER0001"
		orderB = "HSTESTFILTER0002"
	)
	UpsertOrder(UpsertOrderInput{
		OrderNumber: orderA, GoodsID: 940701, GoodsName: "筛选商品甲",
		Count: 1, Status: 3, Result: "完成", Platform: "self", Source: "guest",
		Contact: "guest-a@example.com",
	})
	UpsertOrder(UpsertOrderInput{
		OrderNumber: orderB, GoodsID: 940702, GoodsName: "筛选商品乙",
		Count: 1, Status: 4, Result: "关闭", Platform: "self", Source: "admin",
	})
	t.Cleanup(func() {
		_, _ = db.Must().Exec("DELETE FROM orders WHERE order_number IN (?,?)", orderA, orderB)
	})

	// 按商品
	list := ListOrders(OrderFilter{GoodsID: 940701, Limit: 100})
	if len(list) == 0 || list[0].OrderNumber != orderA {
		t.Fatalf("按商品 940701 应命中 %s，实际 %+v", orderA, list)
	}

	// 按来源
	list = ListOrders(OrderFilter{Source: "guest", Limit: 1000})
	for _, o := range list {
		if o.Source != "guest" {
			t.Fatalf("source=guest 结果混入了 %s", o.Source)
		}
	}
	if CountOrders(OrderFilter{Source: "guest"}) != len(list) {
		t.Fatal("source 筛选的总数与列表不一致")
	}

	// 按状态：closed = 4
	list = ListOrders(OrderFilter{Statuses: []int{4}, Limit: 1000})
	for _, o := range list {
		if o.Status != 4 {
			t.Fatalf("状态筛选混入了 status=%d", o.Status)
		}
	}

	// 关键词命中联系方式
	list = ListOrders(OrderFilter{Keyword: "guest-a@example.com", Limit: 100})
	if len(list) != 1 || list[0].OrderNumber != orderA {
		t.Fatalf("关键词应命中联系方式所在的订单 %s，实际 %+v", orderA, list)
	}

	// 关键词命中订单号
	list = ListOrders(OrderFilter{Keyword: "HSTESTFILTER0002", Limit: 100})
	if len(list) != 1 || list[0].OrderNumber != orderB {
		t.Fatalf("关键词应命中订单号 %s，实际 %+v", orderB, list)
	}
}

// 列表要带回 contact，后台详情才不用再查一次。
func TestListOrdersReturnsContact(t *testing.T) {
	if _, e := db.Open(); e != nil {
		t.Fatal(e)
	}
	const orderNo = "HSTESTCONTACT01"
	UpsertOrder(UpsertOrderInput{
		OrderNumber: orderNo, GoodsID: 940801, GoodsName: "联系方式测试",
		Count: 1, Status: 3, Result: "完成", Platform: "self", Source: "user",
		Contact: "contact@example.com",
	})
	t.Cleanup(func() {
		_, _ = db.Must().Exec("DELETE FROM orders WHERE order_number = ?", orderNo)
	})

	list := ListOrders(OrderFilter{Keyword: orderNo, Limit: 10})
	if len(list) != 1 {
		t.Fatalf("应取到 1 单，实际 %d", len(list))
	}
	if list[0].Contact != "contact@example.com" {
		t.Fatalf("列表应带回联系方式，实际 %q", list[0].Contact)
	}
}
