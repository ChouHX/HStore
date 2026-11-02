package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"hstore/backend/internal/db"
	"hstore/backend/internal/imagecache"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

func (a *API) handleStoreProducts(w http.ResponseWriter, r *http.Request) {
	rootID := queryInt(r, "rootId")
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	if rootID == 0 && keyword == "" {
		fail(w, http.StatusBadRequest, "缺少板块或搜索词")
		return
	}
	page, pageSize, _ := parsePage(r, 20)
	if pageSize > 60 {
		pageSize = 60
	}
	q := store.StoreProductQuery{
		RootID:   rootID,
		DirID:    queryInt(r, "dirId"),
		Keyword:  keyword,
		Sort:     r.URL.Query().Get("sort"),
		Page:     page,
		PageSize: pageSize,
	}
	items, total := store.ListStoreProducts(q)
	sanitizeSelfProducts(items)
	localizeProductImages(items)
	ok(w, paged(items, total, page, pageSize))
}

// ===== 自营商品的库存展示口径 =====
//
// 自营商品的真实库存条数属于内部信息，前台一律不展示数字，只给语义化状态：
//   - 本地可用库存 > 0                 → 有货 / 可下单（showStock=true）
//   - 本地为 0 且关联了上游兜底商品     → 有货 / 可下单（下单时走上游采购）
//   - 本地为 0 且没有兜底商品           → 缺货，不可下单（showStock=false）

// isSelfProduct 判断商品是否走自营库存发货。
func isSelfProduct(p *model.Product) bool {
	return p != nil && p.Fulfillment == "self"
}

// selfStockAvailable 返回自营商品当前是否可售（本地有货，或配置了上游兜底）。
func selfStockAvailable(goodsID int) bool {
	avail := store.SelfStockCount(goodsID)
	if avail > 0 {
		return true
	}
	p := store.GetProduct(goodsID)
	if p == nil || p.Fulfillment != "self" {
		return avail > 0
	}
	return p.FallbackGoodsID > 0
}

// maskSelfProductStock 把自营商品的库存数字换成「有货 / 缺货」。
//
// stockCount>0 表示可售；0 表示缺货。上游商品的 stockCount 语义不变（-1 未知、0 缺货）。
func maskSelfProductStock(p *model.Product) {
	if !isSelfProduct(p) {
		return
	}
	if selfStockAvailable(p.GoodsID) {
		p.StockCount = 1
		return
	}
	p.StockCount = 0
}

// sanitizeSelfProducts 对商城列表里的自营商品做脱敏（就地修改）。
func sanitizeSelfProducts(items []model.Product) {
	for i := range items {
		maskSelfProductStock(&items[i])
	}
}

func (a *API) handleStoreStock(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("ids"))
	var ids []int
	for _, part := range strings.Split(raw, ",") {
		if n, e := parseInt(strings.TrimSpace(part)); e == nil && n > 0 && len(ids) < 20 {
			ids = append(ids, n)
		}
	}
	if len(ids) == 0 {
		fail(w, http.StatusBadRequest, "缺少商品编号")
		return
	}
	items := make([]upstream.StockItem, 0, len(ids))
	// 自营商品不请求上游，单独按本地库存给出语义化状态
	selfAvail := map[string]bool{}
	for _, id := range ids {
		p := store.GetProduct(id)
		if p == nil {
			continue
		}
		if p.Fulfillment == "self" {
			selfAvail[itoa(id)] = selfStockAvailable(id)
			continue
		}
		items = append(items, upstream.StockItem{GoodsID: id, StockCount: p.StockCount})
	}
	upstream.RefreshStockForProducts(items, 20)

	stocks := map[string]int{}
	for _, it := range items {
		if p := store.GetProduct(it.GoodsID); p != nil {
			stocks[itoa(it.GoodsID)] = p.StockCount
		}
	}
	// 自营商品返回「是否可售」而非库存条数；上游商品金额不变。
	statuses := map[string]string{}
	for id, avail := range selfAvail {
		if avail {
			stocks[id] = 1
			statuses[id] = "in_stock"
			continue
		}
		stocks[id] = 0
		statuses[id] = "out_of_stock"
	}
	ok(w, map[string]any{"stocks": stocks, "stockStatus": statuses, "selfStockHidden": true})
}

// handleGoodsDetail 商品详情：直接返回本地缓存的原始 JSON。
//
// 缓存由「同步商品 / 刷新库存」写入（见 upstream.GetDetail），所以正常情况这里
// 一次上游请求都不发；只有从没同步过的新商品才会同步回源一次。
func (a *API) handleGoodsDetail(w http.ResponseWriter, r *http.Request) {
	id := queryInt(r, "id")
	if id == 0 {
		fail(w, http.StatusBadRequest, "缺少商品编号")
		return
	}
	if p := store.GetProduct(id); p != nil && p.Fulfillment == "self" {
		detail := selfProductDetail(p)
		ok(w, map[string]any{"detail": detail})
		return
	}
	if !upstream.IsConfigured() {
		fail(w, http.StatusBadRequest, "未配置 App-Id")
		return
	}
	raw, e := upstream.DetailCached(id)
	if e != nil {
		fail(w, http.StatusInternalServerError, e.Error())
		return
	}
	ok(w, map[string]any{"detail": withLocalImage(withLocalStock(id, raw))})
}

// selfProductDetail 为自营商品构造与上游一致的详情结构。
//
// 自营商品的 stockCount 是脱敏后的「可售标记」：>0 表示有货（含上游兜底），
// 0 表示缺货；真实库存条数不对外暴露。
func selfProductDetail(p *model.Product) model.GoodsDetail {
	available := selfStockAvailable(p.GoodsID)
	stockFlag := 0
	if available {
		stockFlag = 1
	}
	name := p.Name
	if p.CustomName != nil && strings.TrimSpace(*p.CustomName) != "" {
		name = strings.TrimSpace(*p.CustomName)
	}
	desc := ""
	if p.CustomDesc != nil {
		desc = *p.CustomDesc
	}
	img := p.ImgURL
	if p.CustomImg != nil && *p.CustomImg != "" {
		img = *p.CustomImg
	}
	return model.GoodsDetail{
		RemoteProduct: model.RemoteProduct{
			GoodsID: p.GoodsID, Name: name, SalesPrice: p.FinalPrice, FaceValue: p.FaceValue,
			GoodsType: p.GoodsType, WarrantyDays: p.WarrantyDays, Status: p.Status,
			ImgURL: img, StockCount: stockFlag,
		},
		MinQuantity:   1,
		MaxQuantity:   999999,
		GoodsDescribe: desc,
	}
}

// withLocalStock 用本地库存覆盖详情缓存里的旧值。
//
// 本地 stock_count 由商品列表加载时的 60 秒节流刷新维护，比 10 分钟 TTL 的
// 详情缓存更新，所以前端看到的库存以本地为准。
func withLocalStock(goodsID int, raw json.RawMessage) json.RawMessage {
	p := store.GetProduct(goodsID)
	if p == nil || p.StockCount < 0 {
		return raw
	}
	// UseNumber：避免 float64 往返把大整数或小数写成科学计数法
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if e := dec.Decode(&obj); e != nil {
		return raw
	}
	if cur, ok := obj["stockCount"].(json.Number); ok {
		if n, e := cur.Int64(); e == nil && int(n) == p.StockCount {
			return raw
		}
	}
	obj["stockCount"] = p.StockCount
	if b, e := json.Marshal(obj); e == nil {
		return b
	}
	return raw
}

// withLocalImage 把详情里的 imgUrl 换成本地缓存地址，避免泄露上游域名。
func withLocalImage(raw json.RawMessage) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if e := dec.Decode(&obj); e != nil {
		return raw
	}
	v, ok := obj["imgUrl"].(string)
	if !ok || v == "" {
		return raw
	}
	obj["imgUrl"] = imagecache.LocalURL(v)
	if b, e := json.Marshal(obj); e == nil {
		return b
	}
	return raw
}

// localizeBoards 把板块 / 子分类图标换成本地缓存地址。
func localizeBoards(boards []model.StoreBoard) {
	for i := range boards {
		boards[i].Img = imagecache.LocalURL(boards[i].Img)
		for j := range boards[i].Children {
			boards[i].Children[j].Img = imagecache.LocalURL(boards[i].Children[j].Img)
		}
	}
}

// handleCachedImage 返回本地缓存的商品图，前台不再接触上游地址。
func (a *API) handleCachedImage(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	e, err := imagecache.Open(key)
	if err != nil {
		http.Error(w, "image not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", e.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
	http.ServeFile(w, r, e.Path())
}

// localizeProductImages 把商品图地址替换成本地缓存地址。
func localizeProductImages(items []model.Product) {
	for i := range items {
		items[i].ImgURL = imagecache.LocalURL(items[i].ImgURL)
		if items[i].CustomImg != nil {
			v := imagecache.LocalURL(*items[i].CustomImg)
			items[i].CustomImg = &v
		}
	}
}

// handleStoreHome 首页一次性数据：板块树、商品数、上游配置状态、最后同步时间、
// 等级折扣、公告与当前用户。前端 SPA 只需一次请求即可渲染首页。
func (a *API) handleStoreHome(w http.ResponseWriter, r *http.Request) {
	normal, popup := store.PublicAnnouncements()
	user := sessionUser(r)
	discount := 1.0
	if user != nil {
		discount = store.GetLevelDiscount(user.LevelID)
	}
	var lastSync int64
	if v := db.GetMeta("last_sync"); v != "" {
		if n, e := strconv.ParseInt(strings.TrimSpace(v), 10, 64); e == nil {
			lastSync = n
		}
	}
	boards := store.StoreBoards()
	localizeBoards(boards)
	ok(w, map[string]any{
		"boards":        boards,
		"productCount":  store.CountStoreProducts(),
		"configured":    upstream.IsConfigured(),
		"lastSync":      lastSync,
		"discount":      discount,
		"announcements": normal,
		"popup":         popup,
		"user":          user,
	})
}
