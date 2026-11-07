package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hstore/backend/internal/store"
)

// 批量修改兑换码：按筛选条件改关联商品，已使用的码保持不动。
func TestAdminRedeemUpdateCodes(t *testing.T) {
	setupStoreDB(t)
	a := &API{mux: http.NewServeMux()}

	if _, e := store.CreateSelfProduct(store.SelfProductInput{
		GoodsID: 950901, Name: "批量修改商品", Price: 1, Enabled: true,
	}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = store.DeleteSelfProduct(950901) })

	batchID, _ := store.CreateCodesBatch(store.CreateCodesInput{FaceValue: 5, Count: 3})
	t.Cleanup(func() { store.DeleteBatch(batchID) })

	items := store.ListCodes(store.CodeFilter{BatchID: batchID}, 10, 0)
	if len(items) != 3 {
		t.Fatalf("应生成 3 张，实际 %d", len(items))
	}
	if !store.UseBalanceCode(items[0].ID, 1) {
		t.Fatal("核销应成功")
	}

	// 预览接口：总数 3、可修改 2（1 张已使用）
	req := httptest.NewRequest(http.MethodGet,
		"/api/admin/redeem/count?batchId="+itoa64(batchID), nil)
	req.AddCookie(adminCookie(t))
	w := httptest.NewRecorder()
	a.handleAdminRedeemCount(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("预览接口状态码 %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"total":3`) || !strings.Contains(w.Body.String(), `"updatable":2`) {
		t.Fatalf("预览统计不正确: %s", w.Body.String())
	}

	// 按批次批量改商品 + 件数
	code, resp := postAdminAction(t, a.handleAdminRedeemPost, map[string]any{
		"action": "updateCodes",
		"filter": map[string]any{"batchId": batchID},
		"update": map[string]any{"goodsId": 950901, "goodsCount": 2},
	})
	if code != http.StatusOK || resp["ok"] != true {
		t.Fatalf("批量修改失败: code=%d resp=%v", code, resp)
	}
	if n, _ := resp["count"].(float64); int(n) != 2 {
		t.Fatalf("应修改 2 张，实际 %v", resp["count"])
	}

	for _, c := range store.ListCodes(store.CodeFilter{BatchID: batchID}, 10, 0) {
		if c.ID == items[0].ID {
			if c.GoodsID != nil {
				t.Fatalf("已使用的码不应被修改: %+v", c)
			}
			continue
		}
		if c.GoodsID == nil || *c.GoodsID != 950901 || c.GoodsCount != 2 || c.FaceValue != 0 {
			t.Fatalf("未使用的码应改成商品码: %+v", c)
		}
	}

	// 商品不存在时拒绝
	code, _ = postAdminAction(t, a.handleAdminRedeemPost, map[string]any{
		"action": "updateCodes",
		"filter": map[string]any{"batchId": batchID},
		"update": map[string]any{"goodsId": 99999999},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("商品不存在应返回 400，实际 %d", code)
	}

	// 没带筛选条件时拒绝，避免误改全库
	code, _ = postAdminAction(t, a.handleAdminRedeemPost, map[string]any{
		"action": "updateCodes",
		"update": map[string]any{"faceValue": 9},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("缺少筛选条件应返回 400，实际 %d", code)
	}

	// 没带修改项时拒绝
	code, _ = postAdminAction(t, a.handleAdminRedeemPost, map[string]any{
		"action": "updateCodes",
		"filter": map[string]any{"batchId": batchID},
		"update": map[string]any{},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("缺少修改项应返回 400，实际 %d", code)
	}
}

// 传 codeIds 时只改选中的那几张。
func TestAdminRedeemUpdateCodesSelected(t *testing.T) {
	setupStoreDB(t)
	a := &API{mux: http.NewServeMux()}

	batchID, _ := store.CreateCodesBatch(store.CreateCodesInput{FaceValue: 2, Count: 3})
	t.Cleanup(func() { store.DeleteBatch(batchID) })

	items := store.ListCodes(store.CodeFilter{BatchID: batchID}, 10, 0)
	if len(items) != 3 {
		t.Fatalf("应生成 3 张，实际 %d", len(items))
	}

	code, resp := postAdminAction(t, a.handleAdminRedeemPost, map[string]any{
		"action":  "updateCodes",
		"codeIds": []int{items[0].ID, items[1].ID},
		"update":  map[string]any{"faceValue": 12.5, "clearDates": true},
	})
	if code != http.StatusOK || resp["ok"] != true {
		t.Fatalf("批量修改失败: code=%d resp=%v", code, resp)
	}
	if n, _ := resp["count"].(float64); int(n) != 2 {
		t.Fatalf("应修改 2 张，实际 %v", resp["count"])
	}

	byID := map[int]float64{}
	for _, c := range store.ListCodes(store.CodeFilter{BatchID: batchID}, 10, 0) {
		byID[c.ID] = c.FaceValue
	}
	if byID[items[0].ID] != 12.5 || byID[items[1].ID] != 12.5 {
		t.Fatalf("选中项面值应改为 12.5: %v", byID)
	}
	if byID[items[2].ID] != 2 {
		t.Fatalf("未选中项面值不应变化: %v", byID)
	}
}
