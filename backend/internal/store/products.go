package store

import (
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

func now() int64 { return time.Now().UnixMilli() }

// ===== 分类 =====

// DetectPlatform 按分类名判断平台（一级板块决定平台）。
func DetectPlatform(name string) string {
	n := strings.ToLower(name)
	for _, k := range []string{"微软", "microsoft", "outlook", "hotmail", "live.com", "office", "ms邮箱", "msn"} {
		if strings.Contains(n, k) {
			return "microsoft"
		}
	}
	for _, k := range []string{"谷歌", "google", "gmail", "goog"} {
		if strings.Contains(n, k) {
			return "google"
		}
	}
	return "other"
}

// FlatCategory 是拍平后的分类（带父级与顶级板块）。
type FlatCategory struct {
	ID       int
	Name     string
	Img      string
	ParentID *int
	Platform string
	RootID   int
}

// FlattenCategories 把上游「板块 → 子分类」树拍平。
func FlattenCategories(cats []model.RemoteCategory) []FlatCategory {
	var out []FlatCategory
	var walk func(list []model.RemoteCategory, parent *int, inherited string, root int)
	walk = func(list []model.RemoteCategory, parent *int, inherited string, root int) {
		for _, c := range list {
			platform := inherited
			rootID := root
			if parent == nil {
				platform = DetectPlatform(c.Name)
				rootID = c.ID
			}
			pid := parent
			out = append(out, FlatCategory{
				ID: c.ID, Name: c.Name, Img: c.Img,
				ParentID: pid, Platform: platform, RootID: rootID,
			})
			if len(c.Children) > 0 {
				id := c.ID
				walk(c.Children, &id, platform, rootID)
			}
		}
	}
	walk(cats, nil, "other", 0)
	return out
}

// SaveCategories 写入分类快照，并清理上游已下架且无商品引用的分类。
func SaveCategories(flat []FlatCategory) error {
	if len(flat) == 0 {
		return nil
	}
	d := db.Must()
	tx, e := d.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()

	ts := now()
	for _, c := range flat {
		if _, e := tx.Exec(`INSERT INTO categories(id, name, img, parent_id, platform, updated_at)
			VALUES(?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, img=excluded.img,
			  parent_id=excluded.parent_id, platform=excluded.platform, updated_at=excluded.updated_at`,
			c.ID, c.Name, c.Img, c.ParentID, c.Platform, ts); e != nil {
			return e
		}
	}
	keep := map[int]bool{}
	for _, c := range flat {
		keep[c.ID] = true
	}
	used := UsedDirs(tx)
	rows, e := tx.Query("SELECT id FROM categories")
	if e != nil {
		return e
	}
	var stale []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil && !keep[id] && !used[id] {
			stale = append(stale, id)
		}
	}
	rows.Close()
	for _, id := range stale {
		if _, e := tx.Exec("DELETE FROM categories WHERE id = ?", id); e != nil {
			return e
		}
	}
	return tx.Commit()
}

type execer interface {
	Query(string, ...any) (*sql.Rows, error)
}

// UsedDirs 返回被商品 dir_ids 引用的分类集合。
func UsedDirs(q execer) map[int]bool {
	out := map[int]bool{}
	rows, e := q.Query("SELECT dir_ids FROM products")
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			for _, d := range parseDirIDs(s) {
				out[d] = true
			}
		}
	}
	return out
}

func parseDirIDs(s string) []int {
	var out []int
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// DirRootID 向上找顶级分类。
func DirRootID(dirID int) (int, bool) {
	d := db.Must()
	cur := dirID
	for i := 0; i < 20; i++ {
		var parent sql.NullInt64
		var id int
		if e := d.QueryRow("SELECT id, parent_id FROM categories WHERE id = ?", cur).Scan(&id, &parent); e != nil {
			return 0, false
		}
		if !parent.Valid {
			return id, true
		}
		cur = int(parent.Int64)
	}
	return cur, true
}

// DirRootName 返回顶级分类名。
func DirRootName(dirID int) string {
	id, ok := DirRootID(dirID)
	if !ok {
		return ""
	}
	var name string
	_ = db.Must().QueryRow("SELECT name FROM categories WHERE id = ?", id).Scan(&name)
	return name
}

// PlatformOfDir 判断分类所属平台。
func PlatformOfDir(dirID int) string { return DetectPlatform(DirRootName(dirID)) }

// CategoryTree 返回分类树（后台同步页用）。
func CategoryTree() []model.CategoryNode {
	d := db.Must()
	rows, e := d.Query("SELECT id, name, parent_id FROM categories ORDER BY name")
	if e != nil {
		return []model.CategoryNode{}
	}
	type row struct {
		id     int
		name   string
		parent *int
	}
	var list []row
	for rows.Next() {
		var r row
		var p sql.NullInt64
		if rows.Scan(&r.id, &r.name, &p) == nil {
			if p.Valid {
				v := int(p.Int64)
				r.parent = &v
			}
			list = append(list, r)
		}
	}
	rows.Close()

	counts := map[int]int{}
	prows, e := d.Query("SELECT dir_ids FROM products")
	if e == nil {
		for prows.Next() {
			var s string
			if prows.Scan(&s) == nil {
				for _, x := range parseDirIDs(s) {
					counts[x]++
				}
			}
		}
		prows.Close()
	}

	byParent := map[int][]row{}
	for _, r := range list {
		key := 0
		if r.parent != nil {
			key = *r.parent
		}
		byParent[key] = append(byParent[key], r)
	}
	var build func(parent int) []model.CategoryNode
	build = func(parent int) []model.CategoryNode {
		out := []model.CategoryNode{}
		for _, r := range byParent[parent] {
			kids := build(r.id)
			out = append(out, model.CategoryNode{
				Key: strconvItoa(r.id), ID: r.id, Title: r.name,
				ProductCount: counts[r.id], Children: kids,
			})
		}
		return out
	}
	return build(0)
}

func strconvItoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ===== 商品读取 =====

func scanProduct(sc interface{ Scan(...any) error }) (model.Product, error) {
	var p model.Product
	var dirs string
	var manual, rootID sql.NullFloat64
	var rootInt sql.NullInt64
	var cname, cdesc, cimg sql.NullString
	var updated int64
	e := sc.Scan(&p.GoodsID, &p.Name, &p.FaceValue, &p.SalesPrice, &p.GoodsType, &p.WarrantyDays,
		&p.Status, &p.Multiple, &p.IsRepeat, &p.IsRefOrder, &p.IsRefMoney, &p.SkuType,
		&p.ImgURL, &dirs, &p.Platform, &updated, &p.StockCount, &cname, &cdesc, &cimg,
		&rootInt, &p.CostPrice, &p.FinalPrice, &p.Enabled, &manual, &p.PriceLocked)
	if e != nil {
		return p, e
	}
	_ = manual.Scan
	_ = rootID
	p.DirIDs = parseDirIDs(dirs)
	p.UpdatedAt = updated
	if cname.Valid {
		p.CustomName = &cname.String
	}
	if cdesc.Valid {
		p.CustomDesc = &cdesc.String
	}
	if cimg.Valid {
		p.CustomImg = &cimg.String
	}
	if rootInt.Valid {
		v := int(rootInt.Int64)
		p.RootCategoryID = &v
	}
	if manual.Valid {
		v := manual.Float64
		p.ManualPrice = &v
	}
	return p, nil
}

const productCols = `goods_id, name, face_value, sales_price, goods_type, warranty_days, status,
multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids, platform,
updated_at, stock_count, custom_name, custom_desc, custom_img, root_category_id,
cost_price, final_price, enabled, manual_price, price_locked`

// GetProduct 按 goods_id 取商品。
func GetProduct(goodsID int) *model.Product {
	row := db.Must().QueryRow("SELECT "+productCols+" FROM products WHERE goods_id = ?", goodsID)
	p, e := scanProduct(row)
	if e != nil {
		return nil
	}
	return &p
}

// ProductFilter 是后台商品筛选条件。
type ProductFilter struct {
	Keyword     string
	Platform    string
	CategoryID  int
	RootIDs     []int
	SubIDs      []int
	EnabledList []int
	Enabled     string
	Limit       int
	Offset      int
}

func buildProductWhere(f ProductFilter) (string, []any) {
	var where []string
	var args []any
	if f.Keyword != "" {
		where = append(where, "(name LIKE ? OR custom_name LIKE ? OR CAST(goods_id AS TEXT) = ?)")
		args = append(args, "%"+f.Keyword+"%", "%"+f.Keyword+"%", f.Keyword)
	}
	if f.Platform != "" {
		where = append(where, "platform = ?")
		args = append(args, f.Platform)
	}
	if f.CategoryID > 0 {
		where = append(where, "dir_ids LIKE ?")
		args = append(args, "%"+strconvItoa(f.CategoryID)+"%")
	}
	if len(f.RootIDs) > 0 {
		where = append(where, "root_category_id IN ("+joinInts(f.RootIDs)+")")
	}
	if len(f.SubIDs) > 0 {
		var ors []string
		for _, id := range f.SubIDs {
			ors = append(ors, "dir_ids LIKE '%"+strconvItoa(id)+"%'")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	if len(f.EnabledList) > 0 {
		where = append(where, "enabled IN ("+joinInts(f.EnabledList)+")")
	} else if f.Enabled == "1" || f.Enabled == "0" {
		where = append(where, "enabled = ?")
		args = append(args, f.Enabled)
	}
	if len(where) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

func joinInts(list []int) string {
	parts := make([]string, len(list))
	for i, v := range list {
		parts[i] = strconvItoa(v)
	}
	return strings.Join(parts, ",")
}

// ListProductsAdmin 后台商品列表（含分类名）。
func ListProductsAdmin(f ProductFilter) []model.AdminProduct {
	d := db.Must()
	where, args := buildProductWhere(f)
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	q := "SELECT " + productCols + " FROM products " + where +
		" ORDER BY root_category_id ASC, final_price ASC LIMIT ? OFFSET ?"
	// 注意：连接池只有 1 个连接，必须先取完分类名再开结果集，
	// 否则 rows 未关闭时再次查询会永久阻塞。
	cats := categoryNames()
	rows, e := d.Query(q, append(args, limit, f.Offset)...)
	if e != nil {
		return []model.AdminProduct{}
	}
	defer rows.Close()
	out := []model.AdminProduct{}
	for rows.Next() {
		p, e := scanProduct(rows)
		if e != nil {
			continue
		}
		ap := model.AdminProduct{Product: p, RootName: "未分类"}
		var rootID int
		if p.RootCategoryID != nil {
			rootID = *p.RootCategoryID
		} else if len(p.DirIDs) > 0 {
			if r, ok := DirRootID(p.DirIDs[0]); ok {
				rootID = r
			}
		}
		if name, ok := cats[rootID]; ok {
			ap.RootName = name
		}
		if len(p.DirIDs) > 0 {
			if name, ok := cats[p.DirIDs[0]]; ok && p.DirIDs[0] != rootID {
				ap.SubName = name
			}
		}
		out = append(out, ap)
	}
	return out
}

func categoryNames() map[int]string {
	out := map[int]string{}
	rows, e := db.Must().Query("SELECT id, name FROM categories")
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var name string
		if rows.Scan(&id, &name) == nil {
			out[id] = name
		}
	}
	return out
}

// CountProductsAdmin 统计后台商品数。
func CountProductsAdmin(f ProductFilter) int {
	where, args := buildProductWhere(f)
	var c int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM products "+where, args...).Scan(&c)
	return c
}

// ListProductIDs 返回当前筛选下的全部 goods_id。
func ListProductIDs(f ProductFilter) []int {
	where, args := buildProductWhere(f)
	rows, e := db.Must().Query("SELECT goods_id FROM products "+where, args...)
	if e != nil {
		return []int{}
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// DeleteProducts 删除本地商品。
func DeleteProducts(ids []int) int {
	d := db.Must()
	for _, id := range ids {
		_, _ = d.Exec("DELETE FROM products WHERE goods_id = ?", id)
	}
	return len(ids)
}

// SetProductsEnabled 批量上下架。
func SetProductsEnabled(ids []int, enabled bool) int {
	d := db.Must()
	v := 0
	if enabled {
		v = 1
	}
	ts := now()
	for _, id := range ids {
		_, _ = d.Exec("UPDATE products SET enabled = ?, updated_at = ? WHERE goods_id = ?", v, ts, id)
	}
	return len(ids)
}

// SetProductManualPrice 手动锁价 / 解锁。
func SetProductManualPrice(goodsID int, price *float64, locked bool) {
	d := db.Must()
	ts := now()
	if locked && price != nil {
		_, _ = d.Exec("UPDATE products SET manual_price = ?, price_locked = 1, final_price = ?, updated_at = ? WHERE goods_id = ?",
			*price, *price, ts, goodsID)
		return
	}
	_, _ = d.Exec("UPDATE products SET manual_price = NULL, price_locked = 0, updated_at = ? WHERE goods_id = ?", ts, goodsID)
}

// CountProducts 商品总数。
func CountProducts() int {
	var c int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM products").Scan(&c)
	return c
}

// CustomizedProduct 是后台「商品自定义信息」列表项。
type CustomizedProduct struct {
	GoodsID    int     `json:"goodsId"`
	Name       string  `json:"name"`
	Platform   string  `json:"platform"`
	CustomName *string `json:"customName"`
	CustomDesc *string `json:"customDesc"`
	CustomImg  *string `json:"customImg"`
}

// ListCustomizedProducts 列出已设置自定义信息的商品。
func ListCustomizedProducts(keyword string, limit int) []CustomizedProduct {
	if limit <= 0 {
		limit = 200
	}
	q := "SELECT goods_id, name, platform, custom_name, custom_desc, custom_img FROM products WHERE custom_name IS NOT NULL AND custom_name <> ''"
	var args []any
	if keyword != "" {
		q += " AND (name LIKE ? OR custom_name LIKE ? OR CAST(goods_id AS TEXT) = ?)"
		args = append(args, "%"+keyword+"%", "%"+keyword+"%", keyword)
	}
	q += " ORDER BY updated_at DESC LIMIT ?"
	args = append(args, limit)
	rows, e := db.Must().Query(q, args...)
	if e != nil {
		return []CustomizedProduct{}
	}
	defer rows.Close()
	out := []CustomizedProduct{}
	for rows.Next() {
		var it CustomizedProduct
		var cn, cd, ci sql.NullString
		if rows.Scan(&it.GoodsID, &it.Name, &it.Platform, &cn, &cd, &ci) == nil {
			if cn.Valid {
				it.CustomName = &cn.String
			}
			if cd.Valid {
				it.CustomDesc = &cd.String
			}
			if ci.Valid {
				it.CustomImg = &ci.String
			}
			out = append(out, it)
		}
	}
	return out
}

// SetProductCustomInfo 设置/清除商品自定义信息。
func SetProductCustomInfo(goodsID int, name, desc, img string, clear bool) bool {
	d := db.Must()
	var exists int
	if e := d.QueryRow("SELECT 1 FROM products WHERE goods_id = ?", goodsID).Scan(&exists); e != nil {
		return false
	}
	if clear {
		_, _ = d.Exec("UPDATE products SET custom_name = NULL, custom_desc = NULL, custom_img = NULL, updated_at = ? WHERE goods_id = ?", now(), goodsID)
		return true
	}
	var n, ds, im any
	if name != "" {
		n = name
	}
	if desc != "" {
		ds = desc
	}
	if img != "" {
		im = img
	}
	_, _ = d.Exec("UPDATE products SET custom_name = ?, custom_desc = ?, custom_img = ?, updated_at = ? WHERE goods_id = ?",
		n, ds, im, now(), goodsID)
	return true
}

// ===== 商城前台 =====

// StoreBoards 返回有在售商品的板块卡片。
func StoreBoards() []model.StoreBoard {
	d := db.Must()
	rows, e := d.Query(`SELECT c.id, c.name, c.img, COUNT(p.goods_id) AS total,
		MIN(p.final_price) AS minPrice, MAX(p.final_price) AS maxPrice
		FROM products p JOIN categories c ON c.id = p.root_category_id
		WHERE p.enabled = 1 AND p.status = 1
		GROUP BY c.id ORDER BY total DESC, c.name ASC`)
	if e != nil {
		return []model.StoreBoard{}
	}
	var out []model.StoreBoard
	for rows.Next() {
		var b model.StoreBoard
		var minP, maxP sql.NullFloat64
		if rows.Scan(&b.ID, &b.Name, &b.Img, &b.Total, &minP, &maxP) == nil {
			if minP.Valid {
				v := minP.Float64
				b.MinPrice = &v
			}
			if maxP.Valid {
				v := maxP.Float64
				b.MaxPrice = &v
			}
			out = append(out, b)
		}
	}
	rows.Close()

	cats := map[int]model.StoreSubCategory{}
	parents := map[int]int{}
	crows, e := d.Query("SELECT id, name, img, parent_id FROM categories")
	if e == nil {
		for crows.Next() {
			var id, parent int
			var name, img string
			var p sql.NullInt64
			if crows.Scan(&id, &name, &img, &p) == nil {
				cats[id] = model.StoreSubCategory{ID: id, Name: name, Img: img}
				if p.Valid {
					parent = int(p.Int64)
					parents[id] = parent
				}
			}
		}
		crows.Close()
	}
	used := UsedDirs(d)
	for i := range out {
		out[i].Children = []model.StoreSubCategory{}
		for id, parent := range parents {
			if parent != out[i].ID || !used[id] {
				continue
			}
			out[i].Children = append(out[i].Children, cats[id])
		}
		sort.Slice(out[i].Children, func(a, b int) bool { return out[i].Children[a].Name < out[i].Children[b].Name })
	}
	if out == nil {
		out = []model.StoreBoard{}
	}
	return out
}

// StoreProductQuery 是商城商品查询条件。
type StoreProductQuery struct {
	RootID   int
	DirID    int
	Keyword  string
	Sort     string
	Page     int
	PageSize int
}

// ListStoreProducts 商城商品列表。
func ListStoreProducts(q StoreProductQuery) ([]model.Product, int) {
	d := db.Must()
	where := []string{"enabled = 1", "status = 1"}
	var args []any
	if q.RootID > 0 {
		where = append(where, "root_category_id = ?")
		args = append(args, q.RootID)
	}
	if q.DirID > 0 {
		where = append(where, "dir_ids LIKE ?")
		args = append(args, "%"+strconvItoa(q.DirID)+"%")
	}
	if q.Keyword != "" {
		where = append(where, "(name LIKE ? OR custom_name LIKE ? OR CAST(goods_id AS TEXT) = ?)")
		args = append(args, "%"+q.Keyword+"%", "%"+q.Keyword+"%", q.Keyword)
	}
	sortExpr := "COALESCE(NULLIF(custom_name, ''), name) ASC"
	switch q.Sort {
	case "price-asc":
		sortExpr = "final_price ASC"
	case "price-desc":
		sortExpr = "final_price DESC"
	case "stock-desc":
		sortExpr = "stock_count DESC"
	}
	whereSQL := "WHERE " + strings.Join(where, " AND ")
	page, size := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	rows, e := d.Query("SELECT "+productCols+" FROM products "+whereSQL+
		" ORDER BY CASE WHEN stock_count > 0 THEN 0 WHEN stock_count = 0 THEN 2 ELSE 1 END, "+sortExpr+
		" LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	items := []model.Product{}
	if e == nil {
		for rows.Next() {
			if p, e := scanProduct(rows); e == nil {
				items = append(items, p)
			}
		}
		rows.Close()
	}
	var total int
	_ = d.QueryRow("SELECT COUNT(*) FROM products "+whereSQL, args...).Scan(&total)
	return items, total
}

// ProductFilterOptions 是后台筛选下拉数据。
type ProductFilterOptions struct {
	Roots  []map[string]any `json:"roots"`
	Subs   []map[string]any `json:"subs"`
	Status []map[string]any `json:"status"`
}

// GetProductFilterOptions 返回后台商品筛选选项。
func GetProductFilterOptions() ProductFilterOptions {
	d := db.Must()
	out := ProductFilterOptions{Roots: []map[string]any{}, Subs: []map[string]any{}, Status: []map[string]any{}}

	if rows, e := d.Query(`SELECT c.id, c.name, COUNT(*) AS count FROM products p
		JOIN categories c ON c.id = p.root_category_id GROUP BY c.id ORDER BY c.name`); e == nil {
		for rows.Next() {
			var id, count int
			var name string
			if rows.Scan(&id, &name, &count) == nil {
				out.Roots = append(out.Roots, map[string]any{"id": id, "name": name, "count": count})
			}
		}
		rows.Close()
	}

	names := categoryNames()
	parentOf := map[int]int{}
	if rows, e := d.Query("SELECT id, parent_id FROM categories WHERE parent_id IS NOT NULL"); e == nil {
		for rows.Next() {
			var id, parent int
			if rows.Scan(&id, &parent) == nil {
				parentOf[id] = parent
			}
		}
		rows.Close()
	}
	subCount := map[int]int{}
	if rows, e := d.Query("SELECT dir_ids FROM products"); e == nil {
		for rows.Next() {
			var s string
			if rows.Scan(&s) == nil {
				for _, id := range parseDirIDs(s) {
					subCount[id]++
				}
			}
		}
		rows.Close()
	}
	for id, count := range subCount {
		name, ok := names[id]
		if !ok {
			continue
		}
		root, isSub := parentOf[id]
		if !isSub {
			continue
		}
		out.Subs = append(out.Subs, map[string]any{"id": id, "name": name, "rootId": root, "count": count})
	}
	sort.Slice(out.Subs, func(a, b int) bool {
		return out.Subs[a]["name"].(string) < out.Subs[b]["name"].(string)
	})

	if rows, e := d.Query("SELECT enabled, COUNT(*) AS count FROM products GROUP BY enabled ORDER BY enabled DESC"); e == nil {
		for rows.Next() {
			var enabled, count int
			if rows.Scan(&enabled, &count) == nil {
				name := "下架"
				if enabled == 1 {
					name = "上架"
				}
				out.Status = append(out.Status, map[string]any{"id": enabled, "name": name, "count": count})
			}
		}
		rows.Close()
	}
	return out
}

// PlatformCounts 按平台统计商品数。
func PlatformCounts() map[string]int {
	out := map[string]int{"microsoft": 0, "google": 0, "other": 0}
	rows, e := db.Must().Query("SELECT platform, COUNT(*) FROM products GROUP BY platform")
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var c int
		if rows.Scan(&p, &c) == nil {
			out[p] = c
		}
	}
	return out
}

// CountStoreProducts 商城在售商品总数（启用且上架）。
func CountStoreProducts() int {
	var n int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM products WHERE enabled = 1 AND status = 1").Scan(&n)
	return n
}
