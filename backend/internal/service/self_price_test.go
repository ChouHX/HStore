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

// 自营商品已与上游分离：即使传了 fallbackGoodsId，也一律用管理员自定义价，
// 不会去读上游商品的售价。
func TestSelfProductIgnoresFallbackPrice(t *testing.T) {
	store.EnsureSelfCategory()
	_, _ = db.Must().Exec("DELETE FROM products WHERE goods_id IN (820001,910002)")
	seedUpstreamProduct(t, 820001, 0.032)

	goodsID, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: 910002, Name: "自营独立定价商品", Price: 9.99,
		FallbackGoodsID: 820001, Enabled: true,
	})
	if e != nil {
		t.Fatalf("创建自营商品失败: %v", e)
	}
	if got := priceOf(t, goodsID); got != 9.99 {
		t.Fatalf("售价应为自定义的 9.99（不受兜底商品影响），实际 %v", got)
	}
}

// 上游商品调价并同步后，自营商品的售价必须保持不变（不再跟随上游浮动）。
func TestSelfProductPriceStableAfterUpstreamChange(t *testing.T) {
	store.EnsureSelfCategory()
	_, _ = db.Must().Exec("DELETE FROM products WHERE goods_id IN (840001,910004)")

	seedUpstreamProduct(t, 840001, 0.032)
	if _, e := db.Must().Exec(
		"UPDATE products SET cost_price=0.032, final_price=0.064 WHERE goods_id=840001"); e != nil {
		t.Fatal(e)
	}

	goodsID, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: 910004, Name: "自营固定价商品", Price: 0.99,
		FallbackGoodsID: 840001, Enabled: true,
	})
	if e != nil {
		t.Fatalf("创建失败: %v", e)
	}
	if got := priceOf(t, goodsID); got != 0.99 {
		t.Fatalf("初始售价应为自定义的 0.99，实际 %v", got)
	}

	// 模拟上游同步把价格改掉，再重算 + 回填：自营价格必须纹丝不动。
	if _, e := db.Must().Exec(
		"UPDATE products SET cost_price=0.05, sales_price=0.05, final_price=0.05 WHERE goods_id=840001"); e != nil {
		t.Fatal(e)
	}
	store.RecomputeAllPrices()
	upstream.SyncSelfFallbackPrices()

	if got := priceOf(t, goodsID); got != 0.99 {
		t.Fatalf("上游调价后自营售价应保持 0.99，实际 %v", got)
	}
}
