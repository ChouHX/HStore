package httpapi

import (
	"net/http"

	"hstore/backend/internal/db"
	"hstore/backend/internal/service"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

func cryptoSnapshot() map[string]any {
	cfg := store.GetCryptoConfig()
	bk := upstream.GetBybitConfig()
	apiKey := bk.APIKey
	if apiKey != "" {
		apiKey = store.MaskSecret(apiKey)
	}
	return map[string]any{
		"ok":           true,
		"config":       cfg,
		"configured":   upstream.IsBybitConfigured(),
		"apiKeyMasked": apiKey,
		"hasApiKey":    bk.APIKey != "",
		"hasApiSecret": bk.APISecret != "",
		"chains":       store.CryptoChains,
		"pool":         store.ListPool(),
		"orders":       store.ListCryptoOrders(0, 100),
	}
}

func (a *API) handleCryptoConfig(w http.ResponseWriter, r *http.Request) {
	cfg := store.GetCryptoConfig()
	ok(w, map[string]any{
		"enabled": cfg.Enabled, "configured": upstream.IsBybitConfigured(),
		"rate": cfg.Rate, "minUsdt": cfg.MinUsdt,
		"minCny":       round2(cfg.MinUsdt * cfg.Rate),
		"defaultChain": cfg.Chain, "chains": store.CryptoChains,
	})
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func (a *API) handleCryptoCreate(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		Cny   float64 `json:"cny"`
		Chain string  `json:"chain"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	order, e := service.CreateCryptoOrder(user.UID, body.Cny, body.Chain)
	if e != nil {
		fail(w, http.StatusBadRequest, e.Error())
		return
	}
	ok(w, map[string]any{"order": order})
}

func (a *API) handleCryptoGet(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	id := queryInt(r, "id")
	if id == 0 {
		fail(w, http.StatusBadRequest, "缺少订单号")
		return
	}
	owned := store.GetCryptoOrder(id)
	if owned == nil {
		fail(w, http.StatusNotFound, "订单不存在")
		return
	}
	if owned.UID != user.UID && user.Role != "admin" {
		fail(w, http.StatusForbidden, "无权查看")
		return
	}
	ok(w, map[string]any{"order": service.CheckCryptoOrder(id)})
}

func (a *API) handleCryptoCancel(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	id := queryInt(r, "id")
	if id == 0 {
		fail(w, http.StatusBadRequest, "缺少订单号")
		return
	}
	order, e := service.CancelCryptoOrderFor(id, user.UID, user.Role == "admin")
	if e != nil {
		fail(w, http.StatusBadRequest, e.Error())
		return
	}
	ok(w, map[string]any{"order": order})
}

func (a *API) handleCryptoCron(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("x-cron-token")
	}
	expected := db.GetMeta("crypto_cron_secret")
	if expected != "" {
		if token != expected {
			fail(w, http.StatusUnauthorized, "token 错误")
			return
		}
	} else {
		user := sessionUser(r)
		if user == nil || user.Role != "admin" {
			fail(w, http.StatusUnauthorized, "未授权")
			return
		}
	}

	store.ExpireCryptoOrders()
	waiting := store.ListCryptoOrders(0, 50)
	checked := 0
	for _, o := range waiting {
		if o.Status != "waiting" {
			continue
		}
		if checked >= 20 {
			break
		}
		service.CheckCryptoOrder(o.ID)
		checked++
	}
	ok(w, map[string]any{"checked": checked})
}

// ===== 后台加密充值 =====

func (a *API) handleAdminCrypto(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	store.ExpireCryptoOrders()
	writeJSON(w, http.StatusOK, cryptoSnapshot())
}

func (a *API) handleAdminCryptoPost(w http.ResponseWriter, r *http.Request) {
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
	orderID, _ := store.ToInt(body["orderId"])
	id, _ := store.ToInt(body["id"])

	switch action {
	case "saveConfig":
		store.SaveCryptoConfig(body)
		refresh := service.RefreshPoolAddresses()
		store.WriteAudit(&admin.UID, "crypto_config_save", "", "", nil,
			map[string]any{"enabled": body["enabled"], "chain": body["chain"]}, "保存加密充值配置")
		snap := cryptoSnapshot()
		snap["refresh"] = refresh
		writeJSON(w, http.StatusOK, snap)
	case "refreshPool":
		refresh := service.RefreshPoolAddresses()
		snap := cryptoSnapshot()
		snap["refresh"] = refresh
		writeJSON(w, http.StatusOK, snap)
	case "addPool":
		uid := str(body["uid"])
		if uid == "" {
			fail(w, http.StatusBadRequest, "请输入子账户 UID")
			return
		}
		added := store.AddPoolUID(uid)
		refresh := service.RefreshPoolAddresses()
		store.WriteAudit(&admin.UID, "crypto_pool_add", "", uid, nil, nil, "添加地址池子账户")
		snap := cryptoSnapshot()
		snap["added"] = added
		snap["refresh"] = refresh
		writeJSON(w, http.StatusOK, snap)
	case "removePool":
		if id == 0 {
			fail(w, http.StatusBadRequest, "缺少 ID")
			return
		}
		removed := store.RemovePoolSlot(id)
		store.WriteAudit(&admin.UID, "crypto_pool_remove", "", itoa(id), nil, nil, "移除地址池子账户")
		snap := cryptoSnapshot()
		snap["removed"] = removed
		writeJSON(w, http.StatusOK, snap)
	case "togglePool":
		if id == 0 {
			fail(w, http.StatusBadRequest, "缺少 ID")
			return
		}
		enabled, _ := body["enabled"].(bool)
		changed := store.SetPoolSlotEnabled(id, enabled)
		snap := cryptoSnapshot()
		snap["changed"] = changed
		writeJSON(w, http.StatusOK, snap)
	case "checkOrder":
		if orderID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单")
			return
		}
		order := service.CheckCryptoOrder(orderID)
		snap := cryptoSnapshot()
		snap["order"] = order
		writeJSON(w, http.StatusOK, snap)
	case "checkOrders":
		store.ExpireCryptoOrders()
		waiting := store.ListCryptoOrders(0, 50)
		checked := 0
		for _, o := range waiting {
			if o.Status != "waiting" || checked >= 10 {
				continue
			}
			service.CheckCryptoOrder(o.ID)
			checked++
		}
		snap := cryptoSnapshot()
		snap["checked"] = checked
		writeJSON(w, http.StatusOK, snap)
	case "simulate":
		if orderID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单")
			return
		}
		order := service.SimulateCryptoOrder(orderID)
		store.WriteAudit(&admin.UID, "crypto_order_simulate", "", itoa(orderID), nil, nil, "模拟到账（测试）")
		snap := cryptoSnapshot()
		snap["order"] = order
		writeJSON(w, http.StatusOK, snap)
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}
