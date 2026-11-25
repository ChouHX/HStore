package store

import (
	"testing"

	"hstore/backend/internal/db"
)

// 上游涨价后，成本价必须被同步更新，且能触发售价重算。
func TestUpdateCostPrice(t *testing.T) {
	const gid = 950401
	if _, e := db.Must().Exec(`INSERT INTO products(goods_id, name, cost_price, sales_price, final_price,
		platform, updated_at, stock_count, fulfillment, price_locked)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(goods_id) DO UPDATE SET cost_price=excluded.cost_price,
		  stock_count=excluded.stock_count, price_locked=0, manual_price=NULL`,
		gid, "成本同步测试", 3.38, 3.38, 5.75, "google", 1, 100, "upstream", 0); e != nil {
		t.Fatalf("准备商品失败: %v", e)
	}
	t.Cleanup(func() { _, _ = db.Must().Exec("DELETE FROM products WHERE goods_id=?", gid) })

	// 上游从 3.38 涨到 5.84
	if !UpdateCostPrice(gid, 5.84, 50) {
		t.Fatal("成本价变化应返回 true")
	}
	var cost, stock float64
	_ = db.Must().QueryRow("SELECT cost_price, stock_count FROM products WHERE goods_id=?", gid).
		Scan(&cost, &stock)
	if cost != 5.84 {
		t.Fatalf("成本价应为 5.84，实际 %v", cost)
	}
	if stock != 50 {
		t.Fatalf("库存应为 50，实际 %v", stock)
	}

	// 价格没变时不重复触发重算
	if UpdateCostPrice(gid, 5.84, 50) {
		t.Fatal("成本价未变化时应返回 false")
	}
}

// 自营商品的成本由管理员维护，不该被上游同步覆盖。
func TestUpdateCostPriceSkipsSelf(t *testing.T) {
	const gid = 950402
	if _, e := db.Must().Exec(`INSERT INTO products(goods_id, name, cost_price, final_price,
		platform, updated_at, stock_count, fulfillment)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(goods_id) DO UPDATE SET cost_price=excluded.cost_price`,
		gid, "自营成本保护", 2.0, 5.0, "self", 1, 10, "self"); e != nil {
		t.Fatalf("准备商品失败: %v", e)
	}
	t.Cleanup(func() { _, _ = db.Must().Exec("DELETE FROM products WHERE goods_id=?", gid) })

	UpdateCostPrice(gid, 9.99, 10)
	var cost float64
	_ = db.Must().QueryRow("SELECT cost_price FROM products WHERE goods_id=?", gid).Scan(&cost)
	if cost != 2.0 {
		t.Fatalf("自营商品成本价不应被覆盖，期望 2.0，实际 %v", cost)
	}
}
