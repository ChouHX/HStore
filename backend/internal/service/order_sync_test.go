package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

func TestPendingOrderContinuesAfterTimeout(t *testing.T) {
	no := store.NewSystemOrderNo()
	store.UpsertOrder(store.UpsertOrderInput{OrderNumber: no, UpstreamOrderNo: "remote-" + no,
		GoodsID: 1, Count: 1, Money: 39, SaleMoney: 39, Status: 1, Source: "guest"})
	age := func() {
		t.Helper()
		if _, err := db.Must().Exec(`UPDATE orders SET updated_at=?,created_at=? WHERE order_number=?`,
			time.Now().Add(-time.Minute).UnixMilli(), time.Now().Add(-24*time.Hour).UnixMilli(), no); err != nil {
			t.Fatal(err)
		}
	}
	age()
	calls := 0
	fetch := func(ctx context.Context, upstreamNo string) (*model.OrderDetail, error) {
		calls++
		if upstreamNo != "remote-"+no {
			t.Fatal("queried wrong order")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("query must have a deadline")
		}
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		if calls == 2 {
			return &model.OrderDetail{OrderNumber: upstreamNo, Status: 1}, nil
		}
		return &model.OrderDetail{OrderNumber: upstreamNo, Status: 3, Money: 19.5,
			Cards: []model.Card{{CardID: "card-" + no, CardNo: "delivered"}}}, nil
	}
	if err := syncPendingOrder(context.Background(), no, fetch); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected transient timeout: %v", err)
	}
	if got := store.GetOrder(no); got.Status != 1 || got.Money != 39 {
		t.Fatalf("timeout changed order: %+v", got)
	}
	_ = syncPendingOrder(context.Background(), no, fetch)
	if calls != 1 {
		t.Fatal("failed request was not throttled")
	}
	for i := 0; i < 2; i++ {
		age()
		if err := syncPendingOrder(context.Background(), no, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.GetOrder(no); got.Status != 3 || len(got.Cards) != 1 || got.Money != 39 {
		t.Fatalf("late delivery was not recovered: %+v", got)
	}
	age()
	_ = syncPendingOrder(context.Background(), no, fetch)
	if calls != 3 {
		t.Fatal("completed order was queried again")
	}
}

func TestGuestAcceptedOrderViewAndRetryGuard(t *testing.T) {
	u, err := store.CreateGuestUser()
	if err != nil {
		t.Fatal(err)
	}
	no := store.NewSystemOrderNo()
	payment, err := store.CreateAlipayOrder(u.UID, store.NewAlipayOrderNo(), store.HashAlipayToken(no), "test-qr", 3900, 99, 60000, 60000)
	if err != nil {
		t.Fatal(err)
	}
	store.UpsertOrder(store.UpsertOrderInput{OrderNumber: no, UpstreamOrderNo: "remote-" + no,
		OuterNumber: "outer-" + no, GoodsID: 1, Count: 1, Money: 39, SaleMoney: 39, Status: 1,
		Source: "guest", UserUID: &u.UID})
	id, err := store.InsertGuestPurchase(model.GuestPurchase{Token: no, UID: u.UID, Channel: "alipay",
		PaymentOrderID: payment.ID, Status: "fulfilled", UpstreamOrderNo: no, OuterNumber: "outer-" + no})
	if err != nil {
		t.Fatal(err)
	}
	view, err := guestOrderView(store.GetGuestPurchase(id))
	if err != nil || view.Status != "fulfilling" {
		t.Fatalf("legacy falsely completed guest view: %+v %v", view, err)
	}
	if err := FulfillGuestPurchase(id); err != nil {
		t.Fatal(err)
	}
	if gp := store.GetGuestPurchase(id); gp.Status != "fulfilling" {
		t.Fatalf("accepted guest did not remain pending: %+v", gp)
	}
	if err := store.ApplyOrderSync(no, model.OrderDetail{OrderNumber: "remote-" + no, Status: 4,
		Result: "上游充值失败：商品库存不足", RetMoney: 19.5}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := FulfillGuestPurchase(id); err != nil {
			t.Fatal(err)
		}
	}
	view, err = guestOrderView(store.GetGuestPurchase(id))
	if err != nil || view.Status != "delivery_failed" || !strings.Contains(view.Error, "缺货") || strings.Contains(view.Error, "上游") {
		t.Fatalf("refund view incorrect: %+v %v", view, err)
	}
	if got := store.GetAlipayOrder(payment.ID); got.PaymentStatus != payment.PaymentStatus {
		t.Fatal("supplier refund changed buyer payment state")
	}
}
