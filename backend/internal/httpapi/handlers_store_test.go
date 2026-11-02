package httpapi

import (
	"fmt"
	"path/filepath"
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

// setupStoreDB 用临时库跑一遍完整的建表 / 迁移 / 种子，供商城接口测试使用。
// 必须在用到 store 包之前调用（db 是进程内单例）。
func setupStoreDB(t *testing.T) {
	t.Helper()
	t.Setenv("FAKA_DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	if _, err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
}

func insertProduct(t *testing.T, goodsID int, fulfillment string, fallbackGoodsID, stockCount int) {
	t.Helper()
	_, err := db.Must().Exec(
		`INSERT INTO products(goods_id, name, platform, updated_at, stock_count, fulfillment, fallback_goods_id)
		 VALUES(?,?,?,?,?,?,?)`,
		goodsID, "测试商品", "self", 1, stockCount, fulfillment, fallbackGoodsID,
	)
	if err != nil {
		t.Fatalf("insert product %d: %v", goodsID, err)
	}
}

func insertSelfStock(t *testing.T, goodsID, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := db.Must().Exec(
			"INSERT INTO self_stock(goods_id, content, status, source, source_id, created_at) VALUES(?,?,?,?,?,?)",
			goodsID, "card", "available", "test", fmt.Sprintf("s-%d-%d", goodsID, i), 1,
		); err != nil {
			t.Fatalf("insert self stock for %d: %v", goodsID, err)
		}
	}
}

// 自营商品的真实库存条数不能出现在前台接口里，只给「有货 / 缺货」标记：
//   - 本地有货                → 有货
//   - 本地无货 + 上游兜底商品  → 有货（下单时走上游采购）
//   - 本地无货 + 无兜底        → 缺货
func TestMaskSelfProductStock(t *testing.T) {
	setupStoreDB(t)

	insertProduct(t, 910001, "self", 0, 0)
	insertSelfStock(t, 910001, 7) // 真实库存 7 条，前台不能看到 7
	insertProduct(t, 910002, "self", 0, 0)
	insertProduct(t, 910003, "self", 12345, 0)
	insertProduct(t, 920001, "upstream", 0, 42)

	cases := []struct {
		goodsID int
		want    int
		desc    string
	}{
		{910001, 1, "本地有货"},
		{910002, 0, "本地缺货且无上游兜底"},
		{910003, 1, "本地缺货但有上游兜底"},
		{920001, 42, "上游商品库存原样返回"},
	}

	// 列表接口：就地脱敏
	p := store.GetProduct(910001)
	if p == nil {
		t.Fatal("商品 910001 不存在")
	}
	items := make([]model.Product, 0, len(cases))
	for _, c := range cases {
		got := store.GetProduct(c.goodsID)
		if got == nil {
			t.Fatalf("商品 %d 不存在", c.goodsID)
		}
		items = append(items, *got)
	}
	sanitizeSelfProducts(items)
	for i, c := range cases {
		if items[i].StockCount != c.want {
			t.Errorf("列表 %s（商品 %d）: stockCount = %d, want %d",
				c.desc, c.goodsID, items[i].StockCount, c.want)
		}
	}

	// 详情接口：自营商品的 stockCount 同样是脱敏标记
	for _, c := range cases {
		prod := store.GetProduct(c.goodsID)
		if prod == nil {
			t.Fatalf("商品 %d 不存在", c.goodsID)
		}
		detail := selfProductDetail(prod)
		if c.goodsID == 920001 {
			continue // 上游商品不走 selfProductDetail
		}
		if detail.StockCount != c.want {
			t.Errorf("详情 %s（商品 %d）: stockCount = %d, want %d",
				c.desc, c.goodsID, detail.StockCount, c.want)
		}
	}
}
