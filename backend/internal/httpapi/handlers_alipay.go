package httpapi

import (
	"net/http"
	"strings"

	"hstore/backend/internal/model"
	"hstore/backend/internal/service"
	"hstore/backend/internal/store"
)

func alipaySnapshot() map[string]any {
	cfg := store.GetAlipayConfig()
	pending := store.ListAlipayPendingLedger(100)
	return map[string]any{
		"config":        cfg,
		"configured":    cfg.Configured,
		"scanState":     store.GetAlipayScanState(),
		"orders":        store.ListAlipayOrders(0, 200),
		"pendingLedger": pending,
		"recentLedger":  store.ListAlipayLedger(100, ""),
		"openOrders":    store.CountOpenAlipayOrders(),
		"unmatched":     len(pending),
	}
}

func (a *API) handleAlipayConfig(w http.ResponseWriter, r *http.Request) {
	ok(w, service.AlipayPublicConfig())
}

func (a *API) handleAlipayCreate(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		Cny float64 `json:"cny"`
	}
	if err := readJSON(r, &body); err != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	order, err := service.CreateAlipayOrder(user, body.Cny)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := map[string]any{"order": order}
	if checkout, checkoutErr := service.GetAlipayCheckout(order.CheckoutToken); checkoutErr == nil {
		resp["checkout"] = checkout
	}
	ok(w, resp)
}

func (a *API) handleAlipayList(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	store.ExpireAlipayOrders()
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	ok(w, map[string]any{"orders": store.ListAlipayOrders(user.UID, limit)})
}

func (a *API) findAlipayOrder(w http.ResponseWriter, r *http.Request, user *model.User) *model.AlipayOrder {
	id := queryInt(r, "id")
	orderNo := strings.TrimSpace(r.URL.Query().Get("orderNo"))
	var order *model.AlipayOrder
	if id > 0 {
		order = store.GetAlipayOrder(id)
	} else if orderNo != "" {
		order = store.GetAlipayOrderByNo(orderNo)
	}
	if order == nil {
		fail(w, http.StatusNotFound, "订单不存在")
		return nil
	}
	if order.UID != user.UID && user.Role != "admin" {
		fail(w, http.StatusForbidden, "无权查看")
		return nil
	}
	return order
}

func (a *API) handleAlipayGet(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	store.ExpireAlipayOrders()
	order := a.findAlipayOrder(w, r, user)
	if order == nil {
		return
	}
	ok(w, map[string]any{"order": order})
}

func (a *API) handleAlipayClose(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	id := queryInt(r, "id")
	if id == 0 {
		fail(w, http.StatusBadRequest, "缺少订单号")
		return
	}
	order, err := service.CloseAlipayOrder(id, user)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": err.Error(), "order": order})
		return
	}
	ok(w, map[string]any{"order": order})
}

// handleAlipayCheckout 是公开收银台状态接口，token 本身即凭证。
func (a *API) handleAlipayCheckout(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	checkout, err := service.GetAlipayCheckout(token)
	if err != nil {
		fail(w, http.StatusNotFound, err.Error())
		return
	}
	ok(w, map[string]any{"checkout": checkout})
}

// ===== 管理端 =====

func (a *API) handleAdminAlipay(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	store.ExpireAlipayOrders()
	ok(w, alipaySnapshot())
}

func (a *API) handleAdminAlipayPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	action := str(body["action"])
	orderID, _ := store.ToInt(body["orderId"])

	switch action {
	case "saveConfig":
		if err := service.ValidateAlipayConfigPatch(body); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		oldCfg := store.GetAlipayConfig()
		store.SaveAlipayConfig(body)
		newCfg := store.GetAlipayConfig()
		if oldCfg.AppID != newCfg.AppID || oldCfg.Env != newCfg.Env || oldCfg.CollectionCode != newCfg.CollectionCode {
			store.ResetAlipayScanState()
		}
		store.WriteAudit(&admin.UID, "alipay_config_save", "", "", nil,
			map[string]any{"enabled": body["enabled"], "env": body["env"], "appId": body["appId"]},
			"保存支付宝经营码配置")
		ok(w, alipaySnapshot())
	case "test":
		result, err := service.TestAlipayConnection(r.Context())
		snap := alipaySnapshot()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"ok": false, "message": err.Error(), "config": snap["config"],
			})
			return
		}
		store.WriteAudit(&admin.UID, "alipay_test", "", "", nil, nil, "测试支付宝账务明细查询")
		snap["test"] = result
		ok(w, snap)
	case "scan":
		result, err := service.ScanAlipay(r.Context())
		snap := alipaySnapshot()
		snap["scan"] = result
		if err != nil {
			snap["ok"] = false
			snap["message"] = err.Error()
			writeJSON(w, http.StatusBadGateway, snap)
			return
		}
		store.WriteAudit(&admin.UID, "alipay_scan", "", "", nil, result, "手动查账")
		ok(w, snap)
	case "resetScan":
		store.ResetAlipayScanState()
		store.WriteAudit(&admin.UID, "alipay_scan_reset", "", "", nil, nil, "重置支付宝查账游标")
		snap := alipaySnapshot()
		snap["reset"] = true
		ok(w, snap)
	case "expire":
		n := service.ExpireAlipayOrders()
		snap := alipaySnapshot()
		snap["expired"] = n
		ok(w, snap)
	case "reconcilePending":
		settled, unmatched, ambiguous := service.ReconcilePendingAlipay(200)
		snap := alipaySnapshot()
		snap["reconcile"] = map[string]any{"settled": settled, "unmatched": unmatched, "ambiguous": ambiguous}
		ok(w, snap)
	case "checkOrder":
		if orderID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单")
			return
		}
		order := store.GetAlipayOrder(orderID)
		if order == nil {
			fail(w, http.StatusNotFound, "订单不存在")
			return
		}
		snap := alipaySnapshot()
		snap["order"] = order
		ok(w, snap)
	case "closeOrder":
		if orderID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单")
			return
		}
		order, err := service.CloseAlipayOrder(orderID, admin)
		snap := alipaySnapshot()
		snap["order"] = order
		if err != nil {
			snap["ok"] = false
			snap["message"] = err.Error()
			writeJSON(w, http.StatusBadRequest, snap)
			return
		}
		store.WriteAudit(&admin.UID, "alipay_order_close", "", itoa(orderID), nil, nil, "关闭支付宝充值订单")
		ok(w, snap)
	case "manualMatch":
		ledgerID, _ := store.ToInt(body["ledgerId"])
		if orderID == 0 || ledgerID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单或流水")
			return
		}
		order, warning, err := service.ManualMatchAlipay(orderID, int64(ledgerID))
		snap := alipaySnapshot()
		snap["order"] = order
		if warning != "" {
			snap["warning"] = warning
		}
		if err != nil {
			snap["ok"] = false
			snap["message"] = err.Error()
			writeJSON(w, http.StatusBadRequest, snap)
			return
		}
		store.WriteAudit(&admin.UID, "alipay_manual_match", "order", itoa(orderID), nil,
			map[string]any{"ledgerId": ledgerID}, "手工关联支付宝流水")
		ok(w, snap)
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}
