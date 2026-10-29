package store

import (
	"strings"
	"testing"
	"time"

	"hstore/backend/internal/db"
)

// TestNewOrderNoFormat 锁定统一单号格式：前缀 + 14 位时间 + 10 位十六进制。
func TestNewOrderNoFormat(t *testing.T) {
	no := NewOrderNo(PrefixCrypto)
	if !strings.HasPrefix(no, PrefixCrypto) {
		t.Fatalf("单号应带前缀 %q，实际 %q", PrefixCrypto, no)
	}
	body := strings.TrimPrefix(no, PrefixCrypto)
	if len(body) != 24 {
		t.Fatalf("单号长度应为 前缀+24，实际 %q (%d)", no, len(body))
	}
	// 时间部分必须可解析，保证按单号能看出下单时间
	if _, err := time.Parse("20060102150405", body[:14]); err != nil {
		t.Fatalf("单号时间部分不可解析: %q (%v)", body[:14], err)
	}
	for _, c := range body[14:] {
		if !strings.ContainsRune("0123456789ABCDEF", c) {
			t.Fatalf("随机部分应是大写十六进制: %q", body[14:])
		}
	}
}

// TestNewOrderNoUnique 生成 2000 个单号不应冲突。
func TestNewOrderNoUnique(t *testing.T) {
	seen := make(map[string]bool, 2000)
	for i := 0; i < 2000; i++ {
		no := NewSystemOrderNo()
		if seen[no] {
			t.Fatalf("单号重复: %s", no)
		}
		seen[no] = true
	}
}

// TestCryptoOrderGetsSystemOrderNo 建单时必须写入系统单号，且能被按号查回。
func TestCryptoOrderGetsSystemOrderNo(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	nowMs := time.Now().UnixMilli()
	_, _ = d.Exec("INSERT OR IGNORE INTO bybit_pool(id, sub_uid, chain, status, updated_at) VALUES(950,'uid-950','TRX','available',?)", nowMs)

	id := InsertCryptoOrder(950, 950, "uid-950", "TNoTestAddr", "TRX",
		7, "1.00", "1.0001", 7, 7, nowMs+1800000)
	if id == 0 {
		t.Fatal("建单失败")
	}

	order := GetCryptoOrder(id)
	if order == nil {
		t.Fatal("订单读不回来")
	}
	if !strings.HasPrefix(order.OrderNo, PrefixCrypto) {
		t.Fatalf("USDT 支付单号应带 %q 前缀，实际 %q", PrefixCrypto, order.OrderNo)
	}
	if order.OrderNo == "" || strings.Contains(order.OrderNo, "USDT-") {
		t.Fatalf("不应再用 USDT-<id> 这种简陋单号，实际 %q", order.OrderNo)
	}

	// 用户端按单号查单
	if got := GetCryptoOrderByNo(order.OrderNo); got == nil || got.ID != id {
		t.Fatalf("按支付单号查不到订单: %q", order.OrderNo)
	}
	if GetCryptoOrderByNo("NOT-EXIST") != nil {
		t.Fatal("不存在的单号应返回 nil")
	}
}
