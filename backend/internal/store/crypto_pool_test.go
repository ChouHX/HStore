package store

import (
	"sync"
	"testing"
	"time"

	"hstore/backend/internal/db"
)

// TestPickPoolSlotDoesNotFreeze 选地址不应改变子账户状态。
//
// 之前每个订单会把子账户置为 reserved，池子只有两个地址时，
// 用户下单后直接关网页就会占住一半通道直到过期，拖慢其他人。
func TestPickPoolSlotDoesNotFreeze(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	nowMs := time.Now().UnixMilli()

	_, _ = d.Exec(`INSERT OR IGNORE INTO bybit_pool(id, sub_uid, address_trx, address_evm, chain, status, updated_at)
		VALUES(960,'uid-960','TTrx960','0xevm960','TRX','available',?)`, nowMs)

	// 连续取 5 次都不应把地址锁住
	for i := 0; i < 5; i++ {
		slot, ok := PickPoolSlot("TRX")
		if !ok {
			t.Fatalf("第 %d 次取地址失败", i)
		}
		if slot.Address == "" {
			t.Fatal("地址为空")
		}
	}
	var status string
	if err := d.QueryRow("SELECT status FROM bybit_pool WHERE id=960").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "available" {
		t.Fatalf("选地址不应改变状态，实际 %q", status)
	}

	// EVM 链应拿到 address_evm
	evmSlot, ok := PickPoolSlot("ARBI")
	if !ok {
		t.Fatal("EVM 链取地址失败")
	}
	if evmSlot.Address != "0xevm960" {
		t.Fatalf("EVM 链应使用 address_evm，实际 %q", evmSlot.Address)
	}

	// 新链都应能选到地址（共用同一个 EVM 地址）
	for _, code := range []string{"BSC", "ARBI", "MATIC"} {
		s, ok := PickPoolSlot(code)
		if !ok || s.Address != "0xevm960" {
			t.Fatalf("%s 应复用同一个 EVM 地址，实际 %+v ok=%v", code, s, ok)
		}
	}
}

// TestUniqueAmountAcrossConcurrentOrders 同一地址多笔待支付单必须金额唯一，
// 否则去掉地址冻结后无法区分到账属于哪一单。
func TestUniqueAmountAcrossConcurrentOrders(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	nowMs := time.Now().UnixMilli()
	_, _ = d.Exec(`INSERT OR IGNORE INTO bybit_pool(id, sub_uid, address_trx, chain, status, updated_at)
		VALUES(961,'uid-961','TTrx961','TRX','available',?)`, nowMs)
	_, _ = d.Exec(`INSERT OR IGNORE INTO users(uid, username, balance, created_at, updated_at)
		VALUES(961,'amt_test',0,?,?)`, nowMs, nowMs)

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		amt, ok := UniqueExpectedAmount(7)
		if !ok {
			t.Fatalf("第 %d 次生成唯一金额失败", i)
		}
		if seen[amt] {
			t.Fatalf("金额重复: %s（同一地址会串单）", amt)
		}
		seen[amt] = true
		// 真的落库，才能验证下一次会避开它
		if id := InsertCryptoOrder(961, 961, "uid-961", "TTrx961", "TRX",
			7, "1.00", amt, 7, 7, nowMs+1800000); id == 0 {
			t.Fatal("建单失败")
		}
	}
}

// TestUniqueAmountAvoidsExpiredUnsettled 过期但未结算的单仍占用金额，
// 因为它可能补结算（用户在有效期内付了款）。
func TestUniqueAmountAvoidsExpiredUnsettled(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	nowMs := time.Now().UnixMilli()

	const amt = "7.0042"
	// 直接插入一个 expired 且无 txid 的单
	_, err := d.Exec(`INSERT INTO crypto_orders(uid,order_no,pool_id,sub_uid,address,chain,
		requested_cny,requested_usdt,usdt_amount,rate,credit_cny,status,expires_at,created_at)
		VALUES(961,'US-EXPIRE-AMT',961,'uid-961','TTrx961','TRX',7,'1.00',?,7,7,'expired',?,?)`,
		amt, nowMs-60000, nowMs-3600000)
	if err != nil {
		t.Fatal(err)
	}

	// 反复生成都不应再给出这个金额
	for i := 0; i < 40; i++ {
		got, ok := UniqueExpectedAmount(7)
		if !ok {
			continue
		}
		if got == amt {
			t.Fatal("过期未结算的单占用的金额被复用，补结算时会串单")
		}
	}
}

// TestExpireDoesNotBlockPool 过期不应影响地址可用性。
func TestExpireDoesNotBlockPool(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	nowMs := time.Now().UnixMilli()
	_, _ = d.Exec(`INSERT OR IGNORE INTO bybit_pool(id, sub_uid, address_trx, chain, status, updated_at)
		VALUES(962,'uid-962','TTrx962','TRX','available',?)`, nowMs)

	// 建一个已经过期的单
	id := InsertCryptoOrder(961, 962, "uid-962", "TTrx962", "TRX",
		7, "1.00", "7.0099", 7, 7, nowMs-1000)
	if id == 0 {
		t.Fatal("建单失败")
	}
	if n := ExpireCryptoOrders(); n < 1 {
		t.Fatalf("应至少过期 1 单，实际 %d", n)
	}
	o := GetCryptoOrder(id)
	if o == nil || o.Status != "expired" {
		t.Fatalf("订单未过期: %+v", o)
	}
	// 地址池不受影响
	slot, ok := PickPoolSlot("TRX")
	if !ok {
		t.Fatal("过期后地址仍应可用")
	}
	if slot.Address == "" {
		t.Fatal("地址为空")
	}
}

// TestUniqueAmountConcurrent 并发下生成的金额必须互不相同。
// 去掉地址冻结后，金额是唯一区分依据，这里模拟 30 个并发建单。
func TestUniqueAmountConcurrent(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	nowMs := time.Now().UnixMilli()
	_, _ = d.Exec(`INSERT OR IGNORE INTO bybit_pool(id, sub_uid, address_trx, chain, status, updated_at)
		VALUES(963,'uid-963','TTrx963','TRX','available',?)`, nowMs)
	_, _ = d.Exec(`INSERT OR IGNORE INTO users(uid, username, balance, created_at, updated_at)
		VALUES(963,'conc_test',0,?,?)`, nowMs, nowMs)

	const n = 30
	type res struct {
		amt string
		id  int
	}
	ch := make(chan res, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			amt, ok := UniqueExpectedAmount(7)
			if !ok {
				ch <- res{}
				return
			}
			id := InsertCryptoOrder(963, 963, "uid-963", "TTrx963", "TRX",
				7, "1.00", amt, 7, 7, nowMs+1800000)
			ch <- res{amt: amt, id: id}
		}()
	}
	wg.Wait()
	close(ch)

	seen := map[string]bool{}
	okCount := 0
	for r := range ch {
		if r.id == 0 || r.amt == "" {
			continue
		}
		okCount++
		if seen[r.amt] {
			t.Fatalf("并发下出现重复金额: %s（会导致串单）", r.amt)
		}
		seen[r.amt] = true
	}
	if okCount < n/2 {
		t.Fatalf("并发建单成功率过低: %d/%d", okCount, n)
	}
	t.Logf("并发 %d 个请求，成功 %d 笔，金额全部唯一", n, okCount)
}
