package service

import (
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

// seedUpstreamProduct 造一个上游商品，final_price 即其售价。
func seedUpstreamProduct(t *testing.T, goodsID int, price float64) {
	t.Helper()
	if _, e := db.Must().Exec(`INSERT INTO products(
		goods_id, name, face_value, sales_price, cost_price, final_price, goods_type,
		warranty_days, status, multiple, is_repeat, is_ref_order, is_ref_money, sku_type,
		img_url, dir_ids, platform, updated_at, stock_count)
		VALUES(?,?,?,?,?,?,1,0,1,1,0,0,0,0,'','[]','microsoft',0,100)`,
		goodsID, "上游商品", price, price, price, price); e != nil {
		t.Fatalf("造上游商品失败: %v", e)
	}
}

func priceOf(t *testing.T, goodsID int) float64 {
	t.Helper()
	var p float64
	if e := db.Must().QueryRow("SELECT final_price FROM products WHERE goods_id=?", goodsID).Scan(&p); e != nil {
		t.Fatalf("读取售价失败: %v", e)
	}
	return p
}

// 未绑定兜底的自营商品必须自定义价格，价格为 0 / 负数都要拒绝。
func TestSelfProductRequiresPriceWithoutFallback(t *testing.T) {
	store.EnsureSelfCategory()

	_, _ = db.Must().Exec("DELETE FROM products WHERE goods_id=910001")
	_, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: 910001, Name: "未绑定的自营商品", Price: 0, Enabled: true,
	})
	if e == nil {
		t.Fatal("未绑定兜底且售价为 0，应当报错")
	}
}

// 绑定了兜底的自营商品，售价直接跟随兜底商品，传入的自定义价被忽略。
func TestSelfProductPriceFollowsFallback(t *testing.T) {
	store.EnsureSelfCategory()
	_, _ = db.Must().Exec("DELETE FROM products WHERE goods_id IN (820001,910002)")
	seedUpstreamProduct(t, 820001, 0.032)

	goodsID, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: 910002, Name: "绑定兜底的自营商品", Price: 9.99,
		FallbackGoodsID: 820001, Enabled: true,
	})
	if e != nil {
		t.Fatalf("创建自营商品失败: %v", e)
	}
	if got := priceOf(t, goodsID); got != 0.032 {
		t.Fatalf("售价应跟随兜底的 0.032，实际 %v", got)
	}

	// 上游调价后，同步回填要跟着变。
	if _, e := db.Must().Exec("UPDATE products SET final_price=0.05 WHERE goods_id=?", 820001); e != nil {
		t.Fatalf("改上游价失败: %v", e)
	}
	if n := upstream.SyncSelfFallbackPrices(); n == 0 {
		t.Fatal("SyncSelfFallbackPrices 应当更新绑定商品的价格")
	}
	if got := priceOf(t, goodsID); got != 0.05 {
		t.Fatalf("上游调价后售价应变为 0.05，实际 %v", got)
	}
}

// 未绑定兜底的自营商品价格不受上游同步影响。
func TestSelfProductPriceIndependentWithoutFallback(t *testing.T) {
	store.EnsureSelfCategory()

	_, _ = db.Must().Exec("DELETE FROM products WHERE goods_id=910003")
	goodsID, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: 910003, Name: "纯自营商品", Price: 1.23, Enabled: true,
	})
	if e != nil {
		t.Fatalf("创建自营商品失败: %v", e)
	}
	upstream.SyncSelfFallbackPrices()
	if got := priceOf(t, goodsID); got != 1.23 {
		t.Fatalf("未绑定兜底时售价应保持 1.23，实际 %v", got)
	}
}

// 兜底指向自身时（接管上游商品），价格由同步 UPSERT 写入，回填不应破坏它。
func TestSyncSelfFallbackPricesSkipsSelfReference(t *testing.T) {
	// 共享测试库里同一商品号可能已被前几次运行接管过，先清干净保证可重复运行。
	_, _ = db.Must().Exec("DELETE FROM products WHERE goods_id=830001")
	seedUpstreamProduct(t, 830001, 0.064)

	// 接管：上游商品转为自营，兜底指向自身。
	if e := store.UpdateSelfProduct(store.SelfProductInput{
		GoodsID: 830001, Name: "接管上游商品", Price: 0.064,
		FallbackGoodsID: 830001, Enabled: true,
	}); e != nil {
		t.Fatalf("接管上游商品失败: %v", e)
	}
	// 模拟同步写入了新价。
	if _, e := db.Must().Exec("UPDATE products SET final_price=0.07 WHERE goods_id=?", 830001); e != nil {
		t.Fatalf("改价失败: %v", e)
	}
	// 自引用时回填跳过，避免读到旧值把价格写坏。
	upstream.SyncSelfFallbackPrices()
	if got := priceOf(t, 830001); got != 0.07 {
		t.Fatalf("自引用商品售价不应被回填改动，期望 0.07 实际 %v", got)
	}
}

// 跟随上游的商品在上游调价并同步后，必须走和上游同 ID 商品一样的加价路径，
// 不能被「成本价」直接顶掉（线上曾把 0.064 的售价打成 0.032）。
func TestSelfProductFollowPriceKeepsMarkup(t *testing.T) {
	store.EnsureSelfCategory()
	_, _ = db.Must().Exec("DELETE FROM products WHERE goods_id IN (840001,910004)")

	// 兜底商品：成本 0.032，经全站 100% 加价后售价 0.064。
	seedUpstreamProduct(t, 840001, 0.032)
	if _, e := db.Must().Exec(
		"UPDATE products SET cost_price=0.032, final_price=0.064 WHERE goods_id=840001"); e != nil {
		t.Fatal(e)
	}

	goodsID, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: 910004, Name: "跟随加价商品", FallbackGoodsID: 840001, Enabled: true,
	})
	if e != nil {
		t.Fatalf("创建失败: %v", e)
	}
	if got := priceOf(t, goodsID); got != 0.064 {
		t.Fatalf("初始售价应为兜底商品售价 0.064，实际 %v", got)
	}

	// 模拟上游同步把成本价调到 0.05，随后重算 + 回填（与 catalog.go 同步尾部同序）。
	if _, e := db.Must().Exec(
		"UPDATE products SET cost_price=0.05, sales_price=0.05 WHERE goods_id=840001"); e != nil {
		t.Fatal(e)
	}
	store.RecomputeAllPrices()
	upstream.SyncSelfFallbackPrices()

	follow := priceOf(t, goodsID)
	base := priceOf(t, 840001)
	if follow != base {
		t.Fatalf("跟随商品的售价应与兜底商品一致：跟随=%v 兜底=%v", follow, base)
	}
	if follow <= 0.05 {
		t.Fatalf("售价不应被成本价顶掉（成本 0.05，实际售价 %v），说明加价规则没生效", follow)
	}
}
