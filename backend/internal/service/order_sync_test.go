package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/mail"
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

func TestShortageMailRetriesOnlyUnsentRecipients(t *testing.T) {
	no := store.NewSystemOrderNo()
	store.UpsertOrder(store.UpsertOrderInput{OrderNumber: no, UpstreamOrderNo: "remote-" + no,
		GoodsID: 1, GoodsName: "mail-test-product", Status: 1, Count: 5, SaleMoney: 45, Money: 45, Source: "admin"})
	if err := store.ApplyOrderSync(no, model.OrderDetail{OrderNumber: "remote-" + no, Status: 4, Result: "商品库存不足"}); err != nil {
		t.Fatal(err)
	}
	_, err := db.Must().Exec(`UPDATE order_notifications SET status='sending' WHERE order_number=?`, no)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Must().Exec(`DELETE FROM order_notifications WHERE order_number=?`, no) })
	item := &store.ShortageNotification{OrderNumber: no, GoodsName: "mail-test-product", Count: 5, SaleMoney: 45, RetMoney: 22.5, Result: "商品库存不足"}
	cfg := store.SMTPConfig{Enabled: true, Provider: store.MailProviderResend, ResendDomain: "example.com", ResendAPIKey: "test-only"}
	counts := map[string]int{}
	failSecond := true
	send := func(ctx context.Context, _ store.SMTPConfig, opt mail.SendOptions) error {
		counts[opt.To]++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("mail send must have a deadline")
		}
		if !strings.Contains(opt.Subject, no) || !strings.Contains(opt.Text, "联系客服") && !strings.Contains(opt.Text, "联系用户处理退款") {
			t.Error("missing refund handling details")
		}
		if opt.To == "second@example.com" && failSecond {
			return errors.New("temporary provider failure")
		}
		return nil
	}
	to := []string{"FIRST@example.com", "first@example.com", "second@example.com"}
	if err := sendShortageNotification(context.Background(), item, to, cfg, send); err == nil {
		t.Fatal("expected second recipient failure")
	}
	var raw string
	if err := db.Must().QueryRow(`SELECT sent_recipients FROM order_notifications WHERE order_number=?`, no).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &item.SentRecipients); err != nil {
		t.Fatal(err)
	}
	if len(item.SentRecipients) != 1 || item.SentRecipients[0] != "first@example.com" {
		t.Fatalf("receipt not persisted: %s", raw)
	}
	failSecond = false
	if err := sendShortageNotification(context.Background(), item, to, cfg, send); err != nil {
		t.Fatal(err)
	}
	if counts["first@example.com"] != 1 || counts["second@example.com"] != 2 {
		t.Fatalf("duplicate mail sent: %+v", counts)
	}
	if err := store.CompleteShortageNotification(no, nil); err != nil {
		t.Fatal(err)
	}
}
