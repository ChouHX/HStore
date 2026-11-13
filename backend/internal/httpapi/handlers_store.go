package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/imagecache"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

// ===== 商城对外字段白名单 =====
//
// products 表里的 model.Product 是后台结构，含成本价、上游平台、目录 ID、同步时间等
// 内部字段；商城列表 / 详情一律只下发下面两个白名单结构，避免成本价等核心数据泄露。

// storeProductView 是商城列表下发的商品字段。
type storeProductView struct {
	GoodsID     int     `json:"goodsId"`
	Name        string  `json:"name"`
	CustomName  *string `json:"customName,omitempty"`
	CustomDesc  *string `json:"customDesc,omitempty"`
	CustomImg   *string `json:"customImg,omitempty"`
	ImgURL      string  `json:"imgUrl"`
	FinalPrice  float64 `json:"finalPrice"`
	GoodsType   int     `json:"goodsType"`
	Status      int     `json:"status"`
	StockCount  int     `json:"stockCount"`
	Fulfillment string  `json:"fulfillment,omitempty"`
}

// storeDetailView 是商品详情下发的字段。
type storeDetailView struct {
	Fulfillment       string          `json:"fulfillment,omitempty"`
	GoodsID           int             `json:"goodsId"`
	Name              string          `json:"name"`
	ImgURL            string          `json:"imgUrl"`
	SalesPrice        float64         `json:"salesPrice"`
	FaceValue         float64         `json:"faceValue"`
	GoodsType         int             `json:"goodsType"`
	Status            int             `json:"status"`
	StockCount        int             `json:"stockCount"`
	MinQuantity       int             `json:"minQuantity"`
	MaxQuantity       int             `json:"maxQuantity"`
	WarrantyDays      int             `json:"warrantyDays,omitempty"`
	GoodsDescribe     string          `json:"goodsDescribe,omitempty"`
	GoodsDetail       string          `json:"goodsDetail,omitempty"`
	BuyNotice         string          `json:"buyNotice,omitempty"`
	RechargeTemplates json.RawMessage `json:"rechargeTemplates,omitempty"`
}

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
	views := make([]storeProductView, 0, len(items))
	for _, p := range items {
		views = append(views, storeProductViewOf(p))
	}
	ok(w, paged(views, total, page, pageSize))
}

// storeProductViewOf 把后台商品结构裁剪成商城可见字段，并把图片换成本地代理地址。
//
// 自营商品的 stockCount 由 store.scanProduct 覆盖为本地可用条数（self_stock 中
// status=available 的行数），所以这里直接下发即可，前端展示的就是真实库存。
func storeProductViewOf(p model.Product) storeProductView {
	v := storeProductView{
		GoodsID:     p.GoodsID,
		Name:        p.Name,
		CustomName:  p.CustomName,
		CustomDesc:  p.CustomDesc,
		ImgURL:      imagecache.LocalURL(p.ImgURL),
		FinalPrice:  p.FinalPrice,
		GoodsType:   p.GoodsType,
		Status:      p.Status,
		StockCount:  p.StockCount,
		Fulfillment: p.Fulfillment,
	}
	if p.CustomImg != nil {
		img := imagecache.LocalURL(*p.CustomImg)
		v.CustomImg = &img
	}
	return v
}

// ===== 商城库存刷新 =====
//
// 列表页展示的是后台定时同步的本地库存（cmd/server stockLoop，2 分钟一轮），
// 所以 /api/store/stock 直接返回库里的值，最多在后台补一次上游刷新：
// 60 秒节流 + 单飞，绝不阻塞请求。

const storeStockRefreshInterval = 60 * time.Second

var (
	storeStockMu         sync.Mutex
	storeStockRefreshing bool
	storeStockRefreshed  time.Time
)

// refreshStoreStockAsync 触发一次后台库存刷新；60 秒内最多一次，同一时间最多一个任务。
func refreshStoreStockAsync(ids []int) {
	if len(ids) == 0 {
		return
	}
	storeStockMu.Lock()
	if storeStockRefreshing || time.Since(storeStockRefreshed) < storeStockRefreshInterval {
		storeStockMu.Unlock()
		return
	}
	storeStockRefreshing = true
	storeStockRefreshed = time.Now()
	storeStockMu.Unlock()

	go func() {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("[hstore] 商城库存刷新 panic: %v", v)
			}
			storeStockMu.Lock()
			storeStockRefreshing = false
			storeStockMu.Unlock()
		}()
		counts := upstream.FetchStockCounts(ids, 20)
		for id, n := range counts {
			store.UpdateStock(id, n)
		}
	}()
}

// handleStoreStock 返回商品库存快照：全部读本地库，自营商品给本地可用条数。
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
	stocks := map[string]int{}
	upstreamIDs := make([]int, 0, len(ids))
	for _, id := range ids {
		p := store.GetProduct(id)
		if p == nil {
			continue
		}
		if p.Fulfillment == "self" {
			stocks[itoa(id)] = store.SelfStockCount(id)
			continue
		}
		stocks[itoa(id)] = p.StockCount
		upstreamIDs = append(upstreamIDs, id)
	}
	// 上游商品的库存由后台异步补刷，本次请求先用库里的值应答。
	refreshStoreStockAsync(upstreamIDs)
	ok(w, map[string]any{"stocks": stocks})
}

// handleGoodsDetail 商品详情。
//
// 上游商品的详情来自本地 detail_json 缓存（后台库存同步 / 首次访问时写入），
// 浏览路径不额外请求上游；只有从没缓存过的新商品才会同步回源一次。
// 带 fresh=1 时尝试回源拿「此刻」的库存，但同一商品 30 秒内只放行一次，
// 其余请求直接回退本地缓存（见 upstream.DetailFresh），避免购买弹窗把上游打爆。
func (a *API) handleGoodsDetail(w http.ResponseWriter, r *http.Request) {
	id := queryInt(r, "id")
	if id <= 0 {
		fail(w, http.StatusBadRequest, "缺少商品编号")
		return
	}
	if p := store.GetProduct(id); p != nil && p.Fulfillment == "self" {
		ok(w, map[string]any{"detail": selfProductDetail(p)})
		return
	}
	if !upstream.IsConfigured() {
		fail(w, http.StatusBadRequest, "商品服务暂不可用，请联系客服")
		return
	}
	var raw json.RawMessage
	var e error
	if r.URL.Query().Get("fresh") == "1" {
		raw, e = upstream.DetailFresh(id)
	} else {
		raw, e = upstream.DetailCached(id)
	}
	if e != nil {
		fail(w, http.StatusInternalServerError, customerOrderError(e.Error()))
		return
	}
	detail, e := buildStoreDetail(id, raw)
	if e != nil {
		fail(w, http.StatusInternalServerError, "解析商品详情失败")
		return
	}
	ok(w, map[string]any{"detail": detail})
}

// buildStoreDetail 把上游详情 JSON 裁剪成白名单字段。
//
// 缓存里的原始 JSON 带成本价（salesPrice）、上游目录 ID、SKU 结构等内部信息，
// 不能整包下发；库存优先用本地 stock_count（后台同步比 10 分钟 TTL 的缓存更新）。
func buildStoreDetail(goodsID int, raw json.RawMessage) (storeDetailView, error) {
	var d model.GoodsDetail
	if e := json.Unmarshal(raw, &d); e != nil {
		return storeDetailView{}, e
	}
	stock := d.StockCount
	if p := store.GetProduct(goodsID); p != nil && p.StockCount >= 0 {
		stock = p.StockCount
	}
	minQty, maxQty := d.MinQuantity, d.MaxQuantity
	if minQty < 1 {
		minQty = 1
	}
	if maxQty < 1 {
		maxQty = 999999
	}
	return storeDetailView{
		GoodsID:           goodsID,
		Name:              d.Name,
		ImgURL:            imagecache.LocalURL(d.ImgURL),
		SalesPrice:        d.SalesPrice,
		FaceValue:         d.FaceValue,
		GoodsType:         d.GoodsType,
		Status:            d.Status,
		StockCount:        stock,
		MinQuantity:       minQty,
		MaxQuantity:       maxQty,
		WarrantyDays:      d.WarrantyDays,
		GoodsDescribe:     d.GoodsDescribe,
		GoodsDetail:       d.GoodsDetail,
		BuyNotice:         d.BuyNotice,
		RechargeTemplates: d.RechargeTemplates,
	}, nil
}

// selfProductDetail 为自营商品构造与上游一致的详情结构。
//
// stockCount 是本地真实可用条数（与列表页同一口径）。本地为 0 但配置了上游兜底
// 商品时仍然可以下单，前端据列表里的 hasFallback 判断是否放开按钮。
func selfProductDetail(p *model.Product) storeDetailView {
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
	return storeDetailView{
		GoodsID:       p.GoodsID,
		Fulfillment:   "self",
		Name:          name,
		ImgURL:        imagecache.LocalURL(img),
		SalesPrice:    p.FinalPrice,
		FaceValue:     p.FaceValue,
		GoodsType:     p.GoodsType,
		Status:        p.Status,
		StockCount:    store.SelfStockCount(p.GoodsID),
		MinQuantity:   1,
		MaxQuantity:   999999,
		WarrantyDays:  p.WarrantyDays,
		GoodsDescribe: desc,
	}
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
