package store

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

func seedSyncOrder(t *testing.T, source string) (*model.LocalOrder, int) {
	t.Helper()
	u, err := CreateGuestUser()
	if err != nil {
		t.Fatal(err)
	}
	if source == "user" {
		if _, err := db.Must().Exec(`UPDATE users SET role='user' WHERE uid=?`, u.UID); err != nil {
			t.Fatal(err)
		}
	}
	no := NewSystemOrderNo()
	UpsertOrder(UpsertOrderInput{OrderNumber: no, UpstreamOrderNo: "remote-" + no,
		OuterNumber: "outer-" + no, GoodsID: 6399, GoodsName: "delivery test", Count: 5,
		Money: 39, SaleMoney: 39, CostMoney: 19.5, Status: 1, Source: source, UserUID: &u.UID})
	if _, err := db.Must().Exec(`UPDATE orders SET updated_at=?,created_at=? WHERE order_number=?`,
		now()-60000, now()-10*60*1000, no); err != nil {
		t.Fatal(err)
	}
	id := 0
	if source == "guest" {
		id, err = InsertGuestPurchase(model.GuestPurchase{Token: "token-" + no, UID: u.UID,
			Channel: "alipay", Status: "fulfilled", UpstreamOrderNo: no, OuterNumber: "outer-" + no,
			GoodsID: 6399, GoodsCount: 5, AmountCny: 39})
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Must().Exec(`DELETE FROM guest_purchases WHERE upstream_order_no=? OR outer_number=?`, no, "outer-"+no)
		_, _ = db.Must().Exec(`DELETE FROM cards WHERE order_number=?`, no)
		_, _ = db.Must().Exec(`DELETE FROM orders WHERE order_number=?`, no)
	})
	return GetOrder(no), id
}

func TestOrderSyncLateDeliveryAndRefund(t *testing.T) {
	for _, status := range []int{3, 4, 5} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			o, guestID := seedSyncOrder(t, "guest")
			beforeUser := GetUser(*o.UserUID)
			detail := model.OrderDetail{OrderNumber: o.UpstreamOrderNo, OuterNumber: o.OuterNumber,
				Status: status, Count: 5, Money: 19.5, Result: "发货成功"}
			if status == 3 {
				for i := 0; i < 5; i++ {
					detail.Cards = append(detail.Cards, model.Card{CardID: fmt.Sprintf("%s-%d", o.OrderNumber, i), CardNo: "test-card"})
				}
			} else {
				detail.Result = "上游充值失败：商品库存不足"
				detail.RetMoney = 19.5
			}
			if err := ApplyOrderSync(o.OrderNumber, detail); err != nil {
				t.Fatal(err)
			}
			got := GetOrder(o.OrderNumber)
			if got.Status != status || got.Money != 39 || got.SaleMoney != 39 || got.CostMoney != 19.5 || got.Count != 5 {
				t.Fatalf("state or local sale data changed incorrectly: %+v", got)
			}
			if len(got.Cards) != len(detail.Cards) {
				t.Fatalf("missing cards: %d", len(got.Cards))
			}
			gp := GetGuestPurchase(guestID)
			if gp.Status != guestFulfillmentStatus(status) || (gp.FulfilledAt != nil) != (status == 3) {
				t.Fatalf("guest delivery state incorrect: %+v", gp)
			}
			if ClaimGuestPurchase(guestID) {
				t.Fatal("accepted purchase must never be purchased again")
			}
			for _, pending := range ListPendingGuestPurchases(1000) {
				if pending.ID == guestID {
					t.Fatal("accepted purchase entered create-order retry queue")
				}
			}
			// Replayed/stale responses cannot add cards, regress status, or settle twice.
			_ = ApplyOrderSync(o.OrderNumber, detail)
			detail.Status = 1
			_ = ApplyOrderSync(o.OrderNumber, detail)
			if got := GetOrder(o.OrderNumber); got.Status != status || got.Settled != 0 {
				t.Fatalf("terminal state regressed or external payment settled: %+v", got)
			}
			afterUser := GetUser(*o.UserUID)
			if afterUser.Balance != beforeUser.Balance || afterUser.TotalSpent != beforeUser.TotalSpent {
				t.Fatal("sync changed the buyer's balance or consumption")
			}
			if status == 4 {
				var returned float64
				if err := db.Must().QueryRow(`SELECT ret_money FROM orders WHERE order_number=?`, o.OrderNumber).Scan(&returned); err != nil || returned != 19.5 {
					t.Fatalf("remote refund not recorded: %v %v", returned, err)
				}
				text := CustomerOrderResult(status, detail.Result)
				if !strings.Contains(text, "缺货") || strings.Contains(text, "上游") || strings.Contains(text, "已退款") {
					t.Fatalf("unsafe customer message: %q", text)
				}
			}
		})
	}
}

func TestOrderSyncPartialDeliveryAndAtomicRollback(t *testing.T) {
	o, guestID := seedSyncOrder(t, "guest")
	detail := model.OrderDetail{OrderNumber: o.UpstreamOrderNo, Status: 2,
		Cards: []model.Card{{CardID: o.OrderNumber + "-1", CardNo: "first"}}}
	if err := ApplyOrderSync(o.OrderNumber, detail); err != nil {
		t.Fatal(err)
	}
	if got := GetGuestPurchase(guestID); got.Status != "fulfilling" || got.FulfilledAt != nil {
		t.Fatalf("partial order marked complete: %+v", got)
	}
	other, _ := seedSyncOrder(t, "admin")
	UpsertCards(other.OrderNumber, []model.Card{{CardID: "collision-" + o.OrderNumber, CardNo: "other"}})
	detail.Status = 3
	detail.Cards = []model.Card{{CardID: o.OrderNumber + "-2", CardNo: "second"}, {CardID: "collision-" + o.OrderNumber, CardNo: "must-not-overwrite"}}
	if err := ApplyOrderSync(o.OrderNumber, detail); err == nil {
		t.Fatal("cross-order card collision must fail")
	}
	if got := GetOrder(o.OrderNumber); got.Status != 2 || len(got.Cards) != 1 {
		t.Fatalf("failed sync was not rolled back: %+v", got)
	}
	if got := GetOrder(other.OrderNumber); got.Cards[0].CardNo != "other" {
		t.Fatal("unrelated order card was overwritten")
	}
	detail.Cards = []model.Card{{CardID: o.OrderNumber + "-1", CardNo: "first"}, {CardID: o.OrderNumber + "-2", CardNo: "second"}}
	if err := ApplyOrderSync(o.OrderNumber, detail); err != nil {
		t.Fatal(err)
	}
	if got := GetOrder(o.OrderNumber); len(got.Cards) != 2 || got.Status != 3 {
		t.Fatalf("cards not merged idempotently: %+v", got)
	}
}

func TestOrderSyncClaimAndTerminalIsolation(t *testing.T) {
	o, _ := seedSyncOrder(t, "admin")
	var claimed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := ClaimOrderSync(o.OrderNumber)
			if err != nil {
				t.Error(err)
			}
			if ok {
				claimed.Add(1)
			}
		}()
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatalf("expected one claim, got %d", claimed.Load())
	}
	for _, status := range []int{3, 4, 5} {
		_, _ = db.Must().Exec(`UPDATE orders SET status=?,updated_at=? WHERE order_number=?`, status, now()-60000, o.OrderNumber)
		if ok, err := ClaimOrderSync(o.OrderNumber); err != nil || ok {
			t.Fatalf("terminal order was claimed: %d %v", status, err)
		}
	}
	_, _ = db.Must().Exec(`UPDATE orders SET status=1,upstream_order_no='',updated_at=? WHERE order_number=?`, now()-60000, o.OrderNumber)
	if ok, err := ClaimOrderSync(o.OrderNumber); err != nil || ok {
		t.Fatalf("self-fulfilled order was claimed: %v", err)
	}
}

func TestOrderSyncBalanceSettlesOnceAndCodeDoesNot(t *testing.T) {
	for _, code := range []bool{false, true} {
		o, _ := seedSyncOrder(t, "user")
		if code {
			_, err := db.Must().Exec(`INSERT INTO redeem_codes(code,order_number,status,created_at) VALUES(?,?,'used',?)`, o.OrderNumber, o.OrderNumber, now())
			if err != nil {
				t.Fatal(err)
			}
		}
		detail := model.OrderDetail{OrderNumber: o.UpstreamOrderNo, Status: 3, Money: 19.5}
		if err := ApplyOrderSync(o.OrderNumber, detail); err != nil {
			t.Fatal(err)
		}
		_ = ApplyOrderSync(o.OrderNumber, detail)
		want := 39.0
		if code {
			want = 0
		}
		if u := GetUser(*o.UserUID); u.TotalSpent != want || u.Balance != 0 {
			t.Fatalf("incorrect settlement code=%v: %+v", code, u)
		}
	}
}

func TestShortageRefundsWalletBuyerExactlyOnce(t *testing.T) {
	o, _ := seedSyncOrder(t, "user")
	_, _ = db.Must().Exec(`UPDATE users SET balance=10 WHERE uid=?`, *o.UserUID)
	detail := model.OrderDetail{OrderNumber: o.UpstreamOrderNo, OuterNumber: o.OuterNumber,
		Status: 4, Result: "商品库存不足", RetMoney: 19.5}
	if err := ApplyOrderSync(o.OrderNumber, detail); err != nil {
		t.Fatal(err)
	}
	if u := GetUser(*o.UserUID); u.Balance != 49 || u.TotalSpent != 0 {
		t.Fatalf("wallet refund incorrect: %+v", u)
	}
	if !OrderBalanceRefunded(o.OrderNumber) {
		t.Fatal("wallet refund missing from immutable ledger")
	}
	_ = ApplyOrderSync(o.OrderNumber, detail)
	if u := GetUser(*o.UserUID); u.Balance != 49 {
		t.Fatalf("wallet refund repeated: %+v", u)
	}
}

func TestShortageNotificationQueueIsDurableAndIdempotent(t *testing.T) {
	o, _ := seedSyncOrder(t, "guest")
	detail := model.OrderDetail{OrderNumber: o.UpstreamOrderNo, OuterNumber: o.OuterNumber,
		Status: 4, Result: "商品库存不足", RetMoney: 19.5}
	if err := ApplyOrderSync(o.OrderNumber, detail); err != nil {
		t.Fatal(err)
	}
	EnsureShortageNotifications()
	item, err := ClaimShortageNotification()
	if err != nil || item == nil || item.OrderNumber != o.OrderNumber || item.Attempts != 1 {
		t.Fatalf("notification was not claimed: %+v %v", item, err)
	}
	CompleteShortageNotification(o.OrderNumber, errors.New("temporary mail failure"))
	if early, err := ClaimShortageNotification(); err != nil || early != nil {
		t.Fatalf("mail retry must wait at least one minute: %+v %v", early, err)
	}
	_, _ = db.Must().Exec(`UPDATE order_notifications SET updated_at=? WHERE order_number=?`, now()-61000, o.OrderNumber)
	item, err = ClaimShortageNotification()
	if err != nil || item == nil || item.Attempts != 2 {
		t.Fatalf("failed notification was not retryable: %+v %v", item, err)
	}
	CompleteShortageNotification(o.OrderNumber, nil)
	item, err = ClaimShortageNotification()
	if err != nil || item != nil {
		t.Fatalf("sent notification was claimed again: %+v %v", item, err)
	}
}

func TestGuestLinkDoesNotCompleteAcceptedOrder(t *testing.T) {
	o, id := seedSyncOrder(t, "guest")
	paidAt := time.Now().Add(-5 * time.Minute).UnixMilli()
	if err := LinkGuestPurchaseOrder(id, o.OrderNumber, &paidAt); err != nil {
		t.Fatal(err)
	}
	if gp := GetGuestPurchase(id); gp.Status != "fulfilling" || gp.FulfilledAt != nil || gp.PaidAt == nil || *gp.PaidAt != paidAt {
		t.Fatalf("accepted order must keep waiting: %+v", gp)
	}
	other, _ := seedSyncOrder(t, "admin")
	if err := LinkGuestPurchaseOrder(id, other.OrderNumber, nil); err == nil {
		t.Fatal("must not link someone else's order")
	}
}
