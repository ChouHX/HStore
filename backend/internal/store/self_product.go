package store

import (
	"errors"
	"strings"
	"time"

	"hstore/backend/internal/db"
)

// 自营板块固定使用这个分类 ID，避免与上游分类号段冲突。
const SelfCategoryID = 900001

// SelfCategoryName 是自营板块的展示名。
const SelfCategoryName = "自营商品"

// SelfGoodsIDStart 是自营商品编号的起点，避开上游号段。
const SelfGoodsIDStart = 900000

// ErrSelfProductNotFound 表示找不到该自营商品。
var ErrSelfProductNotFound = errors.New("自营商品不存在")

// SelfProductInput 是新建 / 编辑自营商品的入参。
type SelfProductInput struct {
	GoodsID         int
	Name            string
	Price           float64
	CostPrice       float64
	Description     string
	ImageURL        string
	Enabled         bool
	FallbackGoodsID int
}

// EnsureSelfCategory 确保自营板块存在，返回其 ID。
// 上游分类同步会跳过 platform='self' 的分类，所以这里创建后不会被清理。
func EnsureSelfCategory() int {
	d := db.Must()
	now := time.Now().UnixMilli()
	_, _ = d.Exec(`INSERT INTO categories(id, name, img, parent_id, platform, updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, platform='self'`,
		SelfCategoryID, SelfCategoryName, "", nil, "self", now)
	return SelfCategoryID
}

// ListSelfProducts 返回全部自营商品（管理员视角，包含已下架的）。
func ListSelfProducts() []map[string]any {
	rows, e := db.Must().Query(`
		SELECT p.goods_id, p.name, COALESCE(p.custom_desc,''), COALESCE(p.img_url,''),
		       p.final_price, p.cost_price, p.enabled,
		       p.fallback_goods_id, p.updated_at,
		       (SELECT COUNT(*) FROM self_stock s WHERE s.goods_id=p.goods_id AND s.status='available'),
		       (SELECT COUNT(*) FROM self_stock s WHERE s.goods_id=p.goods_id AND s.status='sold')
		FROM products p
		WHERE p.fulfillment='self'
		ORDER BY p.goods_id`)
	if e != nil {
		return nil
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			gid, enabled, fb, updated int
			name, desc, img           string
			finalPrice, costPrice     float64
			avail, sold               int
		)
		if rows.Scan(&gid, &name, &desc, &img, &finalPrice, &costPrice,
			&enabled, &fb, &updated, &avail, &sold) != nil {
			continue
		}
		out = append(out, map[string]any{
			"goodsId":         gid,
			"name":            name,
			"description":     desc,
			"imgUrl":          img,
			"finalPrice":      finalPrice,
			"costPrice":       costPrice,
			"enabled":         enabled,
			"fallbackGoodsId": fb,
			"updatedAt":       updated,
			"stockAvailable":  avail,
			"stockSold":       sold,
		})
	}
	return out
}

// resolveSelfPrice 决定自营商品落库的价格。
//
// 自营商品已与上游完全分离（不再绑定兜底商品），因此售价必须由管理员自定义，
// price 必须 > 0；不会再去读任何上游商品的价格。
//
// 返回 最终价格、成本价、是否跟随上游（已恒为 false，保留字段仅为兼容调用方）。
func resolveSelfPrice(fallbackGoodsID int, price, costPrice float64) (float64, float64, bool, error) {
	if price <= 0 {
		return 0, 0, false, errors.New("自营商品必须填写售价")
	}
	cost := costPrice
	if cost <= 0 {
		cost = price
	}
	return price, cost, false, nil
}

func CreateSelfProduct(in SelfProductInput) (int, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return 0, errors.New("商品名称不能为空")
	}
	if in.Price < 0 {
		return 0, errors.New("售价不能为负数")
	}
	finalPrice, cost, follow, e0 := resolveSelfPrice(in.FallbackGoodsID, in.Price, in.CostPrice)
	if e0 != nil {
		return 0, e0
	}
	_ = follow // 自营与上游已分离，不再有跟随上游价格的路径
	d := db.Must()
	catID := EnsureSelfCategory()

	goodsID := in.GoodsID
	if goodsID <= 0 {
		// 取当前最大自营号 +1，冷启动从 SelfGoodsIDStart 起。
		var maxID int
		_ = d.QueryRow(
			"SELECT COALESCE(MAX(goods_id),0) FROM products WHERE platform='self' OR goods_id>=?",
			SelfGoodsIDStart).Scan(&maxID)
		if maxID < SelfGoodsIDStart {
			maxID = SelfGoodsIDStart
		}
		goodsID = maxID + 1
	}

	now := time.Now().UnixMilli()
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	img := strings.TrimSpace(in.ImageURL)
	desc := strings.TrimSpace(in.Description)

	_, e := d.Exec(`INSERT INTO products(
		goods_id, name, face_value, sales_price, goods_type, warranty_days, status,
		multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids,
		platform, updated_at, stock_count, root_category_id, fulfillment,
		fallback_goods_id, custom_desc, custom_name, custom_img
	) VALUES(?,?,?,?,1,0,1,1,0,0,0,0,?,'[]','self',?,0,?,'self',?,?,?,?)`,
		goodsID, name, finalPrice, finalPrice, img, now, catID, 0, desc, name, img)
	if e != nil {
		return 0, e
	}

	// 自营商品一律锁价（manual_price = 自定义价），由管理员自主定价。
	if _, e := d.Exec(
		"UPDATE products SET final_price=?, cost_price=?, manual_price=?, price_locked=1 WHERE goods_id=?",
		finalPrice, cost, finalPrice, goodsID); e != nil {
		return 0, e
	}
	if _, e := d.Exec("UPDATE products SET enabled=? WHERE goods_id=?", enabled, goodsID); e != nil {
		return 0, e
	}
	return goodsID, nil
}

// UpdateSelfProduct 编辑自营商品信息。
func UpdateSelfProduct(in SelfProductInput) error {
	if in.GoodsID <= 0 {
		return ErrSelfProductNotFound
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return errors.New("商品名称不能为空")
	}
	if in.Price < 0 {
		return errors.New("售价不能为负数")
	}
	finalPrice, cost, follow, e0 := resolveSelfPrice(in.FallbackGoodsID, in.Price, in.CostPrice)
	if e0 != nil {
		return e0
	}
	_ = follow // 自营与上游已分离，恒为锁价，不再跟随上游价格
	d := db.Must()
	now := time.Now().UnixMilli()
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	img := strings.TrimSpace(in.ImageURL)

	// 注意：不改 sales_price —— 上游商品的 sales_price 是同步进来的原始上游价。
	// 自营商品始终锁价（manual_price = 自定义价），不再根据兜底商品浮动。
	lockSQL := "manual_price=?, price_locked=1"
	lockArgs := []any{finalPrice}
	// 注意：不再写 fallback_goods_id——兜底已下线，保留列中已有的历史值不动，
	// 以免抹掉当初的履约配置（历史订单追溯需要）。
	res, e := d.Exec(`UPDATE products SET
		name=?, custom_name=?, custom_desc=?, img_url=?, custom_img=?,
		final_price=?, cost_price=?, `+lockSQL+`,
		enabled=?, updated_at=?, root_category_id=?, fulfillment='self'
		WHERE goods_id=? AND (platform='self' OR fulfillment='self')`,
		append([]any{name, name, strings.TrimSpace(in.Description), img, img, finalPrice, cost},
			append(lockArgs, enabled, now, EnsureSelfCategory(), in.GoodsID)...)...)
	if e != nil {
		return e
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 可能本来不是自营商品，尝试接管为自营。
		// 注意：不要改 platform —— 上游商品（platform='microsoft'）保留 platform
		// 才能继续从上游同步价格/库存/详情，这里只打 fulfillment='self' 标记。
		res2, e2 := d.Exec(`UPDATE products SET
			name=?, custom_name=?, custom_desc=?, final_price=?, cost_price=?,
			`+lockSQL+`, enabled=?, updated_at=?,
			root_category_id=?, fulfillment='self'
			WHERE goods_id=?`,
			append([]any{name, name, strings.TrimSpace(in.Description), finalPrice, cost},
				append(lockArgs, enabled, now, EnsureSelfCategory(), in.GoodsID)...)...)
		if e2 != nil {
			return e2
		}
		if n2, _ := res2.RowsAffected(); n2 == 0 {
			return ErrSelfProductNotFound
		}
	}
	return nil
}

// DeleteSelfProduct 删除自营商品及其本地库存。
// 若已有订单，则仅下架，保留历史可追溯。
func DeleteSelfProduct(goodsID int) (bool, error) {
	d := db.Must()
	var orders int
	_ = d.QueryRow("SELECT COUNT(*) FROM orders WHERE goods_id=?", goodsID).Scan(&orders)
	if orders > 0 {
		if _, e := d.Exec("UPDATE products SET enabled=0 WHERE goods_id=?", goodsID); e != nil {
			return false, e
		}
		return false, nil
	}
	tx, e := d.Begin()
	if e != nil {
		return false, e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e := tx.Exec("DELETE FROM self_stock WHERE goods_id=? AND status IN ('available','voided')", goodsID); e != nil {
		return false, e
	}
	res, e := tx.Exec("DELETE FROM products WHERE goods_id=?", goodsID)
	if e != nil {
		return false, e
	}
	n, _ := res.RowsAffected()
	if e := tx.Commit(); e != nil {
		return false, e
	}
	return n > 0, nil
}
