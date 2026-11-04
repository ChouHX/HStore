package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
)

// setupStoreDB 用临时库跑一遍完整的建表 / 迁移 / 种子，供商城接口测试使用。
// 必须在用到 store 包之前调用（db 是进程内单例）。
func setupStoreDB(t *testing.T) {
	t.Helper()
	t.Setenv("FAKA_DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	if _, err := db.Open(); err != nil {
		t.Fatalf("open db: %v", err)
	}
}

func insertProduct(t *testing.T, goodsID int, fulfillment string, fallbackGoodsID, stockCount int) {
	t.Helper()
	_, err := db.Must().Exec(
		`INSERT INTO products(goods_id, name, platform, updated_at, stock_count, fulfillment, fallback_goods_id, root_category_id, enabled, status)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		goodsID, "测试商品", "self", 1, stockCount, fulfillment, fallbackGoodsID, 930001, 1, 1,
	)
	if err != nil {
		t.Fatalf("insert product %d: %v", goodsID, err)
	}
}

func insertSelfStock(t *testing.T, goodsID, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := db.Must().Exec(
			"INSERT INTO self_stock(goods_id, content, status, source, source_id, created_at) VALUES(?,?,?,?,?,?)",
			goodsID, "card", "available", "test", fmt.Sprintf("s-%d-%d", goodsID, i), 1,
		); err != nil {
			t.Fatalf("insert self stock for %d: %v", goodsID, err)
		}
	}
}

// 商城列表接口只能下发白名单字段：成本价、上游平台、目录 ID、同步时间等后台字段
// 一旦出现在公开响应里就等于把核心数据交出去了。
func TestStoreProductsWhitelist(t *testing.T) {
	setupStoreDB(t)

	insertProduct(t, 950001, "upstream", 0, 42)
	insertProduct(t, 950002, "self", 12345, 0)
	insertSelfStock(t, 950002, 7)

	a := &API{mux: http.NewServeMux()}
	req := httptest.NewRequest(http.MethodGet, "/api/store/products?rootId=930001&pageSize=50", nil)
	w := httptest.NewRecorder()
	a.handleStoreProducts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列表接口状态码 %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		OK    bool                     `json:"ok"`
		Items []map[string]interface{} `json:"items"`
		Total int                      `json:"total"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &resp); e != nil {
		t.Fatalf("解析响应失败: %v body=%s", e, w.Body.String())
	}
	if !resp.OK || resp.Total != 2 || len(resp.Items) != 2 {
		t.Fatalf("应返回 2 件商品，实际 total=%d items=%d", resp.Total, len(resp.Items))
	}

	// 后台字段一个都不能出现（包括大小写 / 下划线变体）。
	banned := []string{"costPrice", "cost_price", "salesPrice", "faceValue", "dirIds", "dir_ids",
		"platform", "manualPrice", "priceLocked", "updatedAt", "fallbackGoodsId", "fallback_goods_id",
		"rootCategoryId", "detailJson", "stockSyncedAt"}
	body := w.Body.String()
	for _, k := range banned {
		if strings.Contains(body, k) {
			t.Errorf("商城列表泄露后台字段 %q: %s", k, body)
		}
	}

	// 自营商品下发的就是真实库存条数，并带上 hasFallback 供前端判断能否下单。
	for _, it := range resp.Items {
		switch int(it["goodsId"].(float64)) {
		case 950002:
			if got := it["stockCount"].(float64); got != 7 {
				t.Errorf("自营商品库存应为真实值 7，实际 %v", got)
			}
			if it["fulfillment"] != "self" || it["hasFallback"] != true {
				t.Errorf("自营商品应带 fulfillment=self / hasFallback=true，实际 %v", it)
			}
		case 950001:
			if got := it["stockCount"].(float64); got != 42 {
				t.Errorf("上游商品库存应为 42，实际 %v", got)
			}
			if _, has := it["hasFallback"]; has {
				t.Errorf("上游商品不应出现 hasFallback: %v", it)
			}
		}
	}
}

// 自营商品详情返回真实可用条数（不是「有货 / 缺货」标记），
// 上游商品的详情走白名单，成本价等字段同样不能出现。
func TestGoodsDetailProjection(t *testing.T) {
	setupStoreDB(t)

	insertProduct(t, 960001, "self", 0, 0)
	insertSelfStock(t, 960001, 5)
	insertProduct(t, 960002, "upstream", 0, 0)

	raw := json.RawMessage(`{"goodsId":960002,"name":"上游商品","salesPrice":1.5,"faceValue":2,` +
		`"goodsType":1,"status":1,"stockCount":0,"minQuantity":1,"maxQuantity":10,` +
		`"warrantyDays":30,"goodsDescribe":"说明","goodsDetail":"详情","buyNotice":"须知",` +
		`"imgUrl":"https://upstream.example/a.png","dirIds":[1,2],"multiple":3,` +
		`"goodsTags":[{"n":"x"}],"serviceTags":[{"n":"y"}],"limitBalance":88.5}`)
	if _, e := db.Must().Exec(
		"UPDATE products SET detail_json = ?, detail_synced_at = ? WHERE goods_id = 960002",
		string(raw), 1,
	); e != nil {
		t.Fatalf("写入详情缓存失败: %v", e)
	}

	// 自营：真实库存 5
	self := store.GetProduct(960001)
	if self == nil {
		t.Fatal("商品 960001 不存在")
	}
	detail := selfProductDetail(self)
	if detail.StockCount != 5 {
		t.Errorf("自营详情库存应为真实值 5，实际 %d", detail.StockCount)
	}
	if detail.MinQuantity != 1 || detail.MaxQuantity <= 0 {
		t.Errorf("自营详情购买数量区间异常: %d~%d", detail.MinQuantity, detail.MaxQuantity)
	}

	// 上游：白名单投影
	got, e := buildStoreDetail(960002, raw)
	if e != nil {
		t.Fatalf("构建上游详情失败: %v", e)
	}
	if got.GoodsID != 960002 || got.Name != "上游商品" || got.MaxQuantity != 10 || got.WarrantyDays != 30 {
		t.Errorf("上游详情字段缺失: %+v", got)
	}
	// 图片换成本地代理地址，不再暴露上游域名
	if !strings.HasPrefix(got.ImgURL, "/api/img/") {
		t.Errorf("详情图片应换成本地地址，实际 %q", got.ImgURL)
	}
	// 库存以本地同步值为准（这里是 0），并且不含后台字段
	enc, e := json.Marshal(got)
	if e != nil {
		t.Fatalf("序列化失败: %v", e)
	}
	for _, k := range []string{"dirIds", "multiple", "goodsTags", "serviceTags", "limitBalance", "platform"} {
		if strings.Contains(string(enc), k) {
			t.Errorf("上游详情泄露字段 %q: %s", k, string(enc))
		}
	}
	// 后台编辑商品要用到的字段必须保留
	for _, k := range []string{"name", "goodsDescribe", "goodsDetail", "imgUrl"} {
		if !strings.Contains(string(enc), k) {
			t.Errorf("上游详情缺少后台编辑所需字段 %q: %s", k, string(enc))
		}
	}
}

// 库存接口直接返回库里的值（自营取本地可用条数），不能在请求里同步回源。
func TestHandleStoreStockReadsLocal(t *testing.T) {
	setupStoreDB(t)

	insertProduct(t, 970001, "upstream", 0, 12)
	insertProduct(t, 970002, "self", 0, 9999)
	insertSelfStock(t, 970002, 3)

	a := &API{mux: http.NewServeMux()}
	req := httptest.NewRequest(http.MethodGet, "/api/store/stock?ids=970001,970002", nil)
	w := httptest.NewRecorder()
	a.handleStoreStock(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("库存接口状态码 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK     bool           `json:"ok"`
		Stocks map[string]int `json:"stocks"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &resp); e != nil {
		t.Fatalf("解析响应失败: %v", e)
	}
	if resp.Stocks["970001"] != 12 {
		t.Errorf("上游商品库存应为 12，实际 %d", resp.Stocks["970001"])
	}
	if resp.Stocks["970002"] != 3 {
		t.Errorf("自营商品库存应为本地真实值 3，实际 %d", resp.Stocks["970002"])
	}
}
