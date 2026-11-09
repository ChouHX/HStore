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

// TestSelfStockSeparatedFromUpstream 验证自营商品完全依赖本地库存。
//
// 兜底已下线：即使数据库里还残留旧的 fallback_goods_id（历史数据），
// 需求超过本地库存时也必须直接拒绝，不会再向上游采购补齐。
func TestSelfStockSeparatedFromUpstream(t *testing.T) {
	const goodsID = 900101
	seedSelfProduct(t, goodsID, 50, 7001)

	if got := store.SelfStockCount(goodsID); got != 50 {
		t.Fatalf("本地库存应为 50，实际 %d", got)
	}

	// 需求 200 > 本地 50：必须拒绝，不能靠残留的兜底配置去采购。
	res := placeSelfOrder(PlaceOrderOptions{GoodsID: goodsID},
		store.GetProduct(goodsID), 200, "guest", "usdt")
	if res.Status == 200 {
		t.Fatalf("超出本地库存时应拒绝下单，实际成功: %+v", res)
	}
	if res.Message != "库存不足" {
		t.Fatalf("应提示库存不足，实际: %q", res.Message)
	}

	// 本地库存够时正常出货（用兑换码付款，不需要真实余额）。
	user, e := store.CreateGuestUser()
	if e != nil {
		t.Fatalf("创建用户失败: %v", e)
	}
	res = placeSelfOrder(PlaceOrderOptions{
		User: user, GoodsID: goodsID, Count: 2, Source: "user", Payment: "code",
	}, store.GetProduct(goodsID), 2, "user", "code")
	if !res.OK {
		t.Fatalf("库存充足时下单应成功，实际: %+v", res)
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
