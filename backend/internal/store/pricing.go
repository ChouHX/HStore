package store

import (
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"strings"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// ApplyRounding 按规则取整。
func ApplyRounding(x float64, r string) float64 {
	switch r {
	case "ceil2":
		return math.Ceil(x*100) / 100
	case "floor2":
		return math.Floor(x*100) / 100
	case "round3":
		return math.Round(x*1000) / 1000
	case "round4":
		return math.Round(x*10000) / 10000
	case "ceil4":
		return math.Ceil(x*10000) / 10000
	case "floor4":
		return math.Floor(x*10000) / 10000
	case "ceil1":
		return math.Ceil(x*10) / 10
	case "round0":
		return math.Round(x)
	default:
		return math.Round(x*100) / 100
	}
}

func applyFormula(cost float64, r model.PricingRule) float64 {
	switch r.FormulaType {
	case "fixed_add":
		return cost + r.Amount
	case "fixed_sub":
		return cost - r.Amount
	case "ratio":
		return cost * (1 + r.Percent/100)
	case "mixed":
		return cost*r.Multiplier + r.Fixed
	default:
		return cost
	}
}

func scanRule(sc interface{ Scan(...any) error }) (model.PricingRule, error) {
	var r model.PricingRule
	e := sc.Scan(&r.ID, &r.Name, &r.Scope, &r.ScopeID, &r.FormulaType, &r.Amount, &r.Percent,
		&r.Multiplier, &r.Fixed, &r.Rounding, &r.Priority, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	return r, e
}

const ruleCols = `id, name, scope, scope_id, formula_type, amount, percent, multiplier, fixed, rounding, priority, enabled, created_at, updated_at`

// ListRules 列出定价规则。
func ListRules() []model.PricingRule {
	rows, e := db.Must().Query("SELECT " + ruleCols + " FROM pricing_rules ORDER BY priority DESC, id ASC")
	if e != nil {
		return []model.PricingRule{}
	}
	defer rows.Close()
	out := []model.PricingRule{}
	for rows.Next() {
		if r, e := scanRule(rows); e == nil {
			out = append(out, r)
		}
	}
	return out
}

// GetRule 取单条规则。
func GetRule(id int) *model.PricingRule {
	row := db.Must().QueryRow("SELECT "+ruleCols+" FROM pricing_rules WHERE id = ?", id)
	r, e := scanRule(row)
	if e != nil {
		return nil
	}
	return &r
}

// RuleInput 是新建/修改规则的入参。
type RuleInput struct {
	Name        string
	Scope       string
	ScopeID     string
	FormulaType string
	Amount      float64
	Percent     float64
	Multiplier  float64
	Fixed       float64
	Rounding    string
	Priority    int
	Enabled     int
}

// CreateRule 新建规则。
func CreateRule(in RuleInput) *model.PricingRule {
	if in.Scope == "" {
		in.Scope = "global"
	}
	if in.FormulaType == "" {
		in.FormulaType = "fixed_add"
	}
	if in.Rounding == "" {
		in.Rounding = "round2"
	}
	if in.Multiplier == 0 {
		in.Multiplier = 1
	}
	ts := now()
	res, e := db.Must().Exec(`INSERT INTO pricing_rules(name, scope, scope_id, formula_type, amount, percent, multiplier, fixed, rounding, priority, enabled, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.Name, in.Scope, in.ScopeID, in.FormulaType, in.Amount, in.Percent, in.Multiplier,
		in.Fixed, in.Rounding, in.Priority, in.Enabled, ts, ts)
	if e != nil {
		return nil
	}
	id, _ := res.LastInsertId()
	return GetRule(int(id))
}

// UpdateRule 修改规则（空指针字段保持原值）。
func UpdateRule(id int, patch map[string]any) *model.PricingRule {
	cur := GetRule(id)
	if cur == nil {
		return nil
	}
	name, scope, scopeID := cur.Name, cur.Scope, cur.ScopeID
	formula, rounding := cur.FormulaType, cur.Rounding
	amount, percent, multiplier, fixed := cur.Amount, cur.Percent, cur.Multiplier, cur.Fixed
	priority, enabled := cur.Priority, cur.Enabled
	if v, ok := patch["name"].(string); ok {
		name = v
	}
	if v, ok := patch["scope"].(string); ok && v != "" {
		scope = v
	}
	if v, ok := patch["scopeId"].(string); ok {
		scopeID = v
	}
	if v, ok := patch["formulaType"].(string); ok && v != "" {
		formula = v
	}
	if v, ok := patch["rounding"].(string); ok && v != "" {
		rounding = v
	}
	if v, ok := patch["amount"].(float64); ok {
		amount = v
	}
	if v, ok := patch["percent"].(float64); ok {
		percent = v
	}
	if v, ok := patch["multiplier"].(float64); ok {
		multiplier = v
	}
	if v, ok := patch["fixed"].(float64); ok {
		fixed = v
	}
	if v, ok := patch["priority"].(float64); ok {
		priority = int(v)
	}
	if v, ok := patch["enabled"].(float64); ok {
		enabled = int(v)
	}
	_, _ = db.Must().Exec(`UPDATE pricing_rules SET name=?, scope=?, scope_id=?, formula_type=?, amount=?,
		percent=?, multiplier=?, fixed=?, rounding=?, priority=?, enabled=?, updated_at=? WHERE id=?`,
		name, scope, scopeID, formula, amount, percent, multiplier, fixed, rounding, priority, enabled, now(), id)
	return GetRule(id)
}

// DeleteRule 删除规则。
func DeleteRule(id int) { _, _ = db.Must().Exec("DELETE FROM pricing_rules WHERE id = ?", id) }

var specificity = map[string]int{"global": 0, "platform": 1, "category": 2, "product": 3}

// ResolvePrice 解析单个商品售价：取最具体的匹配规则。
func ResolvePrice(cost float64, goodsID int, dirIDs []int, platform string) (float64, *int) {
	var matched []model.PricingRule
	for _, r := range ListRules() {
		if r.Enabled != 1 {
			continue
		}
		ok := false
		switch r.Scope {
		case "global":
			ok = true
		case "product":
			ok = r.ScopeID == strconvItoa(goodsID)
		case "category":
			for _, d := range dirIDs {
				if r.ScopeID == strconvItoa(d) {
					ok = true
					break
				}
			}
		case "platform":
			ok = r.ScopeID == platform
		}
		if ok {
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		return ApplyRounding(cost, "round2"), nil
	}
	sort.SliceStable(matched, func(a, b int) bool {
		if specificity[matched[a].Scope] != specificity[matched[b].Scope] {
			return specificity[matched[a].Scope] > specificity[matched[b].Scope]
		}
		return matched[a].Priority > matched[b].Priority
	})
	r := matched[0]
	price := math.Max(0, ApplyRounding(applyFormula(cost, r), r.Rounding))
	id := r.ID
	return price, &id
}

// RecomputeAllPrices 按当前规则重算全部售价（锁价优先）。
func RecomputeAllPrices() int {
	d := db.Must()
	rows, e := d.Query("SELECT goods_id, cost_price, dir_ids, platform, manual_price, price_locked FROM products")
	if e != nil {
		return 0
	}
	type item struct {
		goodsID int
		cost    float64
		dirs    []int
		plat    string
		manual  sql.NullFloat64
		locked  int
	}
	var list []item
	for rows.Next() {
		var it item
		var dirs string
		if rows.Scan(&it.goodsID, &it.cost, &dirs, &it.plat, &it.manual, &it.locked) == nil {
			it.dirs = parseDirIDs(dirs)
			list = append(list, it)
		}
	}
	rows.Close()
	for _, it := range list {
		final := 0.0
		if it.locked == 1 && it.manual.Valid {
			final = it.manual.Float64
		} else {
			final, _ = ResolvePrice(it.cost, it.goodsID, it.dirs, it.plat)
		}
		_, _ = d.Exec("UPDATE products SET final_price = ? WHERE goods_id = ?", final, it.goodsID)
	}
	return len(list)
}

// RecomputeProductPrice 重算单个商品售价。
func RecomputeProductPrice(goodsID int) {
	p := GetProduct(goodsID)
	if p == nil {
		return
	}
	final := 0.0
	if p.PriceLocked == 1 && p.ManualPrice != nil {
		final = *p.ManualPrice
	} else {
		final, _ = ResolvePrice(p.CostPrice, p.GoodsID, p.DirIDs, p.Platform)
	}
	_, _ = db.Must().Exec("UPDATE products SET final_price = ? WHERE goods_id = ?", final, goodsID)
}

// ImportProducts 从上游列表导入/上架商品。
func ImportProducts(items []model.RemoteProduct, dirID int, platform string) int {
	d := db.Must()
	ts := now()
	rootID, _ := DirRootID(dirID)
	count := 0
	for _, p := range items {
		dirs := p.DirIDs
		if len(dirs) == 0 {
			dirs = []int{dirID}
		}
		dirJSON, _ := json.Marshal(dirs)
		warranty := NormalizeWarranty(p)
		_, e := d.Exec(`INSERT INTO products(
			goods_id, name, face_value, sales_price, cost_price, final_price, goods_type, warranty_days, status,
			multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids, platform, root_category_id, enabled, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?)
			ON CONFLICT(goods_id) DO UPDATE SET
			  name=excluded.name, face_value=excluded.face_value, sales_price=excluded.sales_price,
			  cost_price=excluded.cost_price, goods_type=excluded.goods_type, warranty_days=excluded.warranty_days,
			  status=excluded.status, multiple=excluded.multiple, is_repeat=excluded.is_repeat,
			  is_ref_order=excluded.is_ref_order, is_ref_money=excluded.is_ref_money, sku_type=excluded.sku_type,
			  img_url=excluded.img_url, dir_ids=excluded.dir_ids, root_category_id=excluded.root_category_id,
			  enabled=1, updated_at=excluded.updated_at`,
			p.GoodsID, p.Name, p.FaceValue, p.SalesPrice, p.SalesPrice, p.SalesPrice, defInt(p.GoodsType, 1),
			warranty, defInt(p.Status, 1), defInt(p.Multiple, 1), p.IsRepeat, p.IsRefOrder, p.IsRefMoney,
			p.SkuType, p.ImgURL, string(dirJSON), platform, nullInt(rootID), ts)
		if e == nil {
			count++
		}
	}
	RecomputeAllPrices()
	return count
}

func defInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// NormalizeWarranty 兼容 warrantyDays 与 warrantyTime/warrantyUnit。
func NormalizeWarranty(p model.RemoteProduct) int {
	if p.WarrantyDays > 0 {
		return p.WarrantyDays
	}
	time := p.WarrantyTime
	if time <= 0 {
		return 0
	}
	switch p.WarrantyUnit {
	case 1:
		return time * 30
	case 2:
		return time * 365
	}
	return time
}

// ImportedGoodsSet 返回已导入的商品编号集合。
func ImportedGoodsSet(ids []int) map[int]bool {
	out := map[int]bool{}
	if len(ids) == 0 {
		return out
	}
	rows, e := db.Must().Query("SELECT goods_id FROM products WHERE goods_id IN (" + joinInts(ids) + ")")
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// RefreshResult 是「同步当前商品上游信息」的统计。
type RefreshResult struct {
	Total   int   `json:"total"`
	Updated int   `json:"updated"`
	Failed  int   `json:"failed"`
	Missing []int `json:"missing"`
}

// UpdateProductFromDetail 用上游详情覆盖本地商品字段（不动上下架/自定义/锁价）。
func UpdateProductFromDetail(d model.GoodsDetail, fallbackName string) {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		name = fallbackName
	}
	stock := d.StockCount
	if stock == 0 && d.StockCount == 0 {
		stock = 0
	}
	ts := now()
	_, _ = db.Must().Exec(`UPDATE products SET name=?, face_value=?, sales_price=?, cost_price=?,
		goods_type=?, warranty_days=?, status=?, multiple=?, is_repeat=?, is_ref_order=?,
		is_ref_money=?, sku_type=?, img_url=?, stock_count=?, stock_synced_at=?, updated_at=?
		WHERE goods_id=?`,
		name, d.FaceValue, d.SalesPrice, d.SalesPrice, defInt(d.GoodsType, 1), NormalizeWarranty(d.RemoteProduct),
		defInt(d.Status, 1), defInt(d.Multiple, 1), d.IsRepeat, d.IsRefOrder, d.IsRefMoney, d.SkuType,
		d.ImgURL, stock, ts, ts, d.GoodsID)
}

// UpdateStock 只更新库存。
func UpdateStock(goodsID, stock int) {
	_, _ = db.Must().Exec("UPDATE products SET stock_count = ?, stock_synced_at = ? WHERE goods_id = ?", stock, now(), goodsID)
}

// StaleStockIDs 返回 60 秒内未同步过库存的商品。
func StaleStockIDs(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	cutoff := now() - 60_000
	rows, e := db.Must().Query("SELECT goods_id FROM products WHERE goods_id IN ("+joinInts(ids)+") AND stock_synced_at < ?", cutoff)
	if e != nil {
		return nil
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// UnknownStockIDs 返回库存未知（<0）的商品。
func UnknownStockIDs() []int {
	rows, e := db.Must().Query("SELECT goods_id FROM products WHERE stock_count < 0")
	if e != nil {
		return nil
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}
