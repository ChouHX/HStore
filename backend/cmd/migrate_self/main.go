// Command migrate_self 把指定的上游商品迁移为「自营商品」，并为其绑定 fallback 上游采购源。
//
// 迁移做的事（全部幂等，可以重复执行）：
//  1. products.fulfillment = 'self'         —— 打上自营标记（也是同步保护的开关）；
//  2. products.root_category_id = 900001    —— 挂到自营分类「自营商品」下；
//  3. products.fallback_goods_id = 自身 ID  —— 本地 self_stock 没有卡密时，
//     兑换自动回落到上游同一 goods_id 采购。
//
// 特意「不做」的事：
//   - 不改 platform 字段。上游同步的清理条件是
//     `DELETE FROM products WHERE platform IN ('microsoft','google')
//     AND COALESCE(fulfillment,'upstream') <> 'self'`，
//     ON CONFLICT 更新也不会写 fulfillment / fallback_goods_id，
//     所以只要 fulfillment='self'，同步既不会删它、也不会把它改回 upstream。
//     保留 platform='microsoft' 同时还能继续从上游拉价格/库存/详情。
//   - 不往 self_stock 里造库存。这三个商品目前没有本地卡密，
//     self_stock 为空时会自动全额走 fallback 上游采购，这是预期行为。
//   - 不改价格 / 上下架 / custom_*，避免覆盖管理员的设置。
//
// ⚠ 关于上游全量同步（已修复，无需重跑）：
//
//	早期版本 ON CONFLICT(products.goods_id) DO UPDATE 的字段列表里**有** root_category_id
//	和 final_price，每次上游同步都会把这三行的板块归属改回 17、售价改回上游价。
//	现已在 catalog.go 的同步 SQL 中对 fulfillment='self' 的商品加 CASE 保护，
//	root_category_id / final_price 均保留原值，因此同步之后不需要重跑本命令。
//	本命令仍然幂等，重复执行没有副作用。
//
// 用法：
//
//	# 只看当前状态，不做修改
//	FAKA_DB_PATH=/tmp/selftest/test.db go run ./cmd/migrate_self -dry-run
//
//	# 执行迁移
//	FAKA_DB_PATH=/tmp/selftest/test.db go run ./cmd/migrate_self
//
//	# 迁移后自检：确认几个商品仍然是自营（同步之后跑一次）
//	FAKA_DB_PATH=/tmp/selftest/test.db go run ./cmd/migrate_self -verify
//
//	# 自定义商品列表（默认 5768,5769,5500）
//	go run ./cmd/migrate_self -ids 5768,5769,5500
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
)

// defaultGoodsIDs 是需要迁移为自营的微软商品。
// fallback_goods_id 指回自身：上游同一 goods_id 仍可采购。
var defaultGoodsIDs = []int{5768, 5769, 5500}

// row 是 products 表里我们要看/要改的字段快照。
type row struct {
	GoodsID         int
	Name            string
	Platform        string
	Fulfillment     string
	FallbackGoodsID int
	RootCategoryID  int
	Enabled         int
	Status          int
	SalesPrice      float64
	CostPrice       float64
	FinalPrice      float64
	StockCount      int
	SelfStock       int // self_stock 中状态为 available 的条数
}

func main() {
	var (
		ids    = flag.String("ids", "", "要迁移的商品 ID，逗号分隔（默认 5768,5769,5500）")
		dryRun = flag.Bool("dry-run", false, "只打印当前状态，不做任何修改")
		verify = flag.Bool("verify", false, "自检模式：确认商品仍为自营，不做修改；不满足时退出码 1")
	)
	flag.Parse()

	goodsIDs, e := parseIDs(*ids)
	if e != nil {
		fmt.Fprintf(os.Stderr, "解析 -ids 失败: %v\n", e)
		os.Exit(2)
	}

	// db.Open 会顺带建表/补列/seed，迁移前先确保 schema 是最新的。
	if _, e := db.Open(); e != nil {
		fmt.Fprintf(os.Stderr, "打开数据库失败 (%s): %v\n", db.DBPath(), e)
		os.Exit(2)
	}

	fmt.Printf("[migrate_self] 数据库: %s\n", db.DBPath())
	fmt.Printf("[migrate_self] 目标商品: %v  自营分类: %d (%s)\n\n",
		goodsIDs, store.SelfCategoryID, store.SelfCategoryName)

	before := snapshot(goodsIDs)
	printTable("执行前", before)

	switch {
	case *verify:
		os.Exit(runVerify(before))
	case *dryRun:
		fmt.Println("\n[migrate_self] -dry-run：未做任何修改。")
		return
	}

	changed, e := migrate(goodsIDs)
	if e != nil {
		fmt.Fprintf(os.Stderr, "\n[migrate_self] 迁移失败: %v\n", e)
		os.Exit(1)
	}

	after := snapshot(goodsIDs)
	fmt.Println()
	printTable("执行后", after)
	fmt.Printf("\n[migrate_self] 已更新 %d 行（重复执行时该值会变成 0，属正常幂等行为）。\n", changed)

	// 迁移后自检：不通过说明迁移没生效。
	if code := runVerify(after); code != 0 {
		os.Exit(code)
	}
}

func parseIDs(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		out := append([]int(nil), defaultGoodsIDs...)
		sort.Ints(out)
		return out, nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, e := strconv.Atoi(part)
		if e != nil {
			return nil, fmt.Errorf("非法商品 ID %q", part)
		}
		if v <= 0 {
			return nil, fmt.Errorf("商品 ID 必须为正数: %d", v)
		}
		out = append(out, v)
	}
	sort.Ints(out)
	return out, nil
}

// migrate 在单个事务里完成迁移。全部是幂等 UPDATE：
// 已经迁移过的商品再跑一次不会有任何副作用。
func migrate(goodsIDs []int) (int, error) {
	d := db.Must()
	tx, e := d.Begin()
	if e != nil {
		return 0, e
	}
	defer func() { _ = tx.Rollback() }()

	// 1) 确保自营分类存在（上游同步会跳过 platform='self' 的分类）。
	now := time.Now().UnixMilli()
	if _, e := tx.Exec(`INSERT INTO categories(id, name, img, parent_id, platform, updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, platform='self'`,
		store.SelfCategoryID, store.SelfCategoryName, "", nil, "self", now); e != nil {
		return 0, fmt.Errorf("写入自营分类: %w", e)
	}

	// 2) 只动这三行的自营相关字段。
	//    - fulfillment='self'  → 上游同步的清理条件会跳过它（关键保护）；
	//    - fallback_goods_id=goods_id → 本地无卡密时全额走上游采购；
	//    - root_category_id=900001 → 挂到自营板块。
	//    注意 SQLite 的 UPDATE 里 `fallback_goods_id = goods_id` 取的是本行原值，正是我们想要的。
	//    这里不改 platform / 价格 / 上下架 / custom_* 字段。
	stmt := `UPDATE products
		SET fulfillment = 'self',
		    fallback_goods_id = goods_id,
		    root_category_id = ?
		WHERE goods_id = ?
		  AND ( COALESCE(fulfillment, '') <> 'self'
		        OR COALESCE(fallback_goods_id, 0) <> goods_id
		        OR COALESCE(root_category_id, 0) <> ? )`
	total := 0
	for _, gid := range goodsIDs {
		var exists int
		if e := tx.QueryRow("SELECT COUNT(*) FROM products WHERE goods_id=?", gid).Scan(&exists); e != nil {
			return 0, fmt.Errorf("查询商品 %d: %w", gid, e)
		}
		if exists == 0 {
			fmt.Printf("[migrate_self] 警告: 商品 #%d 不在 products 表里，跳过（需要先同步一次上游）\n", gid)
			continue
		}
		// WHERE 里带上「值已经正确就不改」的条件，重复执行时 RowsAffected 为 0。
		res, e := tx.Exec(stmt, store.SelfCategoryID, gid, store.SelfCategoryID)
		if e != nil {
			return 0, fmt.Errorf("迁移商品 %d: %w", gid, e)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			total += int(n)
		}
	}

	if e := tx.Commit(); e != nil {
		return 0, e
	}
	return total, nil
}

func snapshot(goodsIDs []int) []row {
	d := db.Must()
	var out []row
	for _, gid := range goodsIDs {
		var r row
		e := d.QueryRow(`SELECT goods_id, name, COALESCE(platform,''), COALESCE(fulfillment,''),
			COALESCE(fallback_goods_id,0), COALESCE(root_category_id,0), COALESCE(enabled,0),
			COALESCE(status,0), COALESCE(sales_price,0), COALESCE(cost_price,0),
			COALESCE(final_price,0), COALESCE(stock_count,0)
			FROM products WHERE goods_id=?`, gid).Scan(
			&r.GoodsID, &r.Name, &r.Platform, &r.Fulfillment, &r.FallbackGoodsID,
			&r.RootCategoryID, &r.Enabled, &r.Status, &r.SalesPrice, &r.CostPrice,
			&r.FinalPrice, &r.StockCount)
		if e != nil {
			r.GoodsID = gid
			r.Name = "<不存在>"
			r.Fulfillment = "<missing>"
			out = append(out, r)
			continue
		}
		_ = d.QueryRow("SELECT COUNT(*) FROM self_stock WHERE goods_id=? AND status='available'", gid).
			Scan(&r.SelfStock)
		out = append(out, r)
	}
	return out
}

func printTable(title string, rows []row) {
	fmt.Printf("=== %s ===\n", title)
	fmt.Printf("%-8s %-6s %-10s %-9s %-10s %-8s %-5s %-9s %-8s %-10s %s\n",
		"goods_id", "库存", "platform", "fulfill", "fallback", "root_cat", "启用", "售价", "stock_cnt", "self_stock", "name")
	for _, r := range rows {
		name := r.Name
		if rs := []rune(name); len(rs) > 28 {
			name = string(rs[:28]) + "…"
		}
		fmt.Printf("%-8d %-6d %-10s %-9s %-10d %-8d %-5d %-9.2f %-8d %-10d %s\n",
			r.GoodsID, r.Enabled, orDash(r.Platform), orDash(r.Fulfillment),
			r.FallbackGoodsID, r.RootCategoryID, r.Enabled, r.SalesPrice, r.StockCount,
			r.SelfStock, name)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// runVerify 自检：这些商品必须仍然存在且是自营，fallback 指回自身。
// 上游同步跑完之后用它确认商品没被改回 upstream 或被删掉。
func runVerify(rows []row) int {
	fmt.Println("\n=== 自检（上游同步之后也必须保持） ===")
	bad := 0
	for _, r := range rows {
		if r.Fulfillment == "<missing>" {
			fmt.Printf("  ✗ #%d 不存在（可能尚未同步上游商品）\n", r.GoodsID)
			bad++
			continue
		}
		var problems []string
		if r.Fulfillment != "self" {
			problems = append(problems, "fulfillment="+r.Fulfillment+"（应为 self）")
		}
		if r.RootCategoryID != store.SelfCategoryID {
			problems = append(problems, fmt.Sprintf("root_category_id=%d（应为 %d）", r.RootCategoryID, store.SelfCategoryID))
		}
		if r.FallbackGoodsID != r.GoodsID {
			problems = append(problems, fmt.Sprintf("fallback_goods_id=%d（应为 %d）", r.FallbackGoodsID, r.GoodsID))
		}
		if len(problems) > 0 {
			fmt.Printf("  ✗ #%d %s\n", r.GoodsID, strings.Join(problems, "; "))
			bad++
			continue
		}
		fmt.Printf("  ✓ #%d fulfillment=self fallback=%d root_category_id=%d self_stock=%d\n",
			r.GoodsID, r.FallbackGoodsID, r.RootCategoryID, r.SelfStock)
	}
	if bad > 0 {
		fmt.Printf("\n自检失败：%d 个商品不满足要求。\n", bad)
		return 1
	}
	fmt.Println("\n自检通过：全部目标商品均为自营，且 fallback 指回自身。")
	return 0
}
