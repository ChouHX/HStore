package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hstore-store-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("FAKA_DB_PATH", filepath.Join(dir, "store-test.db"))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestAlipayOrderFlow(t *testing.T) {
	t.Setenv("FAKA_DB_PATH", filepath.Join(t.TempDir(), "alipay-test.db"))
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	nowMs := time.Now().UnixMilli()

	// 1) 唯一金额 + 自动匹配入账
	order, err := CreateAlipayOrder(2, NewAlipayOrderNo(), HashAlipayToken("token-1"), "https://qr.alipay.com/test", 1000, 99, 5*60*1000, 10*60*1000)
	if err != nil {
		t.Fatal(err)
	}
	if order.PayableCents != 1001 {
		t.Fatalf("payable = %d, want 1001", order.PayableCents)
	}
	entryID, inserted, err := InsertAlipayLedgerEntry(model.AlipayLedgerEntry{
		ExternalEventID:   "test:1",
		AccountLogID:      "test-1",
		OccurredAt:        nowMs + 1000,
		OccurredPrecision: 1000,
		AmountCents:       1001,
		Direction:         "CREDIT",
		AlipayOrderNo:     "202610260001",
		TransMemo:         "经营码收款",
		State:             "UNALLOCATED",
		RawJSON:           `{}`,
	})
	if err != nil || !inserted {
		t.Fatalf("insert ledger: %v, inserted=%v", err, inserted)
	}
	result, err := ReconcileAlipayLedgerEntry(entryID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "settled" || result.OrderID != order.ID {
		t.Fatalf("reconcile = %+v", result)
	}
	paid := GetAlipayOrder(order.ID)
	if paid == nil || paid.PaymentStatus != "CONFIRMED" || paid.Credited == nil || *paid.Credited != 10 {
		t.Fatalf("order not settled: %+v", paid)
	}
	user := GetUser(2)
	if user == nil || user.Balance < 110 {
		t.Fatalf("balance = %v, want >= 110", user)
	}
	entry := GetAlipayLedgerEntry(entryID)
	if entry == nil || entry.State != "ALLOCATED" || entry.OrderID == nil || *entry.OrderID != order.ID {
		t.Fatalf("ledger not allocated: %+v", entry)
	}
	if res, _ := ReconcileAlipayLedgerEntry(entryID); res.Kind != "ignored" {
		t.Fatalf("duplicate reconcile = %+v", res)
	}

	// 2) 订单结束后的付款不自动匹配
	lateOrder, err := CreateAlipayOrder(2, NewAlipayOrderNo(), HashAlipayToken("token-2"), "https://qr.alipay.com/test", 2000, 99, 60*1000, 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	lateID, _, err := InsertAlipayLedgerEntry(model.AlipayLedgerEntry{
		ExternalEventID:   "test:2",
		AccountLogID:      "test-2",
		OccurredAt:        lateOrder.EndedAt + 1000,
		OccurredPrecision: 1000,
		AmountCents:       lateOrder.PayableCents,
		Direction:         "CREDIT",
		State:             "UNALLOCATED",
	})
	if err != nil {
		t.Fatal(err)
	}
	lateResult, err := ReconcileAlipayLedgerEntry(lateID)
	if err != nil {
		t.Fatal(err)
	}
	if lateResult.Kind != "unmatched" {
		t.Fatalf("late payment matched unexpectedly: %+v", lateResult)
	}

	// 3) 管理员手工关联，允许金额不一致
	manualOrder, err := CreateAlipayOrder(2, NewAlipayOrderNo(), HashAlipayToken("token-3"), "https://qr.alipay.com/test", 3000, 99, 60*1000, 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	ledgerID, _, err := InsertAlipayLedgerEntry(model.AlipayLedgerEntry{
		ExternalEventID:   "test:3",
		AccountLogID:      "test-3",
		OccurredAt:        time.Now().UnixMilli(),
		OccurredPrecision: 1000,
		AmountCents:       manualOrder.PayableCents + 5,
		Direction:         "CREDIT",
		State:             "UNALLOCATED",
	})
	if err != nil {
		t.Fatal(err)
	}
	settled, err := ManualMatchAlipay(manualOrder.ID, ledgerID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.PaymentStatus != "CONFIRMED" || settled.PaymentBasis != "MANUAL" {
		t.Fatalf("manual settle failed: %+v", settled)
	}
}

func TestGuestUserAndPurchase(t *testing.T) {
	// 复用 TestAlipayOrderFlow 打开的测试数据库（顺序执行时已初始化）。
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	user, err := CreateGuestUser()
	if err != nil {
		t.Fatal(err)
	}
	if user == nil || user.Role != "guest" {
		t.Fatalf("unexpected guest user: %+v", user)
	}
	users := ListUsers(UserFilter{}, 100, 0)
	for _, u := range users {
		if u.Role == "guest" {
			t.Fatalf("guest leaked into admin user list: %+v", u)
		}
	}
	token := "guest-test-token"
	outer := "GUEST-OUTER-1"
	id, err := InsertGuestPurchase(model.GuestPurchase{
		Token: token, UID: user.UID, GuestUser: 1, Channel: "alipay", PaymentOrderID: 1,
		GoodsID: 1, GoodsName: "测试商品", GoodsCount: 1, AttachJSON: "[]",
		AmountCny: 1, Status: "waiting", OuterNumber: outer,
	})
	if err != nil {
		t.Fatal(err)
	}
	gp := GetGuestPurchaseByToken(token)
	if gp == nil || gp.ID != id || gp.GoodsName != "测试商品" {
		t.Fatalf("guest purchase not stored: %+v", gp)
	}
	if !ClaimGuestPurchase(id) {
		t.Fatal("claim guest purchase failed")
	}
	if ClaimGuestPurchase(id) {
		t.Fatal("guest purchase claimed twice")
	}
	MarkGuestPurchaseFulfilled(id, "UPSTREAM-1", nil)
	gp = GetGuestPurchase(id)
	if gp == nil || gp.Status != "fulfilled" || gp.UpstreamOrderNo != "UPSTREAM-1" {
		t.Fatalf("guest purchase not fulfilled: %+v", gp)
	}
}
