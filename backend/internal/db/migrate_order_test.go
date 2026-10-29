package db

import (
	"database/sql"
	"testing"
)

// TestMigrateLegacyOrderNumbers 历史订单（order_number 就是上游号）必须被
// 换成本地单号，旧号归档到 upstream_order_no，且引用表同步更新。
func TestMigrateLegacyOrderNumbers(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	_, err = conn.Exec(`
CREATE TABLE orders (
  order_number TEXT PRIMARY KEY,
  upstream_order_no TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'user'
);
CREATE TABLE cards (card_id TEXT PRIMARY KEY, order_number TEXT NOT NULL);
CREATE TABLE ledger (id INTEGER PRIMARY KEY, order_number TEXT NOT NULL DEFAULT '');
CREATE TABLE redeem_codes (id INTEGER PRIMARY KEY, order_number TEXT NOT NULL DEFAULT '');
CREATE TABLE guest_purchases (id INTEGER PRIMARY KEY, upstream_order_no TEXT NOT NULL DEFAULT '');

INSERT INTO orders(order_number, source) VALUES
  ('2026102922401856296','user'),
  ('2026102700001355235','guest'),
  ('2026102623172036706','guest');
INSERT INTO cards VALUES ('c1','2026102922401856296');
INSERT INTO ledger(id, order_number) VALUES (1,'2026102922401856296'),(2,'');
INSERT INTO guest_purchases(id, upstream_order_no) VALUES (1,'2026102700001355235');
`)
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
	var orderNos, upstreams []string
	for rows.Next() {
		var o, u string
		if err := rows.Scan(&o, &u); err != nil {
			t.Fatal(err)
		}
		orderNos = append(orderNos, o)
		upstreams = append(upstreams, u)
	}
	rows.Close()

	if len(orderNos) != 3 {
		t.Fatalf("订单数异常: %d", len(orderNos))
	}
	for _, o := range orderNos {
		if o == "2026102922401856296" || o == "2026102700001355235" || o == "2026102623172036706" {
			t.Fatalf("上游号仍被当作本地单号: %s", o)
		}
		if len(o) != 12 || o[:2] != "HS" {
			t.Fatalf("本地单号格式不对: %s", o)
		}
	}
	for _, u := range upstreams {
		if u == "" {
			t.Fatal("上游号没有归档")
		}
	}

	// 2) cards / ledger / guest_purchases 的引用必须跟着改
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
	for name, ref := range map[string]string{"cards": cardRef, "ledger": ledgerRef, "guest_purchases": guestRef} {
		var n int
		if err := conn.QueryRow("SELECT COUNT(*) FROM orders WHERE order_number=?", ref).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s 的引用没有指向有效的本地订单: %q", name, ref)
		}
	}

	// 3) 幂等：再跑一次不应再改任何东西
	before := append([]string(nil), orderNos...)
	if err := migrateLegacyOrderNumbers(conn); err != nil {
		t.Fatal(err)
	}
	rows2, _ := conn.Query(`SELECT order_number FROM orders ORDER BY order_number`)
	var after []string
	for rows2.Next() {
		var o string
		rows2.Scan(&o)
		after = append(after, o)
	}
	rows2.Close()
	if len(after) != len(before) {
		t.Fatalf("重复迁移改变了订单数: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("重复迁移改动了单号: %s -> %s", before[i], after[i])
		}
	}
}

// TestNewLegacyOrderNoStable 同一个旧单号必须始终映射到同一个新单号。
func TestNewLegacyOrderNoStable(t *testing.T) {
	a := newLegacyOrderNo("2026102922401856296")
	b := newLegacyOrderNo("2026102922401856296")
	if a != b {
		t.Fatalf("映射不稳定: %s vs %s", a, b)
	}
	if newLegacyOrderNo("x") == newLegacyOrderNo("y") {
		t.Fatal("不同旧单号不应映射到同一新单号")
	}
	if len(a) != 12 || a[:2] != "HS" {
		t.Fatalf("格式不对: %s", a)
	}
}
