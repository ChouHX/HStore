package store

import (
	"testing"

	"hstore/backend/internal/db"
)

func seedTierProduct(t *testing.T, goodsID int, price float64) {
	t.Helper()
	if _, e := db.Must().Exec(`INSERT INTO products(goods_id, name, final_price, cost_price, sales_price,
		platform, updated_at, stock_count) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(goods_id) DO UPDATE SET final_price=excluded.final_price`,
		goodsID, "阶梯折扣测试", price, price, price, "microsoft", 1, 1000); e != nil {
		t.Fatalf("准备商品失败: %v", e)
	}
	t.Cleanup(func() {
		_, _ = db.Must().Exec("DELETE FROM product_tier_discounts WHERE goods_id=?", goodsID)
		_, _ = db.Must().Exec("DELETE FROM products WHERE goods_id=?", goodsID)
	})
}

// 未达门槛不打折，达到门槛按对应档位打折。
func TestTierDiscountFor(t *testing.T) {
	const gid = 970101
	seedTierProduct(t, gid, 1.0)
	if e := ReplaceTierDiscounts(gid, []TierDiscount{
		{MinCount: 10, Discount: 0.95},
		{MinCount: 50, Discount: 0.90},
		{MinCount: 100, Discount: 0.80},
	}); e != nil {
		t.Fatalf("保存档位失败: %v", e)
	}

	cases := []struct {
		count int
		want  float64
	}{
		{1, 1},     // 未达门槛
		{9, 1},     // 差一件
		{10, 0.95}, // 命中第一档
		{49, 0.95}, // 仍在第一档
		{50, 0.90}, // 命中第二档
		{99, 0.90},
		{100, 0.80}, // 命中第三档
		{500, 0.80}, // 取最高命中档
	}
	for _, c := range cases {
		if got := TierDiscountFor(gid, c.count); got != c.want {
			t.Errorf("买 %d 件折扣应为 %v，实际 %v", c.count, c.want, got)
		}
	}
}

// 单价 = final_price × 等级折扣 × 批量折扣。
func TestResolveUnitPriceCombinesLevelAndTier(t *testing.T) {
	const gid = 970102
	seedTierProduct(t, gid, 10)
	if e := ReplaceTierDiscounts(gid, []TierDiscount{{MinCount: 5, Discount: 0.9}}); e != nil {
		t.Fatalf("保存档位失败: %v", e)
	}

	// 4 件：只有等级折扣 0.8
	b := ResolveUnitPrice(gid, 10, 0.8, 4)
	if b.UnitPrice != 8 {
		t.Fatalf("4 件单价应为 8，实际 %v", b.UnitPrice)
	}
	if b.MatchedCount != 0 {
		t.Fatalf("4 件不应命中档位，实际 %d", b.MatchedCount)
	}

	// 5 件：0.8 × 0.9 = 0.72 → 单价 7.2
	b = ResolveUnitPrice(gid, 10, 0.8, 5)
	if b.UnitPrice != 7.2 {
		t.Fatalf("5 件单价应为 7.2，实际 %v", b.UnitPrice)
	}
	if b.MatchedCount != 5 {
		t.Fatalf("应命中 5 件档，实际 %d", b.MatchedCount)
	}
	if b.TierRate != 0.9 {
		t.Fatalf("批量折扣应为 0.9，实际 %v", b.TierRate)
	}
}

// 清空后恢复原价。
func TestReplaceTierDiscountsClears(t *testing.T) {
	const gid = 970103
	seedTierProduct(t, gid, 2)
	_ = ReplaceTierDiscounts(gid, []TierDiscount{{MinCount: 10, Discount: 0.5}})
	if got := TierDiscountFor(gid, 100); got != 0.5 {
		t.Fatalf("应命中 0.5 折扣，实际 %v", got)
	}
	if e := ReplaceTierDiscounts(gid, nil); e != nil {
		t.Fatalf("清空失败: %v", e)
	}
	if got := TierDiscountFor(gid, 100); got != 1 {
		t.Fatalf("清空后应无折扣，实际 %v", got)
	}
}

// 非法档位被忽略：门槛 <2、折扣不在 (0,1)。
func TestReplaceTierDiscountsRejectsInvalid(t *testing.T) {
	const gid = 970104
	seedTierProduct(t, gid, 1)
	_ = ReplaceTierDiscounts(gid, []TierDiscount{
		{MinCount: 1, Discount: 0.5},   // 门槛太低
		{MinCount: 10, Discount: 1},    // 100% 不打折
		{MinCount: 20, Discount: 1.5},  // 加价，不走这条路径
		{MinCount: 30, Discount: -0.5}, // 负数
		{MinCount: 40, Discount: 0.85}, // 唯一合法
	})
	tiers := ListTierDiscounts(gid)
	if len(tiers) != 1 || tiers[0].MinCount != 40 {
		t.Fatalf("应只保留 1 个合法档位，实际 %+v", tiers)
	}
}
