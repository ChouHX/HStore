package db

import (
	"database/sql"
	"testing"
	"time"
)

// migrateTestSchema 建一份迁移用到的最小表结构（含 created_at 与 sync_meta）。
func migrateTestSchema(t *testing.T, conn *sql.DB) {
	t.Helper()
	if _, err := conn.Exec(`
CREATE TABLE sync_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');
CREATE TABLE orders (
  order_number TEXT PRIMARY KEY,
  upstream_order_no TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'user',
  created_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE cards (card_id TEXT PRIMARY KEY, order_number TEXT NOT NULL);
CREATE TABLE ledger (id INTEGER PRIMARY KEY, order_number TEXT NOT NULL DEFAULT '');
CREATE TABLE redeem_codes (id INTEGER PRIMARY KEY, order_number TEXT NOT NULL DEFAULT '');
CREATE TABLE guest_purchases (id INTEGER PRIMARY KEY, upstream_order_no TEXT NOT NULL DEFAULT '');
-- 生产库的账本触发器：只增不改，迁移不能碰 ledger
CREATE TRIGGER ledger_no_update
  BEFORE UPDATE ON ledger BEGIN SELECT RAISE(ABORT, 'ledger is immutable'); END;
CREATE TRIGGER ledger_no_delete
  BEFORE DELETE ON ledger BEGIN SELECT RAISE(ABORT, 'ledger is immutable'); END;
`); err != nil {
		t.Fatal(err)
	}
}

// orderNos 读出全部单号（按单号排序）。
func orderNos(t *testing.T, conn *sql.DB) []string {
	t.Helper()
	rows, err := conn.Query(`SELECT order_number FROM orders ORDER BY order_number`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			t.Fatal(err)
		}
		out = append(out, o)
	}
	return out
}

// TestMigrateLegacyOrderNumbers 历史订单（order_number 就是上游号）必须被
// 换成本地单号，旧号归档到 upstream_order_no，且引用表同步更新。
func TestMigrateLegacyOrderNumbers(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	migrateTestSchema(t, conn)

	// 2026-10-29 22:40:18.562（毫秒时间戳），用来校验单号里的时间前缀。
	const createdAt = int64(1793361618562)
	_, err = conn.Exec(`
INSERT INTO orders(order_number, source, created_at) VALUES
  ('2026102922401856296','user', ?),
  ('2026102700001355235','guest', ?),
  ('2026102623172036706','guest', ?);
INSERT INTO cards VALUES ('c1','2026102922401856296');
INSERT INTO ledger(id, order_number) VALUES (1,'2026102922401856296'),(2,'');
INSERT INTO guest_purchases(id, upstream_order_no) VALUES (1,'2026102700001355235');
`, createdAt, createdAt, createdAt)
	if err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyOrderNumbers(conn); err != nil {
		t.Fatal(err)
	}

	// 1) 所有订单都换成了本地单号，旧号归档到 upstream_order_no
	rows, err := conn.Query(`SELECT order_number, upstream_order_no FROM orders ORDER BY order_number`)
	if err != nil {
		t.Fatal(err)
	}
	var nos, upstreams []string
	for rows.Next() {
		var o, u string
		if err := rows.Scan(&o, &u); err != nil {
			t.Fatal(err)
		}
		nos = append(nos, o)
		upstreams = append(upstreams, u)
	}
	rows.Close()

	if len(nos) != 3 {
		t.Fatalf("订单数异常: %d", len(nos))
	}
	for _, o := range nos {
		if o == "2026102922401856296" || o == "2026102700001355235" || o == "2026102623172036706" {
			t.Fatalf("上游号仍被当作本地单号: %s", o)
		}
		if len(o) != 18 || o[:2] != "HS" {
			t.Fatalf("本地单号格式不对: %s", o)
		}
		if o[2:14] != time.UnixMilli(createdAt).In(legacyOrderTimeZone).Format(legacyOrderNoTimeLayout) {
			t.Fatalf("单号时间前缀不对: %s", o)
		}
	}
	for _, u := range upstreams {
		if u == "" {
			t.Fatal("上游号没有归档")
		}
	}

	// 2) cards / guest_purchases 的引用必须跟着改；ledger 是只增不改的账本，
	// 保留旧单号快照（有触发器在，改也改不动）。
	var cardRef, ledgerRef, guestRef string
	if err := conn.QueryRow("SELECT order_number FROM cards WHERE card_id='c1'").Scan(&cardRef); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT order_number FROM ledger WHERE id=1").Scan(&ledgerRef); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow("SELECT upstream_order_no FROM guest_purchases WHERE id=1").Scan(&guestRef); err != nil {
		t.Fatal(err)
	}
	for name, ref := range map[string]string{"cards": cardRef, "guest_purchases": guestRef} {
		var n int
		if err := conn.QueryRow("SELECT COUNT(*) FROM orders WHERE order_number=?", ref).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s 的引用没有指向有效的本地订单: %q", name, ref)
		}
	}
	if ledgerRef != "2026102922401856296" {
		t.Fatalf("ledger 快照不应被改动: %q", ledgerRef)
	}

	// 3) 幂等：再跑一次不应再改任何东西
	before := append([]string(nil), nos...)
	if err := migrateLegacyOrderNumbers(conn); err != nil {
		t.Fatal(err)
	}
	after := orderNos(t, conn)
	if len(after) != len(before) {
		t.Fatalf("重复迁移改变了订单数: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("重复迁移改动了单号: %s -> %s", before[i], after[i])
		}
	}
}

// TestMigrateRemapsOldFormatOrderNo 上一版迁移生成的 12 位 HS+hex 单号
// （可由旧单号推导）必须重映射成 18 位带密钥混淆的新格式，且保持幂等。
func TestMigrateRemapsOldFormatOrderNo(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	migrateTestSchema(t, conn)

	const (
		upstream  = "2026102922401856296"
		oldNo     = "HS1A2B3C4D5E" // 旧格式：HS + 10 位大写 hex
		createdAt = int64(1793361618562)
	)
	if _, err := conn.Exec(`
INSERT INTO orders(order_number, upstream_order_no, source, created_at) VALUES (?, ?, 'user', ?);
INSERT INTO cards VALUES ('c1', ?);
INSERT INTO ledger(id, order_number) VALUES (1, ?);
`, oldNo, upstream, createdAt, oldNo, oldNo); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyOrderNumbers(conn); err != nil {
		t.Fatal(err)
	}

	var got, gotUpstream string
	if err := conn.QueryRow("SELECT order_number, upstream_order_no FROM orders").Scan(&got, &gotUpstream); err != nil {
		t.Fatal(err)
	}
	if got == oldNo {
		t.Fatalf("旧格式单号没有被重映射: %s", got)
	}
	if len(got) != 18 || got[:2] != "HS" {
		t.Fatalf("重映射后的单号格式不对: %s", got)
	}
	if got != newLegacyOrderNo(upstream, createdAt) {
		t.Fatalf("重映射结果与 newLegacyOrderNo 不一致: %s", got)
	}
	if gotUpstream != upstream {
		t.Fatalf("重映射不应改动 upstream_order_no: %s", gotUpstream)
	}

	// 引用表跟着改
	var cardRef string
	if err := conn.QueryRow("SELECT order_number FROM cards WHERE card_id='c1'").Scan(&cardRef); err != nil {
		t.Fatal(err)
	}
	if cardRef != got {
		t.Fatalf("cards 引用没跟着重映射: %s != %s", cardRef, got)
	}

	// 幂等：再跑一次不变
	if err := migrateLegacyOrderNumbers(conn); err != nil {
		t.Fatal(err)
	}
	var again string
	if err := conn.QueryRow("SELECT order_number FROM orders").Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("重复迁移改动了重映射结果: %s -> %s", got, again)
	}
}

// TestNewLegacyOrderNoStable 同一个旧单号必须始终映射到同一个新单号。
func TestNewLegacyOrderNoStable(t *testing.T) {
	const createdAt = int64(1793361618562)
	a := newLegacyOrderNo("2026102922401856296", createdAt)
	b := newLegacyOrderNo("2026102922401856296", createdAt)
	if a != b {
		t.Fatalf("映射不稳定: %s vs %s", a, b)
	}
	if newLegacyOrderNo("x", createdAt) == newLegacyOrderNo("y", createdAt) {
		t.Fatal("不同旧单号不应映射到同一新单号")
	}
	if len(a) != 18 || a[:2] != "HS" {
		t.Fatalf("格式不对: %s", a)
	}
	want := time.UnixMilli(createdAt).In(legacyOrderTimeZone).Format(legacyOrderNoTimeLayout)
	if a[2:14] != want {
		t.Fatalf("时间前缀不对: %s != %s", a[2:14], want)
	}
	// 混淆串只能取 base32 字符集
	for _, c := range a[14:] {
		if !(c >= 'A' && c <= 'Z' || c >= '2' && c <= '7') {
			t.Fatalf("混淆串含非法字符: %q", c)
		}
	}
}

// TestBackfillRedeemOrderSaleMoney 历史兑换码订单必须补记销售额，
// 且只补「兑换码核销过 + 金额为 0 + 能查到售价」的那些行。
func TestBackfillRedeemOrderSaleMoney(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`
CREATE TABLE products (
  goods_id    INTEGER PRIMARY KEY,
  final_price REAL NOT NULL DEFAULT 0,
  sales_price REAL NOT NULL DEFAULT 0
);
CREATE TABLE orders (
  order_number TEXT PRIMARY KEY,
  goods_id     INTEGER NOT NULL,
  count        INTEGER NOT NULL DEFAULT 0,
  money        REAL NOT NULL DEFAULT 0,
  sale_money   REAL NOT NULL DEFAULT 0
);
CREATE TABLE redeem_codes (
  id           INTEGER PRIMARY KEY,
  order_number TEXT NOT NULL DEFAULT ''
);
INSERT INTO products(goods_id, final_price, sales_price) VALUES
  (5768, 0.064, 0.1),  -- 有售价
  (900018, 0, 0.07);   -- final_price 为 0，回退到 sales_price
INSERT INTO orders(order_number, goods_id, count, money, sale_money) VALUES
  ('HSCODEFILL1', 5768, 10, 0, 0),    -- 兑换码核销过：应补 0.64
  ('HSCODEFILL2', 900018, 100, 0, 0), -- 兑换码核销过：应补 7
  ('HSNORMAL001', 5768, 5, 0, 0),     -- 普通零元单：不该动
  ('HSPAID00001', 5768, 3, 0.192, 0.192); -- 已记账：不该动
INSERT INTO redeem_codes(id, order_number) VALUES (1, 'HSCODEFILL1'), (2, 'HSCODEFILL2');
`); err != nil {
		t.Fatal(err)
	}

	if err := backfillRedeemOrderSaleMoney(conn); err != nil {
		t.Fatal(err)
	}

	type row struct{ money, sale float64 }
	read := func(no string) row {
		t.Helper()
		var r row
		if err := conn.QueryRow("SELECT money, sale_money FROM orders WHERE order_number = ?", no).
			Scan(&r.money, &r.sale); err != nil {
			t.Fatal(err)
		}
		return r
	}

	if got := read("HSCODEFILL1"); got.money != 0.64 || got.sale != 0.64 {
		t.Fatalf("兑换码订单销售额回填不对: money=%v sale=%v，期望 0.64", got.money, got.sale)
	}
	if got := read("HSCODEFILL2"); got.money != 7 || got.sale != 7 {
		t.Fatalf("final_price 为 0 时应回退 sales_price: money=%v sale=%v，期望 7", got.money, got.sale)
	}
	if got := read("HSNORMAL001"); got.money != 0 || got.sale != 0 {
		t.Fatalf("非兑换码订单不应被补金额: money=%v sale=%v", got.money, got.sale)
	}
	if got := read("HSPAID00001"); got.money != 0.192 || got.sale != 0.192 {
		t.Fatalf("已记账订单不应被覆盖: money=%v sale=%v", got.money, got.sale)
	}

	// 幂等：再跑一次结果不变
	if err := backfillRedeemOrderSaleMoney(conn); err != nil {
		t.Fatal(err)
	}
	if got := read("HSCODEFILL1"); got.money != 0.64 || got.sale != 0.64 {
		t.Fatalf("重复回填改动了结果: money=%v sale=%v", got.money, got.sale)
	}
}
