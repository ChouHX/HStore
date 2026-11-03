package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

// 兑换码取件单没有 contact，公开详情接口必须认兑换码作为第二重凭证，
// 否则用户在「订单查询」里点开详情只会看到「请先登录」。
func TestHandleOrderQueryDetailAcceptsRedeemCode(t *testing.T) {
	setupStoreDB(t)

	id, e := store.CreateSelfProduct(store.SelfProductInput{GoodsID: 940301, Name: "自营卡密商品", Price: 0.5, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = store.DeleteSelfProduct(id) })

	goodsID := id
	store.CreateCodes(store.CreateCodesInput{FaceValue: 1, GoodsID: &goodsID, GoodsCount: 1, Count: 1})
	codes := store.ListCodes(store.CodeFilter{GoodsID: id}, 10, 0)
	if len(codes) != 1 {
		t.Fatalf("应生成 1 条兑换码，实际 %d", len(codes))
	}
	code := codes[0]

	const orderNo = "HSTESTDETAIL001"
	store.UpsertOrder(store.UpsertOrderInput{
		OrderNumber: orderNo, GoodsID: id, GoodsName: "自营卡密商品",
		Count: 1, Status: 3, Result: "兑换码取件", Platform: "self", Source: "user",
	})
	store.UpsertCards(orderNo, []model.Card{{CardID: "SELF-940301-1", CardNo: "user@outlook.com----pw"}})
	store.FinishCode(code.ID, orderNo)

	call := func(query string) *httptest.ResponseRecorder {
		a := &API{mux: http.NewServeMux()}
		req := httptest.NewRequest(http.MethodGet, "/api/orders/query/detail?"+query, nil)
		w := httptest.NewRecorder()
		a.handleOrderQueryDetail(w, req)
		return w
	}

	// 1) 正确的兑换码：放行并带回卡密。
	if w := call("orderNumber=" + orderNo + "&code=" + code.Code); w.Code != http.StatusOK {
		t.Fatalf("兑换码凭证应放行，状态码 %d body=%s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), "user@outlook.com----pw") {
		t.Fatalf("应返回卡密，实际 body=%s", w.Body.String())
	}

	// 2) 错误兑换码：拒绝。
	if w := call("orderNumber=" + orderNo + "&code=ZZZZ-ZZZZ-ZZZZ"); w.Code != http.StatusForbidden {
		t.Fatalf("错误兑换码应拒绝，状态码 %d", w.Code)
	}

	// 3) 完全不给凭证：拒绝。
	if w := call("orderNumber=" + orderNo); w.Code != http.StatusBadRequest {
		t.Fatalf("缺凭证应返回 400，实际 %d", w.Code)
	}
}
