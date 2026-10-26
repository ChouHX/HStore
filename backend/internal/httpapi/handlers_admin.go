package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
	"hstore/backend/internal/service"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

func (a *API) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	d := db.Must()
	var pTotal, pEnabled, pDisabled int
	_ = d.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN enabled = 1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN enabled = 0 THEN 1 ELSE 0 END),0) FROM products`).Scan(&pTotal, &pEnabled, &pDisabled)
	var uTotal, uActive, uFrozen int
	var uBalance float64
	_ = d.QueryRow(`SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN status='active' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='frozen' THEN 1 ELSE 0 END),0),
		COALESCE(ROUND(SUM(balance),2),0) FROM users WHERE role <> 'guest'`).Scan(&uTotal, &uActive, &uFrozen, &uBalance)
	var oTotal int
	var oSale, oCost float64
	_ = d.QueryRow(`SELECT COUNT(*),
		COALESCE(ROUND(SUM(CASE WHEN source IN ('user','guest') THEN sale_money ELSE 0 END),2),0),
		COALESCE(ROUND(SUM(CASE WHEN source IN ('user','guest') THEN cost_money ELSE 0 END),2),0) FROM orders`).Scan(&oTotal, &oSale, &oCost)
	var batches, codes, used int
	_ = d.QueryRow(`SELECT (SELECT COUNT(*) FROM redeem_batches),
		(SELECT COUNT(*) FROM redeem_codes),
		(SELECT COALESCE(SUM(CASE WHEN status='used' THEN 1 ELSE 0 END),0) FROM redeem_codes)`).Scan(&batches, &codes, &used)

	type platRow struct {
		Platform string `json:"platform"`
		C        int    `json:"c"`
	}
	byPlatform := []platRow{}
	if rows, e := d.Query("SELECT platform, COUNT(*) FROM products GROUP BY platform"); e == nil {
		for rows.Next() {
			var p platRow
			if rows.Scan(&p.Platform, &p.C) == nil {
				byPlatform = append(byPlatform, p)
			}
		}
		rows.Close()
	}

	type recentRecharge struct {
		Key          string  `json:"key"`
		ID           int     `json:"id"`
		OrderNo      string  `json:"orderNo"`
		Channel      string  `json:"channel"`
		ChannelLabel string  `json:"channelLabel"`
		Username     string  `json:"username"`
		AmountCny    float64 `json:"amountCny"`
		Status       string  `json:"status"`
		CreatedAt    int64   `json:"createdAt"`
		PaidAt       *int64  `json:"paidAt"`
	}
	store.ExpireAlipayOrders()
	store.ExpireCryptoOrders()
	recent := make([]recentRecharge, 0, 20)
	for _, o := range store.ListAlipayOrders(0, 30) {
		if o.Status != "paid" && o.Status != "waiting" {
			continue
		}
		amount := o.RequestedCny
		if o.Credited != nil {
			amount = *o.Credited
		}
		recent = append(recent, recentRecharge{
			Key: "alipay-" + itoa(o.ID), ID: o.ID, OrderNo: o.OrderNo,
			Channel: "alipay", ChannelLabel: "支付宝", Username: displayRechargeUsername(o.Username),
			AmountCny: amount, Status: o.Status, CreatedAt: o.CreatedAt, PaidAt: o.PaidAt,
		})
	}
	for _, o := range store.ListCryptoOrders(0, 30) {
		if o.Status != "paid" && o.Status != "waiting" {
			continue
		}
		amount := o.CreditCny
		if o.Credited != nil {
			amount = *o.Credited
		}
		username := ""
		if u := store.GetUser(o.UID); u != nil {
			username = u.Username
		}
		recent = append(recent, recentRecharge{
			Key: "usdt-" + itoa(o.ID), ID: o.ID, OrderNo: "USDT-" + itoa(o.ID),
			Channel: "usdt", ChannelLabel: "USDT", Username: displayRechargeUsername(username),
			AmountCny: amount, Status: o.Status, CreatedAt: o.CreatedAt, PaidAt: o.PaidAt,
		})
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].CreatedAt > recent[j].CreatedAt })
	if len(recent) > 8 {
		recent = recent[:8]
	}

	ok(w, map[string]any{
		"products":        map[string]any{"total": pTotal, "enabled": pEnabled, "disabled": pDisabled},
		"users":           map[string]any{"total": uTotal, "active": uActive, "frozen": uFrozen, "balance": uBalance},
		"orders":          map[string]any{"total": oTotal, "sale": oSale, "cost": oCost},
		"redeem":          map[string]any{"batches": batches, "codes": codes, "used": used},
		"byPlatform":      byPlatform,
		"recentRecharges": recent,
	})
}

func adminProductFilter(r *http.Request) store.ProductFilter {
	return store.ProductFilter{
		Keyword:     r.URL.Query().Get("keyword"),
		Platform:    r.URL.Query().Get("platform"),
		CategoryID:  queryInt(r, "categoryId"),
		RootIDs:     queryIntList(r, "rootIds"),
		SubIDs:      queryIntList(r, "subIds"),
		EnabledList: queryIntList(r, "enabledList"),
		Enabled:     r.URL.Query().Get("enabled"),
	}
}

func (a *API) handleAdminProducts(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	page, pageSize, offset := parsePage(r, 15)
	f := adminProductFilter(r)
	if r.URL.Query().Get("idsOnly") == "1" {
		ok(w, map[string]any{"ids": store.ListProductIDs(f)})
		return
	}
	f.Limit, f.Offset = pageSize, offset
	items := store.ListProductsAdmin(f)
	total := store.CountProductsAdmin(f)
	ok(w, paged(items, total, page, pageSize))
}

func (a *API) handleAdminProductsPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		Action  string   `json:"action"`
		IDs     []int    `json:"ids"`
		GoodsID int      `json:"goodsId"`
		Enabled *bool    `json:"enabled"`
		Price   *float64 `json:"price"`
		Locked  bool     `json:"locked"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	switch body.Action {
	case "toggle":
		if len(body.IDs) == 0 {
			fail(w, http.StatusBadRequest, "未选择商品")
			return
		}
		enabled := body.Enabled == nil || *body.Enabled
		store.SetProductsEnabled(body.IDs, enabled)
		action, remark := "product_off", "批量下架"
		if enabled {
			action, remark = "product_on", "批量上架"
		}
		store.WriteAudit(&admin.UID, action, "product", joinIDs(body.IDs),
			nil, map[string]any{"enabled": enabled, "count": len(body.IDs)}, remark)
		ok(w, map[string]any{"count": len(body.IDs)})
	case "price":
		if body.GoodsID == 0 {
			fail(w, http.StatusBadRequest, "缺少商品编号")
			return
		}
		store.SetProductManualPrice(body.GoodsID, body.Price, body.Locked)
		remark := "解除锁价"
		if body.Locked {
			remark = "手动锁价"
		}
		store.WriteAudit(&admin.UID, "product_price", "product", itoa(body.GoodsID),
			nil, map[string]any{"price": body.Price, "locked": body.Locked}, remark)
		ok(w, nil)
	case "delete":
		if len(body.IDs) == 0 {
			fail(w, http.StatusBadRequest, "未选择商品")
			return
		}
		store.DeleteProducts(body.IDs)
		store.WriteAudit(&admin.UID, "product_delete", "product", joinIDs(body.IDs),
			nil, map[string]any{"count": len(body.IDs)}, "删除本地商品")
		ok(w, map[string]any{"count": len(body.IDs)})
	case "recompute":
		n := store.RecomputeAllPrices()
		store.WriteAudit(&admin.UID, "price_recompute", "", "", nil, map[string]any{"count": n}, "重算全部售价")
		ok(w, map[string]any{"count": n})
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}

func displayRechargeUsername(name string) string {
	if strings.HasPrefix(name, "guest_") {
		return "游客"
	}
	return name
}

func joinIDs(ids []int) string {
	parts := make([]string, len(ids))
	for i, v := range ids {
		parts[i] = itoa(v)
	}
	return strings.Join(parts, ",")
}

func (a *API) handleAdminProductFilters(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	opt := store.GetProductFilterOptions()
	ok(w, map[string]any{"roots": opt.Roots, "subs": opt.Subs, "status": opt.Status})
}

func (a *API) handleAdminProductInfo(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	items := store.ListCustomizedProducts(r.URL.Query().Get("keyword"), 200)
	ok(w, map[string]any{"items": items})
}

func (a *API) handleAdminProductInfoPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		Action  string `json:"action"`
		GoodsID int    `json:"goodsId"`
		Name    string `json:"name"`
		Desc    string `json:"desc"`
		Img     string `json:"img"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	if body.GoodsID == 0 {
		fail(w, http.StatusBadRequest, "请输入商品编号")
		return
	}
	if body.Action == "clear" {
		if !store.SetProductCustomInfo(body.GoodsID, "", "", "", true) {
			fail(w, http.StatusNotFound, "商品不存在")
			return
		}
		store.WriteAudit(&admin.UID, "product_info_clear", "product", itoa(body.GoodsID), nil, nil, "清除商品自定义信息")
		ok(w, nil)
		return
	}
	if !store.SetProductCustomInfo(body.GoodsID, strings.TrimSpace(body.Name), strings.TrimSpace(body.Desc), strings.TrimSpace(body.Img), false) {
		fail(w, http.StatusNotFound, "商品不存在")
		return
	}
	store.WriteAudit(&admin.UID, "product_info_save", "product", itoa(body.GoodsID), nil,
		map[string]any{"name": body.Name, "desc": body.Desc, "img": body.Img}, "设置商品自定义信息")
	ok(w, nil)
}

// ===== 分类同步 =====

func keepOnly(nodes []model.CategoryNode, keep map[int]bool) []model.CategoryNode {
	out := []model.CategoryNode{}
	for _, n := range nodes {
		if !keep[n.ID] {
			continue
		}
		children := keepOnly(n.Children, keep)
		n.Children = children
		out = append(out, n)
	}
	return out
}

func (a *API) handleAdminCatalogTree(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	if upstream.IsConfigured() {
		total, roots, ids, e := upstream.PullRemoteCategories()
		if e == nil {
			keep := map[int]bool{}
			for _, id := range ids {
				keep[id] = true
			}
			ok(w, map[string]any{
				"source": "remote",
				"synced": map[string]any{"total": total, "roots": roots},
				"tree":   keepOnly(store.CategoryTree(), keep),
			})
			return
		}
		ok(w, map[string]any{
			"source": "cache", "message": "上游分类拉取失败，已展示本地缓存：" + e.Error(),
			"tree": store.CategoryTree(),
		})
		return
	}
	ok(w, map[string]any{
		"source": "local", "message": "未配置上游客户编号 / API 密钥，仅展示本地分类",
		"tree": store.CategoryTree(),
	})
}

func (a *API) handleAdminCatalogProducts(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	dirID := queryInt(r, "dirId")
	page := queryInt(r, "page")
	if page < 1 {
		page = 1
	}
	if dirID == 0 {
		fail(w, http.StatusBadRequest, "请选择分类")
		return
	}
	data, e := upstream.GetList(page, itoa(dirID))
	if e != nil {
		if upstream.IsEmptyDataError(e) {
			ok(w, map[string]any{"items": []any{}, "page": page, "allPage": 1, "allCount": 0})
			return
		}
		fail(w, http.StatusInternalServerError, e.Error())
		return
	}
	ids := make([]int, 0, len(data.Items))
	for _, it := range data.Items {
		ids = append(ids, it.GoodsID)
	}
	imported := store.ImportedGoodsSet(ids)
	stocks := upstream.FetchStockCounts(ids, 20)
	type item struct {
		model.RemoteProduct
		Imported bool `json:"imported"`
	}
	items := make([]item, 0, len(data.Items))
	for _, it := range data.Items {
		stock := -1
		if v, ok := stocks[it.GoodsID]; ok {
			stock = v
		} else if local := store.GetProduct(it.GoodsID); local != nil {
			stock = local.StockCount
		}
		it.StockCount = stock
		items = append(items, item{RemoteProduct: it, Imported: imported[it.GoodsID]})
	}
	allCount := data.AllCount
	if allCount == 0 {
		allCount = len(items)
	}
	allPage := data.AllPage
	if allPage < 1 {
		allPage = 1
	}
	ok(w, map[string]any{"items": items, "page": page, "allPage": allPage, "allCount": allCount})
}

func (a *API) handleAdminCatalogImport(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		DirID int                   `json:"dirId"`
		Items []model.RemoteProduct `json:"items"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	if body.DirID == 0 {
		fail(w, http.StatusBadRequest, "缺少分类")
		return
	}
	if len(body.Items) == 0 {
		fail(w, http.StatusBadRequest, "未选择商品")
		return
	}
	platform := store.PlatformOfDir(body.DirID)
	count := store.ImportProducts(body.Items, body.DirID, platform)
	ids := make([]int, 0, len(body.Items))
	for _, it := range body.Items {
		ids = append(ids, it.GoodsID)
	}
	upstream.EnrichImportedStock(ids)
	store.WriteAudit(&admin.UID, "catalog_import", "category", itoa(body.DirID), nil,
		map[string]any{"count": count},
		"从「"+store.DirRootName(body.DirID)+"」拉取上架 "+itoa(count)+" 个商品")
	ok(w, map[string]any{"count": count})
}

// ===== 定价 =====

func (a *API) handleAdminPricing(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	ok(w, map[string]any{"rules": store.ListRules()})
}

func (a *API) handleAdminPricingPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body map[string]any
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	action, _ := body["action"].(string)
	id, _ := store.ToInt(body["id"])
	switch action {
	case "create":
		name, _ := body["name"].(string)
		if name == "" {
			fail(w, http.StatusBadRequest, "请填写规则名称")
			return
		}
		in := store.RuleInput{Name: name, ScopeID: str(body["scopeId"]), FormulaType: str(body["formulaType"]), Rounding: str(body["rounding"])}
		if in.Scope = str(body["scope"]); in.Scope == "" {
			in.Scope = "global"
		}
		in.Amount, _ = store.ToFloat(body["amount"])
		in.Percent, _ = store.ToFloat(body["percent"])
		in.Multiplier, _ = store.ToFloat(body["multiplier"])
		in.Fixed, _ = store.ToFloat(body["fixed"])
		in.Priority, _ = store.ToInt(body["priority"])
		in.Enabled, _ = store.ToInt(body["enabled"])
		rule := store.CreateRule(in)
		store.RecomputeAllPrices()
		store.WriteAudit(&admin.UID, "pricing_create", "pricing_rule", itoa(rule.ID), nil, rule, "新建定价规则")
		ok(w, map[string]any{"rule": rule})
	case "update":
		if id == 0 {
			fail(w, http.StatusBadRequest, "缺少规则 ID")
			return
		}
		before := store.GetRule(id)
		rule := store.UpdateRule(id, body)
		store.RecomputeAllPrices()
		store.WriteAudit(&admin.UID, "pricing_update", "pricing_rule", itoa(id), before, rule, "修改定价规则")
		ok(w, map[string]any{"rule": rule})
	case "delete":
		if id == 0 {
			fail(w, http.StatusBadRequest, "缺少规则 ID")
			return
		}
		before := store.GetRule(id)
		store.DeleteRule(id)
		store.RecomputeAllPrices()
		store.WriteAudit(&admin.UID, "pricing_delete", "pricing_rule", itoa(id), before, nil, "删除定价规则")
		ok(w, nil)
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ===== 等级 =====

func (a *API) handleAdminLevels(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	ok(w, map[string]any{"levels": store.ListLevels()})
}

func (a *API) handleAdminLevelsPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body map[string]any
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	action := str(body["action"])
	id, _ := store.ToInt(body["id"])
	switch action {
	case "create":
		name := str(body["name"])
		if name == "" {
			fail(w, http.StatusBadRequest, "请填写等级名称")
			return
		}
		threshold, _ := store.ToFloat(body["threshold"])
		discount := 1.0
		if v, ok := store.ToFloat(body["discount"]); ok {
			discount = v
		}
		sort, _ := store.ToInt(body["sort"])
		level := store.CreateLevel(name, threshold, discount, sort)
		store.RecomputeAllLevels()
		store.WriteAudit(&admin.UID, "level_create", "level", itoa(level.ID), nil, level, "新增会员等级")
		ok(w, map[string]any{"level": level})
	case "update":
		if id == 0 {
			fail(w, http.StatusBadRequest, "缺少等级 ID")
			return
		}
		var before *model.Level
		for _, l := range store.ListLevels() {
			if l.ID == id {
				c := l
				before = &c
			}
		}
		store.UpdateLevel(id, body)
		store.RecomputeAllLevels()
		var after *model.Level
		for _, l := range store.ListLevels() {
			if l.ID == id {
				c := l
				after = &c
			}
		}
		store.WriteAudit(&admin.UID, "level_update", "level", itoa(id), before, after, "修改会员等级")
		ok(w, map[string]any{"level": after})
	case "delete":
		if id == 0 {
			fail(w, http.StatusBadRequest, "缺少等级 ID")
			return
		}
		store.DeleteLevel(id)
		store.WriteAudit(&admin.UID, "level_delete", "level", itoa(id), nil, nil, "删除会员等级")
		ok(w, nil)
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}

// ===== 用户 / 账本 / 日志 =====

func (a *API) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	page, pageSize, offset := parsePage(r, 15)
	uid := queryInt(r, "uid")
	if r.URL.Query().Get("ledger") == "1" {
		items := store.ListLedger(uid, pageSize, offset)
		total := store.CountLedger(uid)
		ok(w, paged(items, total, page, pageSize))
		return
	}
	f := store.UserFilter{
		Keyword: r.URL.Query().Get("keyword"),
		Status:  r.URL.Query().Get("status"),
		LevelID: queryInt(r, "levelId"),
		Role:    r.URL.Query().Get("role"),
	}
	items := store.ListUsers(f, pageSize, offset)
	total := store.CountUsers(f)
	ok(w, paged(items, total, page, pageSize))
}

func (a *API) handleAdminUsersPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body map[string]any
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	uid, _ := store.ToInt(body["uid"])
	if uid == 0 {
		fail(w, http.StatusBadRequest, "缺少用户 UID")
		return
	}
	switch str(body["action"]) {
	case "update":
		user := store.UpdateUser(uid, body)
		store.WriteAudit(&admin.UID, "user_update", "user", itoa(uid), nil, user, "编辑用户资料")
		ok(w, map[string]any{"user": user})
	case "balance":
		amount, _ := store.ToFloat(body["amount"])
		if amount == 0 {
			fail(w, http.StatusBadRequest, "调整金额不能为 0")
			return
		}
		ok2, msg, before, after := store.AdjustBalance(uid, amount, admin.UID, "")
		if !ok2 {
			fail(w, http.StatusBadRequest, msg)
			return
		}
		ok(w, map[string]any{"before": before, "after": after})
	case "level":
		var levelID *int
		if v, ok3 := store.ToInt(body["levelId"]); ok3 && v > 0 {
			levelID = &v
		}
		locked, _ := body["locked"].(bool)
		user := store.SetUserLevel(uid, levelID, locked, admin.UID)
		ok(w, map[string]any{"user": user})
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}

func (a *API) handleAdminLedger(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	page, pageSize, offset := parsePage(r, 15)
	uid := queryInt(r, "uid")
	items := store.ListLedger(uid, pageSize, offset)
	total := store.CountLedger(uid)
	ok(w, paged(items, total, page, pageSize))
}

func (a *API) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	page, pageSize, offset := parsePage(r, 20)
	items := store.ListAudit(pageSize, offset)
	total := store.CountAudit()
	ok(w, paged(items, total, page, pageSize))
}

// ===== 公告 =====

func (a *API) handleAdminAnnouncements(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	ok(w, map[string]any{"items": store.ListAnnouncements()})
}

func (a *API) handleAdminAnnouncementsPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		Action  string `json:"action"`
		ID      int    `json:"id"`
		Type    string `json:"type"`
		Title   string `json:"title"`
		Content string `json:"content"`
		Enabled *bool  `json:"enabled"`
		Sort    int    `json:"sort"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	switch body.Action {
	case "save":
		if strings.TrimSpace(body.Title) == "" {
			fail(w, http.StatusBadRequest, "请填写公告标题")
			return
		}
		typ := body.Type
		if typ == "" {
			typ = "normal"
		}
		enabled := body.Enabled == nil || *body.Enabled
		item := store.SaveAnnouncement(body.ID, typ, strings.TrimSpace(body.Title), body.Content, enabled, body.Sort)
		action := "announcement_create"
		if body.ID > 0 {
			action = "announcement_update"
		}
		store.WriteAudit(&admin.UID, action, "announcement", itoa(item.ID), nil,
			map[string]any{"type": item.Type, "title": item.Title, "enabled": item.Enabled}, "保存公告")
		ok(w, map[string]any{"item": item, "items": store.ListAnnouncements()})
	case "delete":
		if body.ID == 0 {
			fail(w, http.StatusBadRequest, "缺少 ID")
			return
		}
		store.DeleteAnnouncement(body.ID)
		store.WriteAudit(&admin.UID, "announcement_delete", "", itoa(body.ID), nil, nil, "删除公告")
		ok(w, map[string]any{"items": store.ListAnnouncements()})
	case "toggle":
		if body.ID == 0 {
			fail(w, http.StatusBadRequest, "缺少 ID")
			return
		}
		enabled := body.Enabled != nil && *body.Enabled
		store.ToggleAnnouncement(body.ID, enabled)
		ok(w, map[string]any{"items": store.ListAnnouncements()})
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}

// ===== 兑换码 =====

func (a *API) handleAdminRedeem(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "product" && kind != "balance" {
		kind = ""
	}
	f := store.CodeFilter{
		Status:  q.Get("status"),
		Keyword: q.Get("keyword"),
		UsedBy:  queryInt(r, "usedBy"),
		Kind:    kind,
	}
	if q.Get("export") == "1" {
		csv := store.ExportCodesCSV(f)
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="redeem-codes.csv"`)
		_, _ = w.Write([]byte("\uFEFF" + csv))
		return
	}
	page, pageSize, offset := parsePage(r, 15)
	items := store.ListCodes(f, pageSize, offset)
	total := store.CountCodes(f)
	ok(w, paged(items, total, page, pageSize))
}

func (a *API) handleAdminRedeemPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		Action     string  `json:"action"`
		Kind       string  `json:"kind"`
		FaceValue  float64 `json:"faceValue"`
		GoodsID    *int    `json:"goodsId"`
		GoodsCount int     `json:"goodsCount"`
		Count      int     `json:"count"`
		StartAt    *int64  `json:"startAt"`
		EndAt      *int64  `json:"endAt"`
		CodeID     int     `json:"codeId"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	switch body.Action {
	case "create":
		if body.Count <= 0 {
			fail(w, http.StatusBadRequest, "生成数量必须大于 0")
			return
		}
		if body.StartAt != nil && body.EndAt != nil && *body.StartAt > *body.EndAt {
			fail(w, http.StatusBadRequest, "开始时间不能晚于结束时间")
			return
		}
		if body.Kind == "product" {
			if body.GoodsID == nil || *body.GoodsID == 0 {
				fail(w, http.StatusBadRequest, "请选择兑换码关联的商品")
				return
			}
			product := store.GetProduct(*body.GoodsID)
			if product == nil {
				fail(w, http.StatusBadRequest, "商品不存在，请先同步上游商品")
				return
			}
			goodsCount := body.GoodsCount
			if goodsCount < 1 {
				goodsCount = 1
			}
			n := store.CreateCodes(store.CreateCodesInput{
				GoodsID: body.GoodsID, GoodsCount: goodsCount, Count: body.Count,
				StartAt: body.StartAt, EndAt: body.EndAt,
			})
			store.WriteAudit(&admin.UID, "redeem_create", "redeem_code", itoa(*body.GoodsID), nil,
				map[string]any{"goodsId": *body.GoodsID, "goodsCount": goodsCount, "count": n},
				"生成商品兑换码 "+itoa(n)+" 张（"+product.Name+" × "+itoa(goodsCount)+"）")
			ok(w, map[string]any{"count": n})
			return
		}
		if body.FaceValue <= 0 {
			fail(w, http.StatusBadRequest, "面值必须大于 0")
			return
		}
		n := store.CreateCodes(store.CreateCodesInput{
			FaceValue: body.FaceValue, Count: body.Count, StartAt: body.StartAt, EndAt: body.EndAt,
		})
		store.WriteAudit(&admin.UID, "redeem_create", "redeem_code", "", nil,
			map[string]any{"faceValue": body.FaceValue, "count": n},
			"生成余额兑换码 "+itoa(n)+" 张，面值 "+store.TrimFloat(body.FaceValue))
		ok(w, map[string]any{"count": n})
	case "voidCode":
		if body.CodeID == 0 {
			fail(w, http.StatusBadRequest, "缺少券码 ID")
			return
		}
		store.VoidCode(body.CodeID)
		store.WriteAudit(&admin.UID, "redeem_code_void", "redeem_code", itoa(body.CodeID), nil, nil, "单码作废")
		ok(w, nil)
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}

// ===== 设置 =====

func (a *API) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	s := store.GetTianqiSettings()
	cookie := store.GetCookie()
	preview := ""
	if cookie != "" {
		preview = maskCookie(cookie)
	}
	ok(w, map[string]any{
		"baseUrl": s.BaseURL, "appId": s.AppID,
		"appSecretMasked": store.MaskSecret(s.AppSecret), "hasSecret": s.AppSecret != "",
		"hasCookie": cookie != "", "cookieAt": store.GetCookieAt(), "cookiePreview": preview,
		"redeemPurchaseUrl": store.GetRedeemPurchaseURL(),
	})
}

func maskCookie(cookie string) string {
	if i := strings.Index(cookie, "acw_sc__v2="); i >= 0 {
		rest := cookie[i+len("acw_sc__v2="):]
		end := len(rest)
		if j := strings.Index(rest, ";"); j >= 0 {
			end = j
		}
		return cookie[:i] + "acw_sc__v2=****" + rest[end:]
	}
	return cookie
}

func (a *API) handleAdminSettingsPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		Action            string `json:"action"`
		BaseURL           string `json:"baseUrl"`
		AppID             string `json:"appId"`
		AppSecret         string `json:"appSecret"`
		RedeemPurchaseURL string `json:"redeemPurchaseUrl"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	switch body.Action {
	case "clearCookie":
		store.ClearCookie()
		store.WriteAudit(&admin.UID, "settings_cookie_clear", "", "", nil, nil, "清除 WAF Cookie")
		ok(w, nil)
		return
	case "saveSite":
		u := strings.TrimSpace(body.RedeemPurchaseURL)
		if u != "" {
			parsed, e := url.Parse(u)
			if e != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				fail(w, http.StatusBadRequest, "链接需为 http/https 地址")
				return
			}
		}
		store.SaveRedeemPurchaseURL(u)
		store.WriteAudit(&admin.UID, "settings_site_save", "", "", nil, map[string]any{"redeemPurchaseUrl": u}, "保存商城设置")
		ok(w, map[string]any{"redeemPurchaseUrl": u})
		return
	}

	baseURL := strings.TrimSpace(body.BaseURL)
	appID := strings.TrimSpace(body.AppID)
	if baseURL == "" || appID == "" {
		fail(w, http.StatusBadRequest, "基础地址与客户编号不能为空")
		return
	}
	if _, e := url.Parse(baseURL); e != nil {
		fail(w, http.StatusBadRequest, "基础地址格式不正确")
		return
	}
	store.SaveTianqiSettings(baseURL, appID, body.AppSecret)
	store.WriteAudit(&admin.UID, "settings_save", "", "", nil,
		map[string]any{"baseUrl": baseURL, "appId": appID, "appSecretChanged": body.AppSecret != ""},
		"保存开放平台配置")
	ok(w, nil)
}

func (a *API) handleAdminCookieRefresh(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	baseURL := store.GetTianqiSettings().BaseURL
	res := upstream.RefreshWafCookieOnce(baseURL)
	if !res.OK {
		fail(w, http.StatusInternalServerError, res.Message)
		return
	}
	store.SaveCookie(res.Cookie)
	store.WriteAudit(&admin.UID, "settings_cookie_refresh", "", "", nil, nil, "自动获取 WAF Cookie")
	ok(w, map[string]any{"cookieAt": ts(), "cookiePreview": maskCookie(res.Cookie)})
}

func (a *API) handleAdminSettingsTest(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	if !upstream.IsConfigured() {
		fail(w, http.StatusBadRequest, "请先填写客户编号与 API 密钥")
		return
	}
	balance, e := upstream.GetAccount()
	if e != nil {
		fail(w, http.StatusInternalServerError, e.Error())
		return
	}
	ok(w, map[string]any{"balance": balance})
}

// ===== 同步 =====

func (a *API) handleSync(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	if r.URL.Query().Get("demo") == "1" {
		n := service.LoadDemoData()
		ok(w, map[string]any{"source": "demo", "message": "已载入演示数据：" + itoa(n) + " 个商品"})
		return
	}
	var body struct {
		Scope string `json:"scope"`
		IDs   []int  `json:"ids"`
	}
	_ = readJSON(r, &body)

	if body.Scope == "current" {
		if len(body.IDs) == 0 {
			fail(w, http.StatusBadRequest, "未指定要同步的商品")
			return
		}
		res := upstream.RefreshProducts(body.IDs)
		if res.Updated > 0 {
			store.RecomputeAllPrices()
		}
		parts := "已更新 " + itoa(res.Updated) + " 个商品的上游信息"
		if res.Failed > 0 {
			parts += "，" + itoa(res.Failed) + " 个拉取失败"
		}
		if len(res.Missing) > 0 {
			parts += "，" + itoa(len(res.Missing)) + " 个本地已删除"
		}
		ok(w, map[string]any{
			"source": "remote", "scope": "current", "message": parts,
			"total": res.Total, "updated": res.Updated, "failed": res.Failed, "missing": res.Missing,
		})
		return
	}

	res, e := upstream.RunSync()
	if e != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "source": "remote", "message": e.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// 保留：供 JSON 直出时使用
var _ = json.Marshal
var _ = strconv.Itoa
