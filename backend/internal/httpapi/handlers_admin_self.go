package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"hstore/backend/internal/store"
)

// ===== 自营商品 =====

// handleAdminSelfProducts 列出全部自营商品（含已下架）。
func (a *API) handleAdminSelfProducts(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	items := store.ListSelfProducts()
	ok(w, map[string]any{"items": items, "total": len(items)})
}

type selfProductBody struct {
	GoodsID         int     `json:"goodsId"`
	Name            string  `json:"name"`
	Price           float64 `json:"price"`
	CostPrice       float64 `json:"costPrice"`
	Description     string  `json:"description"`
	ImageURL        string  `json:"imgUrl"`
	Enabled         *bool   `json:"enabled"`
	FallbackGoodsID int     `json:"fallbackGoodsId"`
}

// handleAdminSelfProductsPost 新建 / 编辑 / 删除自营商品。
func (a *API) handleAdminSelfProductsPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var raw struct {
		Action string            `json:"action"`
		Items  []selfProductBody `json:"items"`
		selfProductBody
	}
	if e := readJSON(r, &raw); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	enabled := raw.Enabled == nil || *raw.Enabled

	switch raw.Action {
	case "create":
		goodsID, e := store.CreateSelfProduct(store.SelfProductInput{
			GoodsID:         raw.GoodsID,
			Name:            raw.Name,
			Price:           raw.Price,
			CostPrice:       raw.CostPrice,
			Description:     raw.Description,
			ImageURL:        raw.ImageURL,
			Enabled:         enabled,
			FallbackGoodsID: raw.FallbackGoodsID,
		})
		if e != nil {
			fail(w, http.StatusBadRequest, e.Error())
			return
		}
		store.WriteAudit(&admin.UID, "self_product_create", "product", itoa(goodsID),
			nil, map[string]any{"name": raw.Name, "price": raw.Price}, "新建自营商品")
		ok(w, map[string]any{"goodsId": goodsID})

	case "update":
		if raw.GoodsID == 0 {
			fail(w, http.StatusBadRequest, "缺少商品编号")
			return
		}
		e := store.UpdateSelfProduct(store.SelfProductInput{
			GoodsID:         raw.GoodsID,
			Name:            raw.Name,
			Price:           raw.Price,
			CostPrice:       raw.CostPrice,
			Description:     raw.Description,
			ImageURL:        raw.ImageURL,
			Enabled:         enabled,
			FallbackGoodsID: raw.FallbackGoodsID,
		})
		if e != nil {
			fail(w, http.StatusBadRequest, e.Error())
			return
		}
		store.WriteAudit(&admin.UID, "self_product_update", "product", itoa(raw.GoodsID),
			nil, map[string]any{"name": raw.Name, "price": raw.Price}, "编辑自营商品")
		ok(w, nil)

	case "delete":
		if raw.GoodsID == 0 {
			fail(w, http.StatusBadRequest, "缺少商品编号")
			return
		}
		deleted, e := store.DeleteSelfProduct(raw.GoodsID)
		if e != nil {
			fail(w, http.StatusBadRequest, e.Error())
			return
		}
		store.WriteAudit(&admin.UID, "self_product_delete", "product", itoa(raw.GoodsID),
			nil, map[string]any{"deleted": deleted}, "删除自营商品")
		msg := ""
		if !deleted {
			msg = "该商品已有历史订单，已改为下架处理"
		}
		ok(w, map[string]any{"deleted": deleted, "message": msg})

	case "toggle":
		if raw.GoodsID == 0 {
			fail(w, http.StatusBadRequest, "缺少商品编号")
			return
		}
		store.SetProductsEnabled([]int{raw.GoodsID}, enabled)
		store.WriteAudit(&admin.UID, "self_product_toggle", "product", itoa(raw.GoodsID),
			nil, map[string]any{"enabled": enabled}, "自营商品上下架")
		ok(w, nil)

	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}

// ===== 自营卡密 =====

// handleAdminSelfStocks 分页查看自营卡密。
// 不带 goodsId 时返回全部自营商品的卡密。
func (a *API) handleAdminSelfStocks(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	q := r.URL.Query()
	goodsID, _ := strconv.Atoi(q.Get("goodsId"))
	status := strings.TrimSpace(q.Get("status"))
	keyword := strings.TrimSpace(q.Get("keyword"))
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	items, total := store.ListSelfStockPaged(goodsID, status, keyword, page, pageSize)
	ok(w, map[string]any{"items": items, "total": total, "page": page, "pageSize": pageSize})
}

// handleAdminSelfStocksPost 导入 / 删除自营卡密。
func (a *API) handleAdminSelfStocksPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		Action  string `json:"action"`
		GoodsID int    `json:"goodsId"`
		Content string `json:"content"`
		StockID int    `json:"stockId"`
		IDs     []int  `json:"ids"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}

	switch body.Action {
	case "import":
		if body.GoodsID == 0 {
			fail(w, http.StatusBadRequest, "请选择自营商品")
			return
		}
		if strings.TrimSpace(body.Content) == "" {
			fail(w, http.StatusBadRequest, "卡密内容不能为空")
			return
		}
		if p := store.GetProduct(body.GoodsID); p == nil {
			fail(w, http.StatusBadRequest, "商品不存在")
			return
		}
		lines := strings.Split(strings.ReplaceAll(body.Content, "\r\n", "\n"), "\n")
		added, skipped := store.ImportSelfStock(body.GoodsID, lines, "admin", "")
		store.WriteAudit(&admin.UID, "self_stock_import", "product", itoa(body.GoodsID),
			nil, map[string]any{"added": added, "skipped": skipped}, "导入自营卡密")
		ok(w, map[string]any{
			"added":      added,
			"skipped":    skipped,
			"stockCount": store.SelfStockCount(body.GoodsID),
		})

	case "delete":
		if body.GoodsID == 0 || body.StockID == 0 {
			fail(w, http.StatusBadRequest, "缺少商品编号或卡密 ID")
			return
		}
		if !store.DeleteSelfStock(body.GoodsID, body.StockID) {
			fail(w, http.StatusBadRequest, "卡密不存在或已售出，无法删除")
			return
		}
		store.WriteAudit(&admin.UID, "self_stock_delete", "self_stock", itoa(body.StockID),
			nil, nil, "删除自营卡密")
		ok(w, map[string]any{"stockCount": store.SelfStockCount(body.GoodsID)})

	case "batchDelete":
		if body.GoodsID == 0 {
			fail(w, http.StatusBadRequest, "请选择自营商品")
			return
		}
		if len(body.IDs) == 0 {
			fail(w, http.StatusBadRequest, "未选择卡密")
			return
		}
		n := 0
		for _, id := range body.IDs {
			if store.DeleteSelfStock(body.GoodsID, id) {
				n++
			}
		}
		store.WriteAudit(&admin.UID, "self_stock_delete", "self_stock", joinIDs(body.IDs),
			nil, map[string]any{"deleted": n}, "批量删除自营卡密")
		ok(w, map[string]any{"deleted": n, "stockCount": store.SelfStockCount(body.GoodsID)})

	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}
