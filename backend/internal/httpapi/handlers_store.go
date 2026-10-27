package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"hstore/backend/internal/db"
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
	ok(w, paged(items, total, page, pageSize))
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
	for _, id := range ids {
		stock := -1
		if p := store.GetProduct(id); p != nil {
			stock = p.StockCount
		}
		items = append(items, upstream.StockItem{GoodsID: id, StockCount: stock})
	}
	upstream.RefreshStockForProducts(items, 20)

	stocks := map[string]int{}
	for _, it := range items {
		if p := store.GetProduct(it.GoodsID); p != nil {
			stocks[itoa(it.GoodsID)] = p.StockCount
		}
	}
	ok(w, map[string]any{"stocks": stocks})
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
	if !upstream.IsConfigured() {
		fail(w, http.StatusBadRequest, "未配置 App-Id")
		return
	}
	raw, e := upstream.DetailCached(id)
	if e != nil {
		fail(w, http.StatusInternalServerError, e.Error())
		return
	}
	ok(w, map[string]any{"detail": withLocalStock(id, raw)})
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

// handleImageProxy 代理上游商品图片（带 Cookie / Referer 绕过防盗链）。
func (a *API) handleImageProxy(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	if raw == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}
	target, e := url.Parse(raw)
	if e != nil {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}
	cfg := store.GetTianqiSettings()
	base, e := url.Parse(cfg.BaseURL)
	if e != nil {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	host := target.Hostname()
	if host != base.Hostname() && !strings.HasSuffix(host, "."+base.Hostname()) {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	target.Scheme = "https"

	fetch := func(cookie string) (*http.Response, string) {
		req, e := http.NewRequest(http.MethodGet, target.String(), nil)
		if e != nil {
			return nil, ""
		}
		req.Header.Set("Referer", cfg.BaseURL+"/")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		res, e := (&http.Client{}).Do(req)
		if e != nil {
			return nil, ""
		}
		return res, res.Header.Get("Content-Type")
	}

	cookie := store.GetCookie()
	if cookie == "" {
		if r := upstream.RefreshWafCookieOnce(cfg.BaseURL); r.OK {
			cookie = r.Cookie
			store.SaveCookie(cookie)
		}
	}
	res, ct := fetch(cookie)
	if res == nil || res.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		if r := upstream.RefreshWafCookieOnce(cfg.BaseURL); r.OK {
			store.SaveCookie(r.Cookie)
			if res != nil {
				res.Body.Close()
			}
			res, ct = fetch(r.Cookie)
		}
	}
	if res == nil || res.StatusCode != http.StatusOK || !strings.HasPrefix(ct, "image/") {
		if res != nil {
			res.Body.Close()
		}
		http.Error(w, "upstream blocked", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	_, _ = io.Copy(w, res.Body)
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
	ok(w, map[string]any{
		"boards":        store.StoreBoards(),
		"productCount":  store.CountStoreProducts(),
		"configured":    upstream.IsConfigured(),
		"lastSync":      lastSync,
		"discount":      discount,
		"announcements": normal,
		"popup":         popup,
		"user":          user,
	})
}
