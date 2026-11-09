// Command split_self 把「由上游商品迁移而来」的自营商品拆成真正的独立自营商品。
//
// 背景
//
//	早期 migrate_self 把 5500 / 5768 / 5769 这几个上游商品直接打上
//	fulfillment='self'，并把 fallback_goods_id 指向自身，靠「本地没卡密就
//	走上游采购」的方式发货。兜底能力下线后，这些商品既没有本地库存、
//	又不能再调上游，兑换码全部无法核销，且它们的售价/库存仍被上游同步
//	影响，无法算作独立自营商品。
//
// 本命令做的事（单个商品，全部在一个事务内）
//
//  1. 在自营号段（>=900000）新建一个商品，复制旧商品的展示信息
//     （名称/描述/图片/售价/成本价/质保/上下架）与自营分类归属；
//  2. 把旧商品的本地卡密（self_stock）整行迁移到新商品；
//  3. 把旧商品名下的兑换码（redeem_codes.goods_id）改指新商品；
//  4. 删除旧商品行（orders / cards 里的历史记录不受影响，
//     订单表自带 goods_name，商品列表用 LEFT JOIN，不会因缺行报错）。
//
// 特意「不做」的事
//
//   - 不新建/删除 self_stock 行，只改 goods_id，卡密内容与售出状态原样保留；
//   - 不动已生成的订单：orders.goods_id 留着旧号，goods_name 也照旧，
//     历史凭证可追溯（本命令只打印数量用于核对）；
//   - 不给新商品造库存：库存多少就是迁移过来的多少，不足由库存预警邮件提醒。
//
// 用法
//
//	# 先看会做什么（只读，不做任何修改）
//	FAKA_DB_PATH=/path/faka.db go run ./cmd/split_self -ids 5500,5768,5769 -dry-run
//
//	# 真正执行
//	FAKA_DB_PATH=/path/faka.db go run ./cmd/split_self -ids 5500,5768,5769
//
//	# 只处理单个商品
//	FAKA_DB_PATH=/path/faka.db go run ./cmd/split_self -ids 5769
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
)

// sourceProduct 是待拆分的旧商品快照。
type sourceProduct struct {
	GoodsID     int
	Name        string
	FaceValue   float64
	SalesPrice  float64
	FinalPrice  float64
	CostPrice   float64
	GoodsType   int
	Warranty    int
	Status      int
	Multiple    int
	IsRepeat    int
	IsRefOrder  int
	IsRefMoney  int
	SkuType     int
	ImgURL      string
	DirIDs      string
	Platform    string
	Enabled     int
	StockCount  int
	CustomName  string
	CustomDesc  string
	CustomImg   string
	Fulfillment string
	Fallback    int
}

// plan 是一个商品的迁移计划。
type plan struct {
	Src          sourceProduct
	NewGoodsID   int
	StockRows    int    // 需迁移的 self_stock 行数
	StockAvail   int    // 其中可用的
	CodeRows     int    // 需改指的兑换码行数
	CodeUnused   int    // 其中未使用的
	OrderRows    int    // 旧商品的历史订单数（仅用于核对，不改）
	NameConflict string // 新名称若与现有自营商品重名，给出提示
}

func main() {
	var (
		ids    = flag.String("ids", "", "要拆分的旧商品 ID，逗号分隔（必填）")
		prefix = flag.String("prefix", "", "新商品名称前缀（默认「自营-」）")
		dryRun = flag.Bool("dry-run", false, "只打印迁移计划，不做任何修改")
	)
	flag.Parse()

	goodsIDs, e := parseIDs(*ids)
	if e != nil || len(goodsIDs) == 0 {
		fmt.Fprintf(os.Stderr, "参数 -ids 无效（示例：-ids 5500,5768,5769）: %v\n", e)
		os.Exit(2)
	}
	namePrefix := *prefix
	if namePrefix == "" {
		namePrefix = "自营-"
	}

	// db.Open 会建表/补列/seed，确保 schema 最新。
	if _, e := db.Open(); e != nil {
		fmt.Fprintf(os.Stderr, "打开数据库失败 (%s): %v\n", db.DBPath(), e)
		os.Exit(2)
	}
	d := db.Must()

	fmt.Printf("[split_self] 数据库: %s\n", db.DBPath())
	fmt.Printf("[split_self] 待拆分商品: %v  名称前缀: %q\n\n", goodsIDs, namePrefix)

	// 确保自营分类存在（新商品要挂到它下面）。
	store.EnsureSelfCategory()
	// 1) 规划：只读，逐项算出要做什么。
	plans := make([]plan, 0, len(goodsIDs))
	nextID, e := maxSelfGoodsID(d)
	if e != nil {
		fmt.Fprintf(os.Stderr, "读取自营号段失败: %v\n", e)
		os.Exit(2)
	}
	for _, gid := range goodsIDs {
		src, e := loadSource(d, gid)
		if e == sql.ErrNoRows {
			fmt.Printf("  ! 商品 %d 不存在，跳过\n", gid)
			continue
		}
		if e != nil {
			fmt.Fprintf(os.Stderr, "读取商品 %d 失败: %v\n", gid, e)
			os.Exit(2)
		}
		nextID++
		p := plan{Src: src, NewGoodsID: nextID}
		_ = d.QueryRow("SELECT COUNT(*), COALESCE(SUM(status='available'),0) FROM self_stock WHERE goods_id=?", gid).
			Scan(&p.StockRows, &p.StockAvail)
		_ = d.QueryRow("SELECT COUNT(*), COALESCE(SUM(status='unused'),0) FROM redeem_codes WHERE goods_id=?", gid).
			Scan(&p.CodeRows, &p.CodeUnused)
		_ = d.QueryRow("SELECT COUNT(*) FROM orders WHERE goods_id=?", gid).Scan(&p.OrderRows)
		newName := namePrefix + src.displayName()
		if existsSelfName(d, newName) {
			p.NameConflict = "已有同名自营商品，仍会创建（名称不做唯一约束）"
		}
		plans = append(plans, p)
	}

	if len(plans) == 0 {
		fmt.Println("[split_self] 没有可拆分的商品，退出。")
		return
	}

	printPlans(plans)

	if *dryRun {
		fmt.Println("\n[dry-run] 未做任何修改。去掉 -dry-run 正式执行。")
		return
	}

	// 2) 执行：每个商品一个事务，任一失败整体回滚。
	for _, p := range plans {
		if e := applyOne(d, p, namePrefix); e != nil {
			fmt.Fprintf(os.Stderr, "\n[split_self] 商品 %d 迁移失败，已回滚: %v\n", p.Src.GoodsID, e)
			os.Exit(1)
		}
		fmt.Printf("  ✓ %d → %d 完成（迁移库存 %d 行、兑换码 %d 行，删除旧商品）\n",
			p.Src.GoodsID, p.NewGoodsID, p.StockRows, p.CodeRows)
	}
	fmt.Println("\n[split_self] 全部完成。历史订单保留在 orders 表，goods_name 未变。")
}

// maxSelfGoodsID 返回当前自营号段里的最大编号。
func maxSelfGoodsID(d *sql.DB) (int, error) {
	var max int
	e := d.QueryRow(
		"SELECT COALESCE(MAX(goods_id),0) FROM products WHERE goods_id >= ? OR platform='self'",
		store.SelfGoodsIDStart).Scan(&max)
	if e != nil {
		return 0, e
	}
	if max < store.SelfGoodsIDStart {
		max = store.SelfGoodsIDStart
	}
	return max, nil
}

func loadSource(d *sql.DB, gid int) (sourceProduct, error) {
	var s sourceProduct
	e := d.QueryRow(`SELECT goods_id, name, face_value, sales_price, final_price, cost_price,
			goods_type, COALESCE(warranty_days,0), COALESCE(status,1), COALESCE(multiple,1),
			COALESCE(is_repeat,0), COALESCE(is_ref_order,0), COALESCE(is_ref_money,0), COALESCE(sku_type,0),
			COALESCE(img_url,''), COALESCE(dir_ids,'[]'), COALESCE(platform,''),
			COALESCE(enabled,0), COALESCE(stock_count,0),
			COALESCE(custom_name,''), COALESCE(custom_desc,''), COALESCE(custom_img,''),
			COALESCE(fulfillment,'upstream'), COALESCE(fallback_goods_id,0)
		FROM products WHERE goods_id = ?`, gid).
		Scan(&s.GoodsID, &s.Name, &s.FaceValue, &s.SalesPrice, &s.FinalPrice, &s.CostPrice,
			&s.GoodsType, &s.Warranty, &s.Status, &s.Multiple,
			&s.IsRepeat, &s.IsRefOrder, &s.IsRefMoney, &s.SkuType,
			&s.ImgURL, &s.DirIDs, &s.Platform,
			&s.Enabled, &s.StockCount,
			&s.CustomName, &s.CustomDesc, &s.CustomImg,
			&s.Fulfillment, &s.Fallback)
	return s, e
}

func (s sourceProduct) displayName() string {
	if v := strings.TrimSpace(s.CustomName); v != "" {
		return v
	}
	return s.Name
}

func existsSelfName(d *sql.DB, name string) bool {
	var n int
	_ = d.QueryRow(
		"SELECT COUNT(*) FROM products WHERE fulfillment='self' AND (custom_name=? OR name=?)",
		name, name).Scan(&n)
	return n > 0
}

func printPlans(plans []plan) {
	fmt.Println("=== 迁移计划 ===")
	for _, p := range plans {
		fmt.Printf("\n旧商品 #%d  %s\n", p.Src.GoodsID, p.Src.displayName())
		fmt.Printf("  售价 %.4f  成本 %.4f  上架 %d  平台 %s  fulfillment=%s  fallback=%d\n",
			p.Src.FinalPrice, p.Src.CostPrice, p.Src.Enabled, p.Src.Platform, p.Src.Fulfillment, p.Src.Fallback)
		fmt.Printf("  → 新商品 #%d  名称「自营-%s」\n", p.NewGoodsID, p.Src.displayName())
		fmt.Printf("  迁移本地卡密 %d 行（其中可用 %d 张）\n", p.StockRows, p.StockAvail)
		fmt.Printf("  改指兑换码 %d 行（其中未使用 %d 张）\n", p.CodeRows, p.CodeUnused)
		fmt.Printf("  历史订单 %d 笔（保留原 goods_id 与 goods_name，不改）\n", p.OrderRows)
		if p.NameConflict != "" {
			fmt.Printf("  提示: %s\n", p.NameConflict)
		}
	}
}

// applyOne 在单个事务内完成「建新商品 → 迁库存 → 改兑换码 → 删旧商品」。
func applyOne(d *sql.DB, p plan, namePrefix string) error {
	tx, e := d.Begin()
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()
	catID := store.SelfCategoryID
	s := p.Src

	// 确认新号未被占用（并发或历史残留时自增一格）。
	var taken int
	if e := tx.QueryRow("SELECT COUNT(*) FROM products WHERE goods_id=?", p.NewGoodsID).Scan(&taken); e != nil {
		return e
	}
	if taken > 0 {
		return fmt.Errorf("新商品编号 %d 已被占用，请重跑以重新分配", p.NewGoodsID)
	}

	newName := namePrefix + s.displayName()
	// 新商品一律是「纯自营」：fulfillment='self'、platform='self'、
	// fallback_goods_id=0（不再与上游有任何关联），并锁价到迁移时的售价。
	if _, e := tx.Exec(`INSERT INTO products(
			goods_id, name, face_value, sales_price, cost_price, final_price,
			goods_type, warranty_days, status, multiple, is_repeat, is_ref_order, is_ref_money, sku_type,
			img_url, dir_ids, platform, root_category_id, updated_at, stock_count,
			enabled, manual_price, price_locked,
			custom_name, custom_desc, custom_img,
			fulfillment, fallback_goods_id)
		VALUES(?,?,?,?,?,?, ?,?,?,?,?,?,?,?, ?,?,'self',?,?,?, ?,?,1, ?,?,?, 'self',0)`,
		p.NewGoodsID, newName, s.FaceValue, s.SalesPrice, s.CostPrice, s.FinalPrice,
		s.GoodsType, s.Warranty, s.Status, s.Multiple, s.IsRepeat, s.IsRefOrder, s.IsRefMoney, s.SkuType,
		s.ImgURL, s.DirIDs, catID, now, p.StockAvail,
		s.Enabled, s.FinalPrice,
		newName, s.CustomDesc, s.CustomImg); e != nil {
		return fmt.Errorf("创建新商品: %w", e)
	}

	// 迁移本地卡密：只改归属，内容/状态/售出记录原样保留。
	if _, e := tx.Exec("UPDATE self_stock SET goods_id=? WHERE goods_id=?", p.NewGoodsID, s.GoodsID); e != nil {
		return fmt.Errorf("迁移卡密: %w", e)
	}

	// 兑换码改指新商品（含已使用的：它们的订单号与核销记录都在别的表里，改归属不影响追溯）。
	if _, e := tx.Exec("UPDATE redeem_codes SET goods_id=? WHERE goods_id=?", p.NewGoodsID, s.GoodsID); e != nil {
		return fmt.Errorf("迁移兑换码: %w", e)
	}

	// 删掉旧商品行。orders / cards / redeem_codes 均无外键，且订单自带
	// goods_name，列表接口用 LEFT JOIN，因此不会报错、历史仍可查。
	if _, e := tx.Exec("DELETE FROM products WHERE goods_id=?", s.GoodsID); e != nil {
		return fmt.Errorf("删除旧商品: %w", e)
	}

	return tx.Commit()
}

func parseIDs(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("不能为空")
	}
	out := make([]int, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, e := strconv.Atoi(part)
		if e != nil {
			return nil, fmt.Errorf("%q 不是合法商品编号", part)
		}
		out = append(out, n)
	}
	return out, nil
}
