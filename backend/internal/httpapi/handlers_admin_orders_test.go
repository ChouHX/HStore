package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

// 后台「订单管理」必须能看到兑换码核销产生的业务订单。
//
// 之前 /admin/orders 只渲染支付单（支付宝 / USDT），orders 表里的兑换码单
// 完全没有接口可查，管理员以为兑换没记录。
func TestAdminOrdersListsRedeemOrders(t *testing.T) {
	setupStoreDB(t)
	a := &API{mux: http.NewServeMux()}

	store.EnsureSelfCategory()
	goodsID, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: 950801, Name: "后台订单测试商品", Price: 0.5, Enabled: true,
	})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = store.DeleteSelfProduct(goodsID) })

	gid := goodsID
	store.CreateCodes(store.CreateCodesInput{FaceValue: 1, GoodsID: &gid, GoodsCount: 1, Count: 1})
	codes := store.ListCodes(store.CodeFilter{GoodsID: gid}, 10, 0)
	if len(codes) != 1 {
		t.Fatalf("应生成 1 条兑换码，实际 %d", len(codes))
	}

	const redeemOrder = "HSAPITESTREDEEM1"
	store.UpsertOrder(store.UpsertOrderInput{
		OrderNumber: redeemOrder, GoodsID: gid, GoodsName: "兑换码核销单",
		Count: 1, Status: 3, Result: "兑换码取件", Platform: "self", Source: "user",
	})
	store.FinishCode(codes[0].ID, redeemOrder)
	store.UpsertCards(redeemOrder, []model.Card{
		{CardID: "SELF-950801-1", CardNo: "card-no", CardPwd: "card-pwd"},
	})
	t.Cleanup(func() {
		_, _ = db.Must().Exec("DELETE FROM cards WHERE order_number = ?", redeemOrder)
		_, _ = db.Must().Exec("DELETE FROM orders WHERE order_number = ?", redeemOrder)
	})

	// 列表：kind=redeem 能查到
	req := httptest.NewRequest(http.MethodGet, "/api/admin/orders?kind=redeem&pageSize=100", nil)
	req.AddCookie(adminCookie(t))
	w := httptest.NewRecorder()
	a.handleAdminOrders(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, redeemOrder) {
		t.Fatalf("兑换码订单未出现在后台列表: %s", body)
	}
	if !strings.Contains(body, `"contact":""`) {
		t.Fatalf("兑换码订单的 contact 应为空字符串而不是缺失: %s", body)
	}

	// 详情：能拿到订单与兑换码
	req = httptest.NewRequest(http.MethodGet, "/api/admin/orders/detail?orderNumber="+redeemOrder, nil)
	req.AddCookie(adminCookie(t))
	w = httptest.NewRecorder()
	a.handleAdminOrderDetail(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("详情状态码 %d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.String()
	if !strings.Contains(body, codes[0].Code) {
		t.Fatalf("详情应带回兑换码 %s: %s", codes[0].Code, body)
	}
	if !strings.Contains(body, "card-no") {
		t.Fatalf("详情应带回卡密: %s", body)
	}

	// 未登录必须拒绝
	req = httptest.NewRequest(http.MethodGet, "/api/admin/orders", nil)
	w = httptest.NewRecorder()
	a.handleAdminOrders(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401，实际 %d", w.Code)
	}

	// 缺少订单号给 400
	req = httptest.NewRequest(http.MethodGet, "/api/admin/orders/detail", nil)
	req.AddCookie(adminCookie(t))
	w = httptest.NewRecorder()
	a.handleAdminOrderDetail(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少订单号应返回 400，实际 %d", w.Code)
	}

	// 不存在的订单给 404
	req = httptest.NewRequest(http.MethodGet, "/api/admin/orders/detail?orderNumber=NOPE", nil)
	req.AddCookie(adminCookie(t))
	w = httptest.NewRecorder()
	a.handleAdminOrderDetail(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("订单不存在应返回 404，实际 %d", w.Code)
	}
}

// kind=normal 不应命中兑换码订单。
func TestAdminOrdersKindNormalExcludesRedeem(t *testing.T) {
	setupStoreDB(t)
	a := &API{mux: http.NewServeMux()}

	const redeemOrder = "HSAPITESTKIND0001"
	store.UpsertOrder(store.UpsertOrderInput{
		OrderNumber: redeemOrder, GoodsID: 950802, GoodsName: "兑换单",
		Count: 1, Status: 3, Result: "兑换码取件", Platform: "self", Source: "user",
	})
	t.Cleanup(func() {
		_, _ = db.Must().Exec("DELETE FROM orders WHERE order_number = ?", redeemOrder)
	})
	// 造一条兑换码指向它，使其成为兑换码订单。
	gid := 950802
	store.CreateCodes(store.CreateCodesInput{FaceValue: 1, GoodsID: &gid, GoodsCount: 1, Count: 1})
	codes := store.ListCodes(store.CodeFilter{GoodsID: gid}, 10, 0)
	if len(codes) == 0 {
		t.Fatal("应生成兑换码")
	}
	store.FinishCode(codes[0].ID, redeemOrder)
	t.Cleanup(func() { _, _ = db.Must().Exec("DELETE FROM redeem_codes WHERE goods_id = ?", gid) })

	req := httptest.NewRequest(http.MethodGet, "/api/admin/orders?kind=normal&pageSize=100", nil)
	req.AddCookie(adminCookie(t))
	w := httptest.NewRecorder()
	a.handleAdminOrders(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d", w.Code)
	}
	if strings.Contains(w.Body.String(), redeemOrder) {
		t.Fatalf("kind=normal 不应包含兑换码订单: %s", w.Body.String())
	}
}
