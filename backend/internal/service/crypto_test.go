package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hstore-service-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("FAKA_DB_PATH", filepath.Join(dir, "service-test.db"))
	if _, err := db.Open(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = db.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// TestSettleExpiredCryptoOrder 覆盖「订单过期后才补查到账」的场景。
//
// 轮询一旦中断（例如 Bybit 查单持续失败），用户可能已在有效期内付款，
// 但订单先被判成 expired。恢复后必须能补结算，否则用户的钱会卡死。
func TestSettleExpiredCryptoOrder(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()

	nowMs := time.Now().UnixMilli()
	// 准备一个可用地址池与用户
	_, _ = d.Exec("INSERT OR IGNORE INTO bybit_pool(id, sub_uid, chain, status, updated_at) VALUES(900,'svc-uid-900','TRX','available',?)", nowMs)
	_, _ = d.Exec("INSERT OR IGNORE INTO users(uid, username, balance, created_at, updated_at) VALUES(900,'svc_test',10,?,?)", nowMs, nowMs)

	created := nowMs - 40*60*1000 // 40 分钟前创建，已过期
	expires := created + 30*60*1000

	res, err := d.Exec(`INSERT INTO crypto_orders
		(uid,pool_id,sub_uid,address,chain,requested_cny,requested_usdt,usdt_amount,rate,credit_cny,status,expires_at,created_at)
		VALUES(900,900,'svc-uid-900','TExpiredAddr','TRX',7,'1.00','1.0001',7,7,'expired',?,?)`, expires, created)
	if err != nil {
		t.Fatal(err)
	}
	id64, _ := res.LastInsertId()
	id := int(id64)

	order := store.GetCryptoOrder(id)
	if order == nil || order.Status != "expired" {
		t.Fatalf("订单准备失败: %+v", order)
	}

	settled := store.SettleCryptoOrder(order, "TX-EXPIRED-1", "1.0001", "补结算测试")
	if settled == nil || settled.Status != "paid" {
		t.Fatalf("过期订单应能补结算，实际: %+v", settled)
	}
	if settled.ReceivedUsdt == nil || *settled.ReceivedUsdt != "1.0001" {
		t.Fatalf("到账金额未写入: %+v", settled.ReceivedUsdt)
	}

	// 余额应增加 1.0001 * 7 = 7.0007
	var balance float64
	if err := d.QueryRow("SELECT balance FROM users WHERE uid=900").Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance <= 10 {
		t.Fatalf("过期补结算未入账余额: %v", balance)
	}

	// 地址池应被释放
	var status string
	if err := d.QueryRow("SELECT status FROM bybit_pool WHERE id=900").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "available" {
		t.Fatalf("补结算后地址池应释放，实际: %s", status)
	}
}

// TestSettleCryptoOrderIsIdempotent 重复结算不应重复加钱。
func TestSettleCryptoOrderIsIdempotent(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()

	nowMs := time.Now().UnixMilli()
	_, _ = d.Exec("INSERT OR IGNORE INTO bybit_pool(id, sub_uid, chain, status, updated_at) VALUES(901,'svc-uid-901','TRX','available',?)", nowMs)
	_, _ = d.Exec("INSERT OR IGNORE INTO users(uid, username, balance, created_at, updated_at) VALUES(901,'svc_test2',0,?,?)", nowMs, nowMs)

	res, err := d.Exec(`INSERT INTO crypto_orders
		(uid,pool_id,sub_uid,address,chain,requested_cny,requested_usdt,usdt_amount,rate,credit_cny,status,expires_at,created_at)
		VALUES(901,901,'svc-uid-901','TIdemAddr','TRX',7,'1.00','2.0002',7,14,'waiting',?,?)`, nowMs+1800000, nowMs)
	if err != nil {
		t.Fatal(err)
	}
	id64, _ := res.LastInsertId()
	id := int(id64)

	order := store.GetCryptoOrder(id)
	store.SettleCryptoOrder(order, "TX-IDEM-1", "2.0002", "首次")
	store.SettleCryptoOrder(store.GetCryptoOrder(id), "TX-IDEM-1", "2.0002", "重复")

	var balance float64
	if err := d.QueryRow("SELECT balance FROM users WHERE uid=901").Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if balance != 14.0014 {
		t.Fatalf("重复结算导致余额异常: %v (期望 14.0014)", balance)
	}
}
