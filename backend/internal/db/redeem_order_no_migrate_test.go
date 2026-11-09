package db

import (
	"testing"
)

// 兑换码格式判定必须只认标准 19 位（XXXX-XXXX-XXXX-XXXX）。
func TestRedeemOrderNoFormat(t *testing.T) {
	ok := []string{
		"ABCD-EFGH-IJKL-MNOP",
		"abcd-efgh-ijkl-mnop",
		"2345-6789-ABCD-EFGH",
		// 旧版 12 位兑换码（线上仍有大量在流通）
		"ABCD-EFGH-IJKL",
		"y3lr-zq9w-3ca8",
	}
	for _, s := range ok {
		if !redeemOrderNoFormat(s) {
			t.Fatalf("%q 应被认定为合法兑换码", s)
		}
	}
	bad := []string{
		"", "ABCDEFGHIJKLMNOP",
		"ABCD-EFGH-IJKL-MNOP-X", "ABC-EFGH-IJKL-MNOP",
		"ABCD_EFGH_IJKL_MNOP", "HS2026110722591236E6C20F45",
	}
	for _, s := range bad {
		if redeemOrderNoFormat(s) {
			t.Fatalf("%q 不应被认定为合法兑换码", s)
		}
	}
}

// 归一化把无分隔符 / 小写 / 带空格的码值统一成标准格式。
func TestNormalizeRedeemCode(t *testing.T) {
	cases := map[string]string{
		"abcdefghijklmnop":       "ABCD-EFGH-IJKL-MNOP",
		"ABCD-EFGH-IJKL-MNOP":    "ABCD-EFGH-IJKL-MNOP",
		"  abcd-efgh-ijkl-mnop ": "ABCD-EFGH-IJKL-MNOP",
		// 旧一代 12 位码规整后仍是 12 位，不能硬凑成 16 位
		"abcdefghijkl":   "ABCD-EFGH-IJKL",
		"y3lr-zq9w-3ca8": "Y3LR-ZQ9W-3CA8",
	}
	for in, want := range cases {
		if got := normalizeRedeemCode(in); got != want {
			t.Fatalf("normalizeRedeemCode(%q) = %q，期望 %q", in, got, want)
		}
	}
	// 长度不对时原样返回（交给调用方判断格式）。
	if got := normalizeRedeemCode("ABC"); got != "ABC" {
		t.Fatalf("长度异常应原样返回，实际 %q", got)
	}
}

// 历史兑换单的单号要迁移成兑换码，并同步 cards / redeem_codes 的引用。
// 不是兑换码单的订单不能被误改。
func TestMigrateRedeemOrderNumbers(t *testing.T) {
	if _, e := Open(); e != nil {
		t.Fatalf("打开数据库失败: %v", e)
	}
	d := Must()

	const (
		localNo = "HS261107010101ABCD"
		code    = "MIGR-TEST-CODE-0001"
		otherNo = "HS261107010102EFGH"
	)
	// 造一张兑换码单（orders.order_number 还是内部单号）
	_, _ = d.Exec("DELETE FROM cards WHERE order_number IN (?,?)", localNo, otherNo)
	_, _ = d.Exec("DELETE FROM redeem_codes WHERE code = ?", code)
	_, _ = d.Exec("DELETE FROM orders WHERE order_number IN (?,?)", localNo, otherNo)

	if _, e := d.Exec(`INSERT INTO orders(order_number, goods_id, goods_name, count, money, status, result, platform, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, localNo, 990001, "迁移测试商品", 1, 1, 3, "兑换码取件", "self", 1, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := d.Exec(`INSERT INTO orders(order_number, goods_id, goods_name, count, money, status, result, platform, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, otherNo, 990002, "普通订单", 1, 1, 3, "下单成功", "self", 1, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := d.Exec(`INSERT INTO redeem_codes(code, goods_id, goods_count, status, order_number, created_at)
		VALUES(?,?,?,?,?,?)`, code, 990001, 1, "used", localNo, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := d.Exec(`INSERT INTO cards(card_id, order_number, card_no, status, created_at)
		VALUES(?,?,?,?,?)`, "MIGR-CARD-1", localNo, "card-no", 0, 1); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, _ = d.Exec("DELETE FROM cards WHERE card_id = ?", "MIGR-CARD-1")
		_, _ = d.Exec("DELETE FROM redeem_codes WHERE code = ?", code)
		_, _ = d.Exec("DELETE FROM orders WHERE order_number IN (?,?)", localNo, otherNo, code)
	})

	if e := migrateRedeemOrderNumbers(d); e != nil {
		t.Fatalf("迁移失败: %v", e)
	}

	// 兑换单：单号变成兑换码
	var n int
	if e := d.QueryRow("SELECT COUNT(*) FROM orders WHERE order_number = ?", code).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatalf("兑换单单号应迁移为 %s", code)
	}
	if e := d.QueryRow("SELECT COUNT(*) FROM orders WHERE order_number = ?", localNo).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 0 {
		t.Fatalf("旧单号 %s 应已不存在", localNo)
	}
	// cards 引用同步
	if e := d.QueryRow("SELECT COUNT(*) FROM cards WHERE order_number = ?", code).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatalf("cards 引用应同步为 %s", code)
	}
	// redeem_codes 引用同步
	if e := d.QueryRow("SELECT COUNT(*) FROM redeem_codes WHERE code = ? AND order_number = ?", code, code).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatalf("redeem_codes.order_number 应同步为 %s", code)
	}
	// 非兑换单不受影响
	if e := d.QueryRow("SELECT COUNT(*) FROM orders WHERE order_number = ?", otherNo).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatalf("非兑换单 %s 不应被改动", otherNo)
	}

	// 幂等：再跑一次不报错、不变化
	if e := migrateRedeemOrderNumbers(d); e != nil {
		t.Fatalf("重复迁移应无副作用: %v", e)
	}
	if e := d.QueryRow("SELECT COUNT(*) FROM orders WHERE order_number = ?", code).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatal("重复迁移后数据应保持不变")
	}
}
