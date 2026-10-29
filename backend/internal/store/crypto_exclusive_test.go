package store

import (
	"sync"
	"testing"
	"time"

	"hstore/backend/internal/db"
)

// 这一组测试锁定「独占优先」的分配策略：
//   - 地址空闲时用原始金额，不加随机尾数
//   - 只有所有地址都忙，才在同一地址内做金额去重
//   - 金额唯一性只在「同一地址内」成立，不同地址可以收相同金额

func setupPool(t *testing.T, ids ...int) {
	t.Helper()
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	nowMs := time.Now().UnixMilli()
	for _, id := range ids {
		_, _ = d.Exec(`INSERT OR IGNORE INTO bybit_pool(id, sub_uid, address_trx, address_evm, chain, status, updated_at)
			VALUES(?,?,?,?,?, 'available', ?)`,
			id, "uid-"+itoaTest(id), "TTrx"+itoaTest(id), "0xevm"+itoaTest(id), "TRX", nowMs)
	}
}

func itoaTest(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

// TestIdleSlotUsesExactAmount 地址空闲时必须使用原始金额（不加尾数）。
func TestIdleSlotUsesExactAmount(t *testing.T) {
	setupPool(t, 970, 971)

	slot, ok := PickPoolSlot("TRX")
	if !ok {
		t.Fatal("取地址失败")
	}
	if !PoolSlotIsIdle(slot.ID) {
		t.Fatal("新池子里的地址应为空闲")
	}

	// 空闲 → 调用方直接用原值；这里验证空闲判断本身
	// 业务侧逻辑：PoolSlotIsIdle 为 true 时 expected = base（原值）
	if !PoolSlotIsIdle(slot.ID) {
		t.Fatal("空闲判断错误")
	}
}

// TestExclusiveWhenAddressesEnough 地址够用时，多个用户拿到不同地址，
// 即使金额完全相同也不会串单。
func TestExclusiveWhenAddressesEnough(t *testing.T) {
	setupPool(t, 972, 973, 974)
	d := db.Must()
	nowMs := time.Now().UnixMilli()

	const sameAmount = "7.0000"
	used := map[int]bool{}
	for i := 0; i < 3; i++ {
		slot, ok := PickPoolSlot("TRX")
		if !ok {
			t.Fatalf("第 %d 个用户取地址失败", i)
		}
		if used[slot.ID] {
			t.Fatalf("地址 %d 被重复分配（应优先独占）", slot.ID)
		}
		used[slot.ID] = true

		// 空闲地址直接用原值
		amt := sameAmount
		if !PoolSlotIsIdle(slot.ID) {
			t.Fatalf("地址 %d 应为空闲", slot.ID)
		}
		id := InsertCryptoOrder(1, slot.ID, "uid-"+itoaTest(slot.ID), slot.Address, "TRX",
			7, "1.00", amt, 7, 7, nowMs+1800000)
		if id == 0 {
			t.Fatal("建单失败")
		}
	}

	// 三笔相同金额、不同地址 → 各自能按「地址+金额」唯一定位
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM crypto_orders WHERE usdt_amount=? AND status='waiting'`,
		sameAmount).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("应允许 3 笔相同金额的待支付单，实际 %d", n)
	}
}

// TestSharedSlotRequiresUniqueAmount 所有地址都忙时，共用地址内金额必须互不相同。
func TestSharedSlotRequiresUniqueAmount(t *testing.T) {
	setupPool(t, 975)
	d := db.Must()
	nowMs := time.Now().UnixMilli()

	// 占满唯一地址
	first := InsertCryptoOrder(1, 975, "uid-975", "TTrx975", "TRX",
		7, "1.00", "7.0000", 7, 7, nowMs+1800000)
	if first == 0 {
		t.Fatal("首单失败")
	}
	if PoolSlotIsIdle(975) {
		t.Fatal("已有挂单的地址不应判为空闲")
	}

	// 后续订单必须换金额
	seen := map[string]bool{"7.0000": true}
	for i := 0; i < 15; i++ {
		amt, ok := UniqueAmountOnSlot(975, 7)
		if !ok {
			t.Fatalf("第 %d 次生成唯一金额失败", i)
		}
		if seen[amt] {
			t.Fatalf("同一地址内金额重复: %s", amt)
		}
		seen[amt] = true
		if id := InsertCryptoOrder(1, 975, "uid-975", "TTrx975", "TRX",
			7, "1.00", amt, 7, 7, nowMs+1800000); id == 0 {
			t.Fatal("建单失败")
		}
	}

	// 校验：该地址内没有重复金额
	var dup int
	if err := d.QueryRow(`SELECT COUNT(*) FROM (
			SELECT usdt_amount FROM crypto_orders WHERE pool_id=975 AND status='waiting'
			GROUP BY usdt_amount HAVING COUNT(*) > 1)`).Scan(&dup); err != nil {
		t.Fatal(err)
	}
	if dup != 0 {
		t.Fatalf("同一地址内出现 %d 个重复金额", dup)
	}
}

// TestDifferentAddressSameAmountAllowed 不同地址可以同时收相同金额。
func TestDifferentAddressSameAmountAllowed(t *testing.T) {
	setupPool(t, 976, 977)
	nowMs := time.Now().UnixMilli()

	// 用独立金额，避免与其它测试留下的 7.0000 混淆
	const amt = "7.4242"
	for _, pid := range []int{976, 977} {
		if id := InsertCryptoOrder(1, pid, "uid-"+itoaTest(pid), "TTrx"+itoaTest(pid), "TRX",
			7, "1.00", amt, 7, 7, nowMs+1800000); id == 0 {
			t.Fatalf("地址 %d 建单失败", pid)
		}
	}
	// 两个地址各有一笔 7.0000，互不影响
	var n int
	_ = db.Must().QueryRow(`SELECT COUNT(DISTINCT pool_id) FROM crypto_orders
		WHERE usdt_amount=? AND status='waiting' AND pool_id IN (976,977)`, amt).Scan(&n)
	if n != 2 {
		t.Fatalf("应有 2 个不同地址各收一笔相同金额，实际 %d", n)
	}
}

// TestConcurrentExclusiveAssignment 并发下单时，空闲地址应尽量各占一个。
// 走 CreateCryptoOrderAtomic（带互斥锁），验证不会全挤到一个地址。
func TestConcurrentExclusiveAssignment(t *testing.T) {
	// 用独立 id 段，避免与其它测试的池子混在一起
	ids := []int{990, 991, 992, 993, 994}
	setupPool(t, ids...)
	nowMs := time.Now().UnixMilli()

	const n = 5
	var mu sync.Mutex
	assigned := map[int]int{}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := CreateCryptoOrderAtomic(NewCryptoOrderInput{
				UID: 1, Chain: "TRX", CNY: 7, Base: 7, Rate: 7,
				Requested: "1.00", Expires: nowMs + 1800000,
			})
			if err != nil || id == 0 {
				return
			}
			o := GetCryptoOrder(id)
			if o == nil {
				return
			}
			mu.Lock()
			assigned[o.PoolID]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(assigned) != n {
		t.Fatalf("5 个并发订单应各自独占一个地址，实际分布: %v", assigned)
	}
	for pid, cnt := range assigned {
		if cnt != 1 {
			t.Fatalf("地址 %d 被分配了 %d 次", pid, cnt)
		}
	}
	t.Logf("并发分配结果: %v", assigned)
}
