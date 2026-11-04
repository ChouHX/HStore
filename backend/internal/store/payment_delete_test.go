package store

import (
	"testing"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// TestDeleteAlipayOrder 后台删除测试订单：未到账可删且释放金额槽，已到账拒绝。
func TestDeleteAlipayOrder(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()

	// 1) 未支付订单可删除，金额槽一并释放
	order, err := CreateAlipayOrder(1, NewAlipayOrderNo(), HashAlipayToken("del-token-1"),
		"https://qr.alipay.com/del-1", 7000, 99, 60*1000, 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	var slotCount int
	if err := d.QueryRow("SELECT COUNT(*) FROM alipay_amount_slots WHERE order_id=?", order.ID).Scan(&slotCount); err != nil {
		t.Fatal(err)
	}
	if slotCount != 1 {
		t.Fatalf("金额槽未占用: %d", slotCount)
	}
	deleted, err := DeleteAlipayOrder(order.ID)
	if err != nil {
		t.Fatalf("删除未支付订单失败: %v", err)
	}
	if deleted == nil || deleted.ID != order.ID {
		t.Fatalf("返回的订单不对: %+v", deleted)
	}
	if GetAlipayOrder(order.ID) != nil {
		t.Fatal("订单未被删除")
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM alipay_amount_slots WHERE order_id=?", order.ID).Scan(&slotCount); err != nil {
		t.Fatal(err)
	}
	if slotCount != 0 {
		t.Fatalf("金额槽未释放: %d", slotCount)
	}

	// 2) 已到账订单拒绝删除，且数据保持完整
	paidOrder, err := CreateAlipayOrder(1, NewAlipayOrderNo(), HashAlipayToken("del-token-2"),
		"https://qr.alipay.com/del-2", 8000, 99, 60*1000, 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	entryID, _, err := InsertAlipayLedgerEntry(model.AlipayLedgerEntry{
		ExternalEventID:   "test:delete-1",
		AccountLogID:      "test-delete-1",
		OccurredAt:        time.Now().UnixMilli(),
		OccurredPrecision: 1000,
		AmountCents:       paidOrder.PayableCents,
		Direction:         "CREDIT",
		State:             "UNALLOCATED",
		RawJSON:           `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileAlipayLedgerEntry(entryID); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteAlipayOrder(paidOrder.ID); err == nil {
		t.Fatal("已到账订单不应允许删除")
	}
	still := GetAlipayOrder(paidOrder.ID)
	if still == nil || still.PaymentStatus != "CONFIRMED" {
		t.Fatalf("已到账订单被破坏: %+v", still)
	}
}

// TestDeleteAlipayOrderCleansGuestPurchase 删除支付订单时清掉指向它的未履约游客单，
// 否则 guestOrderView 会报「支付订单不存在」，后台轮询也会一直扫到它。
func TestDeleteAlipayOrderCleansGuestPurchase(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	user, err := CreateGuestUser()
	if err != nil {
		t.Fatal(err)
	}
	order, err := CreateAlipayOrder(1, NewAlipayOrderNo(), HashAlipayToken("del-token-3"),
		"https://qr.alipay.com/del-3", 9000, 99, 60*1000, 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	id, err := InsertGuestPurchase(model.GuestPurchase{
		Token: "del-guest-token-3", UID: user.UID, GuestUser: 1, Channel: "alipay", PaymentOrderID: order.ID,
		GoodsID: 1, GoodsName: "测试商品", GoodsCount: 1, AttachJSON: "[]",
		AmountCny: 1, Status: "waiting", OuterNumber: "DEL-OUTER-3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteAlipayOrder(order.ID); err != nil {
		t.Fatal(err)
	}
	if GetGuestPurchase(id) != nil {
		t.Fatal("未履约的游客单应随支付订单一起删除")
	}
}

// TestDeleteCryptoOrder 未到账的 USDT 订单可删，已到账拒绝。
func TestDeleteCryptoOrder(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	d := db.Must()
	nowMs := time.Now().UnixMilli()
	_, _ = d.Exec(`INSERT OR IGNORE INTO bybit_pool(id, sub_uid, address_trx, address_evm, chain, status, updated_at)
		VALUES(980,'uid-980','TTrx980','0xevm980','TRX','available',?)`, nowMs)

	id := InsertCryptoOrder(1, 980, "uid-980", "TTrx980", "TRX", 10, "1.4", "1.401", 7.1, 9.95, nowMs+60*1000)
	if id == 0 {
		t.Fatal("创建 USDT 订单失败")
	}
	order := GetCryptoOrder(id)
	if order == nil || order.Status != "waiting" {
		t.Fatalf("订单状态不对: %+v", order)
	}
	if _, err := DeleteCryptoOrder(id); err != nil {
		t.Fatalf("删除未到账订单失败: %v", err)
	}
	if GetCryptoOrder(id) != nil {
		t.Fatal("订单未被删除")
	}

	// 已到账订单拒绝删除
	paidID := InsertCryptoOrder(1, 980, "uid-980", "TTrx980", "TRX", 10, "1.4", "1.402", 7.1, 9.95, nowMs+60*1000)
	if paidID == 0 {
		t.Fatal("创建 USDT 订单失败")
	}
	paid := GetCryptoOrder(paidID)
	SettleCryptoOrder(paid, "0xtest-delete-txid", "1.402", "测试到账")
	if settled := GetCryptoOrder(paidID); settled == nil || settled.Status != "paid" {
		t.Fatalf("结算失败: %+v", settled)
	}
	if _, err := DeleteCryptoOrder(paidID); err == nil {
		t.Fatal("已到账订单不应允许删除")
	}
	if still := GetCryptoOrder(paidID); still == nil || still.Status != "paid" {
		t.Fatalf("已到账订单被破坏: %+v", still)
	}
}
