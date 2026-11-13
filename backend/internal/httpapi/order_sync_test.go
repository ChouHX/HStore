package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

func TestOrderDeliveryViewsHideSupplierDetails(t *testing.T) {
	setupStoreDB(t)
	no := store.NewSystemOrderNo()
	store.UpsertOrder(store.UpsertOrderInput{OrderNumber: no, UpstreamOrderNo: "supplier-" + no,
		OuterNumber: "procurement-" + no, GoodsID: 1, Count: 5, Money: 45, SaleMoney: 45,
		CostMoney: 22.5, Status: 4, Result: "上游充值失败：商品库存不足", Contact: "delivery@example.com"})
	api := New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/orders/query/detail?orderNumber="+no+"&contact=delivery@example.com", nil)
	api.Handler().ServeHTTP(w, req)
	var response struct {
		OK    bool             `json:"ok"`
		Order model.LocalOrder `json:"order"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || !response.OK || response.Order.Status != 4 || !strings.Contains(response.Order.Result, "缺货") {
		t.Fatalf("failure not visible: %s", w.Body.String())
	}
	for _, internal := range []string{"上游", "supplier-", "procurement-", "22.5", "已退款"} {
		if strings.Contains(w.Body.String(), internal) {
			t.Fatalf("internal information leaked: %s", internal)
		}
	}
	original := store.GetOrder(no)
	public := hideUpstreamForUser(original, "user")
	if public == original || original.UpstreamOrderNo == "" || original.Result != "上游充值失败：商品库存不足" {
		t.Fatal("customer projection modified the original administrative record")
	}
	if admin := hideUpstreamForUser(original, "admin"); admin.UpstreamOrderNo == "" || admin.CostMoney != 22.5 {
		t.Fatal("admin lost diagnostic details")
	}
}
