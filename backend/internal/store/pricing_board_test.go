package store

import (
	"testing"
	"time"

	"hstore/backend/internal/db"
)

// TestResolvePriceOnBoard 验证「指定板块」作用域按 root_category_id 精确匹配。
//
// 旧实现只有 platform（从分类名推断，只有 microsoft/google/other），
// 53 个板块里绝大多数都落到 other，导致「指定平台」实际不可用。
func TestResolvePriceOnBoard(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()

	// 清掉历史规则，保证本测试的匹配结果可预期
	_, _ = d.Exec("DELETE FROM pricing_rules")
	ts := time.Now().UnixMilli()

	// 板块 19（Tiktok）加价 5；板块 17（微软）加价 1；全站加价 0.5
	mk := func(name, scope, scopeID string, amount float64, priority int) {
		if _, e := d.Exec(`INSERT INTO pricing_rules
			(name, scope, scope_id, formula_type, amount, percent, multiplier, fixed, rounding, priority, enabled, created_at, updated_at)
			VALUES(?,?,?,'fixed_add',?,0,1,0,'round4',?,1,?,?)`,
			name, scope, scopeID, amount, priority, ts, ts); e != nil {
			t.Fatal(e)
		}
	}
	mk("全站", "global", "", 0.5, 0)
	mk("Tiktok 板块", "board", "19", 5, 0)
	mk("微软板块", "board", "17", 1, 0)

	// 板块 19 的商品：成本 10 → 10 + 5 = 15（板块规则优先于全站）
	got, ruleID := ResolvePriceOnBoard(10, 1001, []int{19, 1901}, "other", 19)
	if got != 15 {
		t.Fatalf("板块 19 应按板块规则加价 5，实际 %v", got)
	}
	if ruleID == nil {
		t.Fatal("应命中规则")
	}

	// 板块 17 的商品：10 + 1 = 11
	got, _ = ResolvePriceOnBoard(10, 1002, []int{17, 1701}, "microsoft", 17)
	if got != 11 {
		t.Fatalf("板块 17 应按板块规则加价 1，实际 %v", got)
	}

	// 没有板块规则的板块：落到全站 10 + 0.5 = 10.5
	got, _ = ResolvePriceOnBoard(10, 1003, []int{453}, "other", 453)
	if got != 10.5 {
		t.Fatalf("无板块规则时应落到全站规则，实际 %v", got)
	}

	// boardID=0（未知板块）时不应误命中任何板块规则
	got, _ = ResolvePriceOnBoard(10, 1004, nil, "", 0)
	if got != 10.5 {
		t.Fatalf("未知板块应只命中全站规则，实际 %v", got)
	}
}

// TestBoardRuleBeatsPlatform 板块规则应比旧的 platform 规则更具体。
func TestBoardRuleBeatsPlatform(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	_, _ = d.Exec("DELETE FROM pricing_rules")
	ts := time.Now().UnixMilli()

	insReal := func(name, scope, scopeID string, amount float64) {
		_, _ = d.Exec(`INSERT INTO pricing_rules
			(name, scope, scope_id, formula_type, amount, percent, multiplier, fixed, rounding, priority, enabled, created_at, updated_at)
			VALUES(?,?,?,'fixed_add',?,0,1,0,'round4',0,1,?,?)`,
			name, scope, scopeID, amount, ts, ts)
	}
	insReal("平台", "platform", "microsoft", 100)
	insReal("板块", "board", "17", 7)

	// 两者都命中时，板块（更具体）应胜出
	got, _ := ResolvePriceOnBoard(10, 2001, []int{17}, "microsoft", 17)
	if got != 17 {
		t.Fatalf("板块规则应优先于平台规则（10+7=17），实际 %v", got)
	}
}

// TestListBoardOptions 只统计已同步商品所属的板块。
func TestListBoardOptions(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	ts := time.Now().UnixMilli()

	// 造两个板块，各挂 2 / 1 个商品
	_, _ = d.Exec(`INSERT OR IGNORE INTO categories(id,name,parent_id,platform,updated_at) VALUES(19,'Tiktok',0,'other',?)`, ts)
	_, _ = d.Exec(`INSERT OR IGNORE INTO categories(id,name,parent_id,platform,updated_at) VALUES(17,'微软',0,'microsoft',?)`, ts)
	for i, gid := range []int{3001, 3002} {
		_, _ = d.Exec(`INSERT OR IGNORE INTO products(goods_id, name, root_category_id, dir_ids, cost_price, final_price, platform, enabled, status, updated_at)
			VALUES(?,?,19,'[19]',10,10,'other',1,1,?)`, gid, "tiktok-"+itoaTest(i), ts)
	}
	_, _ = d.Exec(`INSERT OR IGNORE INTO products(goods_id, name, root_category_id, dir_ids, cost_price, final_price, platform, enabled, status, updated_at)
		VALUES(3003,'ms-1',17,'[17]',10,10,'microsoft',1,1,?)`, ts)

	opts := ListBoardOptions()
	found := map[int]int{}
	for _, o := range opts {
		found[o.ID] = o.Total
	}
	if found[19] < 2 {
		t.Fatalf("板块 19 应统计到至少 2 个商品，实际 %d", found[19])
	}
	if found[17] < 1 {
		t.Fatalf("板块 17 应统计到至少 1 个商品，实际 %d", found[17])
	}
	// 没有商品的板块不应出现
	for _, o := range opts {
		if o.Total == 0 {
			t.Fatalf("不应列出没有商品的板块: %+v", o)
		}
	}
}
