package store

import (
	"database/sql"
	"math"
	"sort"
	"strings"

	"hstore/backend/internal/db"
)

// TierDiscount 是商品的一档批量折扣。
//
// 语义：购买数量 >= MinCount 时，单价按 discount 打折（0.95 = 95 折）。
// 折扣作用在「加价模板算出的 final_price」之上，再与会员等级折扣叠加。
type TierDiscount struct {
	ID        int     `json:"id"`
	GoodsID   int     `json:"goodsId"`
	MinCount  int     `json:"minCount"`
	Discount  float64 `json:"discount"`
	CreatedAt int64   `json:"createdAt"`
	UpdatedAt int64   `json:"updatedAt"`
}

// ListTierDiscounts 返回某商品的全部折扣档（按 min_count 升序）。
func ListTierDiscounts(goodsID int) []TierDiscount {
	rows, e := db.Must().Query(`SELECT id, goods_id, min_count, discount, created_at, updated_at
		FROM product_tier_discounts WHERE goods_id = ? ORDER BY min_count ASC`, goodsID)
	if e != nil {
		return []TierDiscount{}
	}
	defer rows.Close()
	out := []TierDiscount{}
	for rows.Next() {
		var d TierDiscount
		if rows.Scan(&d.ID, &d.GoodsID, &d.MinCount, &d.Discount, &d.CreatedAt, &d.UpdatedAt) == nil {
			out = append(out, d)
		}
	}
	return out
}

// ListAllTierDiscounts 返回全部商品的折扣档，按商品分组。
func ListAllTierDiscounts() map[int][]TierDiscount {
	rows, e := db.Must().Query(`SELECT id, goods_id, min_count, discount, created_at, updated_at
		FROM product_tier_discounts ORDER BY goods_id ASC, min_count ASC`)
	if e != nil {
		return map[int][]TierDiscount{}
	}
	defer rows.Close()
	out := map[int][]TierDiscount{}
	for rows.Next() {
		var d TierDiscount
		if rows.Scan(&d.ID, &d.GoodsID, &d.MinCount, &d.Discount, &d.CreatedAt, &d.UpdatedAt) == nil {
			out[d.GoodsID] = append(out[d.GoodsID], d)
		}
	}
	return out
}

// ReplaceTierDiscounts 用给定档位整体替换某商品的折扣配置（事务内完成）。
//
// 传空切片表示清空该商品的折扣。档位按 min_count 去重，后出现的覆盖先出现的。
func ReplaceTierDiscounts(goodsID int, tiers []TierDiscount) error {
	if goodsID <= 0 {
		return nil
	}
	d := db.Must()
	tx, e := d.Begin()
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()

	if _, e := tx.Exec("DELETE FROM product_tier_discounts WHERE goods_id = ?", goodsID); e != nil {
		return e
	}
	now := now()
	seen := map[int]bool{}
	for _, t := range tiers {
		if t.MinCount < 2 {
			// 1 件起不打折，避免与"满 1 件"这种无意义配置混淆
			continue
		}
		if t.Discount <= 0 || t.Discount >= 1 {
			// 折扣必须落在 (0,1)：1 表示不减，>1 是加价，都不走这条路径
			continue
		}
		if seen[t.MinCount] {
			continue
		}
		seen[t.MinCount] = true
		if _, e := tx.Exec(`INSERT INTO product_tier_discounts(goods_id, min_count, discount, created_at, updated_at)
			VALUES(?,?,?,?,?)`, goodsID, t.MinCount, t.Discount, now, now); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// DeleteTierDiscounts 删除某商品的全部折扣档。
func DeleteTierDiscounts(goodsID int) {
	_, _ = db.Must().Exec("DELETE FROM product_tier_discounts WHERE goods_id = ?", goodsID)
}

// TierDiscountFor 返回购买 count 件时应适用的折扣系数（没有则返回 1）。
//
// 取满足 min_count <= count 的最大档位：买得越多折扣越低（数值越小）。
// 若同一商品配置了多档，按 min_count 从大到小找第一个命中的。
func TierDiscountFor(goodsID, count int) float64 {
	if goodsID <= 0 || count < 2 {
		return 1
	}
	var discount float64
	// ORDER BY min_count DESC + LIMIT 1：命中满足条件的最高档
	e := db.Must().QueryRow(`SELECT discount FROM product_tier_discounts
		WHERE goods_id = ? AND min_count <= ?
		ORDER BY min_count DESC LIMIT 1`, goodsID, count).Scan(&discount)
	if e != nil || discount <= 0 || discount >= 1 {
		return 1
	}
	return discount
}

// TierPriceBreakdown 描述一次购买的最终单价构成，供前端展示。
type TierPriceBreakdown struct {
	BasePrice    float64 `json:"basePrice"`    // 加价模板后的售价
	LevelRate    float64 `json:"levelRate"`    // 会员等级折扣
	TierRate     float64 `json:"tierRate"`     // 批量折扣
	UnitPrice    float64 `json:"unitPrice"`    // 最终单价
	MatchedCount int     `json:"matchedCount"` // 命中的档位门槛（0 = 未命中）
}

// ResolveUnitPrice 计算一批购买的实际单价：
//
//	最终单价 = final_price × 等级折扣 × 批量折扣
//
// 批量折扣以「加价模板后的 final_price」为基数，与等级折扣相乘叠加。
func ResolveUnitPrice(goodsID int, finalPrice float64, levelRate float64, count int) TierPriceBreakdown {
	if levelRate <= 0 || levelRate > 1 {
		levelRate = 1
	}
	tierRate := TierDiscountFor(goodsID, count)
	unit := round4Local(finalPrice * levelRate * tierRate)
	matched := 0
	if tierRate < 1 {
		// 回填命中的门槛，方便前端显示「满 N 件」
		_ = db.Must().QueryRow(`SELECT min_count FROM product_tier_discounts
			WHERE goods_id = ? AND min_count <= ? ORDER BY min_count DESC LIMIT 1`,
			goodsID, count).Scan(&matched)
	}
	return TierPriceBreakdown{
		BasePrice:    round4Local(finalPrice),
		LevelRate:    levelRate,
		TierRate:     tierRate,
		UnitPrice:    unit,
		MatchedCount: matched,
	}
}

// round4Local 与 service 包的 round4 口径一致（保留 4 位小数）。
func round4Local(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// TierDiscountRow 用于后台列表展示：商品 + 折扣档。
type TierDiscountRow struct {
	GoodsID     int            `json:"goodsId"`
	Name        string         `json:"name"`
	Fulfillment string         `json:"fulfillment"`
	FinalPrice  float64        `json:"finalPrice"`
	Tiers       []TierDiscount `json:"tiers"`
}

// ListTierDiscountRows 返回配置了折扣的商品列表（可按关键词过滤）。
func ListTierDiscountRows(keyword string) []TierDiscountRow {
	grouped := ListAllTierDiscounts()
	if len(grouped) == 0 {
		return []TierDiscountRow{}
	}
	out := make([]TierDiscountRow, 0, len(grouped))
	for gid, tiers := range grouped {
		var name, fulfill string
		var price float64
		e := db.Must().QueryRow(`SELECT COALESCE(NULLIF(custom_name,''), name),
			COALESCE(fulfillment,'upstream'), final_price FROM products WHERE goods_id = ?`, gid).
			Scan(&name, &fulfill, &price)
		if e == sql.ErrNoRows {
			continue
		}
		if keyword != "" && !containsFold(name, keyword) {
			continue
		}
		out = append(out, TierDiscountRow{
			GoodsID: gid, Name: name, Fulfillment: fulfill, FinalPrice: price, Tiers: tiers,
		})
	}
	// 按商品号排序，保证输出稳定
	sortRowsByGoodsID(out)
	return out
}

// containsFold 大小写不敏感的子串判断（后台搜索用）。
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// sortRowsByGoodsID 按商品号升序排列，保证列表输出稳定。
func sortRowsByGoodsID(rows []TierDiscountRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].GoodsID < rows[j].GoodsID })
}
