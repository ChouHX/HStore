package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

// adminCookie 造一个管理员会话 Cookie，供后台接口测试使用。
func adminCookie(t *testing.T) *http.Cookie {
	t.Helper()
	admin := store.GetUser(1)
	if admin == nil || admin.Role != "admin" {
		t.Fatalf("种子管理员不存在: %+v", admin)
	}
	return &http.Cookie{Name: SessionCookie, Value: db.SignSession(admin.UID)}
}

// postAdminAction 以管理员身份 POST 一个后台动作，返回解析后的 JSON。
func postAdminAction(t *testing.T, fn http.HandlerFunc, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.AddCookie(adminCookie(t))
	w := httptest.NewRecorder()
	fn(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// 后台删除测试订单：未到账可删，已到账拒绝并给出明确提示。
func TestAdminDeletePaymentOrder(t *testing.T) {
	setupStoreDB(t)
	a := &API{mux: http.NewServeMux()}

	// 支付宝：未支付订单可删
	order, err := store.CreateAlipayOrder(1, store.NewAlipayOrderNo(), store.HashAlipayToken("api-del-1"),
		"https://qr.alipay.com/api-del-1", 6100, 99, 60*1000, 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	code, resp := postAdminAction(t, a.handleAdminAlipayPost, map[string]any{
		"action": "deleteOrder", "orderId": order.ID,
	})
	if code != http.StatusOK || resp["ok"] != true {
		t.Fatalf("删除未支付订单失败: code=%d resp=%v", code, resp)
	}
	if store.GetAlipayOrder(order.ID) != nil {
		t.Fatal("订单未被删除")
	}

	// 支付宝：已到账订单拒绝删除，且返回中文提示
	paid, err := store.CreateAlipayOrder(1, store.NewAlipayOrderNo(), store.HashAlipayToken("api-del-2"),
		"https://qr.alipay.com/api-del-2", 6200, 99, 60*1000, 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	ledgerID, _, err := store.InsertAlipayLedgerEntry(storeTestLedgerEntry(paid.PayableCents, "api-del-2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReconcileAlipayLedgerEntry(ledgerID); err != nil {
		t.Fatal(err)
	}
	code, resp = postAdminAction(t, a.handleAdminAlipayPost, map[string]any{
		"action": "deleteOrder", "orderId": paid.ID,
	})
	if code != http.StatusBadRequest || resp["ok"] == true {
		t.Fatalf("已到账订单应拒绝删除: code=%d resp=%v", code, resp)
	}
	if msg, _ := resp["message"].(string); msg == "" {
		t.Fatalf("拒绝删除应返回提示文案: %v", resp)
	}
	if store.GetAlipayOrder(paid.ID) == nil {
		t.Fatal("已到账订单不应被删除")
	}

	// USDT：未到账订单可删
	cryptoID := store.InsertCryptoOrder(1, 0, "uid-api-del", "TTrxApiDel", "TRX",
		10, "1.4", "1.403", 7.1, 9.95, time.Now().UnixMilli()+60*1000)
	if cryptoID == 0 {
		t.Fatal("创建 USDT 订单失败")
	}
	code, resp = postAdminAction(t, a.handleAdminCryptoPost, map[string]any{
		"action": "deleteOrder", "orderId": cryptoID,
	})
	if code != http.StatusOK || resp["ok"] != true {
		t.Fatalf("删除未到账 USDT 订单失败: code=%d resp=%v", code, resp)
	}
	if store.GetCryptoOrder(cryptoID) != nil {
		t.Fatal("USDT 订单未被删除")
	}

	// 缺少订单号时给出 400，而不是静默成功
	code, _ = postAdminAction(t, a.handleAdminAlipayPost, map[string]any{"action": "deleteOrder"})
	if code != http.StatusBadRequest {
		t.Fatalf("缺少订单号应返回 400，实际 %d", code)
	}
}

// storeTestLedgerEntry 造一条与订单金额一致、刚发生的收入流水。
func storeTestLedgerEntry(amountCents int64, key string) model.AlipayLedgerEntry {
	return model.AlipayLedgerEntry{
		ExternalEventID:   "test:api-delete:" + key,
		AccountLogID:      "test-api-delete-" + key,
		OccurredAt:        time.Now().UnixMilli(),
		OccurredPrecision: 1000,
		AmountCents:       amountCents,
		Direction:         "CREDIT",
		State:             "UNALLOCATED",
		RawJSON:           `{}`,
	}
}
