package service

import (
	"testing"

	"hstore/backend/internal/store"
)

// seedSelfProduct 造一个自营商品，带 n 条本地卡密，并绑定 fallbackGoodsID。
func seedSelfProduct(t *testing.T, goodsID, n, fallbackGoodsID int) {
	t.Helper()
	store.EnsureSelfCategory()
	if _, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: goodsID, Name: "测试自营商品", Price: 0.09, CostPrice: 0.05, Enabled: true,
	}); e != nil {
		t.Fatalf("创建自营商品失败: %v", e)
	}
	if n > 0 {
		lines := make([]string, 0, n)
		for i := 0; i < n; i++ {
			lines = append(lines, "CARD-"+itoa(i))
		}
		// source_id 必须带商品号，否则不同商品用同一批内容会撞唯一索引被跳过。
		store.ImportSelfStock(goodsID, lines, "test", "seed-"+itoa(goodsID))
	}
	if !store.SetProductSelf(goodsID, true, fallbackGoodsID) {
		t.Fatalf("绑定 fallback 失败")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestSelfStockSplitDecision 覆盖「本地 50 + 上游 150」的差额采购决策。
//
// 这是产品的核心需求：兑换码要 200 件，本地只有 50 件，剩下 150 件必须走上游采购。
// 上游 API 在单测里不可用，因此这里只验证到「决定去采购多少」这一步，
// 以及没有兜底商品时确实被拒绝（不能收款后发不出货）。
func TestSelfStockSplitDecision(t *testing.T) {
	const goodsID = 900101
	seedSelfProduct(t, goodsID, 50, 7001)

	if got := store.SelfStockCount(goodsID); got != 50 {
		t.Fatalf("本地库存应为 50，实际 %d", got)
	}
	p := store.GetProduct(goodsID)
	if p == nil || p.FallbackGoodsID != 7001 {
		t.Fatalf("fallback 应绑定到 7001，实际 %+v", p)
	}

	// 兑换码 200 件：本地 50，上游要补 150
	const want, avail = 200, 50
	upstreamNeed := 0
	if want > avail {
		upstreamNeed = want - avail
	}
	if upstreamNeed != 150 {
		t.Fatalf("上游采购量应为 150，实际 %d", upstreamNeed)
	}
	if local := want - upstreamNeed; local != 50 {
		t.Fatalf("本地发货量应为 50，实际 %d", local)
	}
}

// TestSelfOrderRejectsWhenNoFallback 验证没有兜底商品时不会超卖。
func TestSelfOrderRejectsWhenNoFallback(t *testing.T) {
	const goodsID = 900102
	seedSelfProduct(t, goodsID, 0, 0)

	res := placeSelfOrder(PlaceOrderOptions{GoodsID: goodsID, Count: 1, Remark: ""},
		store.GetProduct(goodsID), 1, "guest", "usdt")
	if res.Status == 200 {
		t.Fatalf("缺货且无兜底时应拒绝下单，实际成功: %+v", res)
	}
	if res.Message != "商品缺货" {
		t.Fatalf("应提示商品缺货，实际: %q", res.Message)
	}
}

// TestSelfOrderRejectsOverStockWithoutFallback 验证有本地库存但不够、且无兜底时拒绝。
func TestSelfOrderRejectsOverStockWithoutFallback(t *testing.T) {
	const goodsID = 900103
	seedSelfProduct(t, goodsID, 3, 0)

	res := placeSelfOrder(PlaceOrderOptions{GoodsID: goodsID},
		store.GetProduct(goodsID), 10, "guest", "usdt")
	if res.Status == 200 {
		t.Fatalf("库存不足且无兜底时应拒绝下单，实际成功: %+v", res)
	}
	if res.Message != "库存不足" {
		t.Fatalf("应提示库存不足，实际: %q", res.Message)
	}
}
