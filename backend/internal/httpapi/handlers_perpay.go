package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"hstore/backend/internal/model"
	payclient "hstore/backend/internal/perpay"
	"hstore/backend/internal/service"
	"hstore/backend/internal/store"
)

func perpayPublicConfig() map[string]any {
	cfg := store.GetPerPayConfig()
	return map[string]any{
		"enabled":       cfg.Enabled,
		"configured":    cfg.Configured,
		"minCny":        cfg.MinCny,
		"maxCny":        cfg.MaxCny,
		"productName":   cfg.ProductName,
		"notifyEnabled": cfg.WebhookReady,
		"returnEnabled": cfg.ReturnEnabled,
	}
}

func perpaySnapshot() map[string]any {
	cfg := store.GetPerPayConfig()
	notifyURL := ""
	returnURL := ""
	if cfg.PublicBaseURL != "" {
		origin, err := payclient.ValidateOrigin(cfg.PublicBaseURL)
		if err == nil {
			notifyURL = origin + "/api/webhooks/perpay"
			returnURL = origin + "/account/recharge"
		}
	}
	return map[string]any{
		"config":      cfg,
		"configured":  cfg.Configured,
		"notifyUrl":   notifyURL,
		"returnUrl":   returnURL,
		"orders":      store.ListPerPayOrders(0, 200),
		"events":      store.ListPerPayWebhookEvents(100),
		"hasApiKey":   cfg.HasAPISecret,
		"hasWebhook":  cfg.HasWebhookSecret,
		"webhookOnly": cfg.NotifyEnabled && cfg.HasWebhookSecret,
	}
}

// ===== 用户侧 =====

func (a *API) handlePerPayConfig(w http.ResponseWriter, r *http.Request) {
	ok(w, perpayPublicConfig())
}

func (a *API) handlePerPayCreate(w http.ResponseWriter, r *http.Request) {
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
	order, err := service.CreatePerPayOrder(user, body.Cny)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "message": err.Error(), "order": order,
		})
		return
	}
	ok(w, map[string]any{"order": order})
}

func (a *API) findPerPayOrder(w http.ResponseWriter, r *http.Request, user *model.User) *model.PerPayOrder {
	id := queryInt(r, "id")
	merchantNo := strings.TrimSpace(r.URL.Query().Get("merchantOrderNo"))
	var order *model.PerPayOrder
	if id > 0 {
		order = store.GetPerPayOrder(id)
	} else if merchantNo != "" {
		order = store.GetPerPayOrderByMerchantNo(merchantNo)
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

func (a *API) handlePerPayList(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	store.ExpirePerPayOrders()
	limit := queryInt(r, "limit")
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	ok(w, map[string]any{"orders": store.ListPerPayOrders(user.UID, limit)})
}

func (a *API) handlePerPayGet(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	order := a.findPerPayOrder(w, r, user)
	if order == nil {
		return
	}
	synced, err := service.SyncPerPayOrder(order.ID, false)
	if err != nil && synced == nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := map[string]any{"order": synced}
	if err != nil {
		resp["warning"] = err.Error()
	}
	ok(w, resp)
}

func (a *API) handlePerPayRetry(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		ID int `json:"id"`
	}
	if err := readJSON(r, &body); err != nil || body.ID == 0 {
		fail(w, http.StatusBadRequest, "缺少订单号")
		return
	}
	order, err := service.RetryPerPayOrder(body.ID, user, user.Role == "admin")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "message": err.Error(), "order": order,
		})
		return
	}
	ok(w, map[string]any{"order": order})
}

func (a *API) handlePerPayClose(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	id := queryInt(r, "id")
	if id == 0 {
		fail(w, http.StatusBadRequest, "缺少订单号")
		return
	}
	order, err := service.ClosePerPayOrder(id, user, user.Role == "admin")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "message": err.Error(), "order": order,
		})
		return
	}
	ok(w, map[string]any{"order": order})
}

// handlePerPayWebhook 接收 PerPay 签名通知。必须按原始请求体验签，并返回固定的 ACK JSON。
func (a *API) handlePerPayWebhook(w http.ResponseWriter, r *http.Request) {
	cfg := store.GetPerPaySecretConfig()
	if cfg.WebhookSecret == "" {
		fail(w, http.StatusNotFound, "通知接收未启用")
		return
	}
	secret, err := payclient.DecodeSecret(cfg.WebhookSecret, "通知密钥")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024+1))
	if err != nil {
		fail(w, http.StatusBadRequest, "读取通知失败")
		return
	}
	if len(body) > 256*1024 {
		fail(w, http.StatusRequestEntityTooLarge, "通知过大")
		return
	}
	verified, err := payclient.VerifyWebhook(r.Header, body, secret, time.Now())
	if err != nil {
		fail(w, http.StatusUnauthorized, err.Error())
		return
	}
	snap := service.PerPayWebhookSnapshot(verified.Event)
	_, _, err = store.ReceivePerPayWebhook(
		verified.Event.EventID,
		verified.DeliveryID,
		verified.Digest,
		verified.Event.EventType,
		snap,
	)
	if err != nil {
		if errors.Is(err, store.ErrPerPayWebhookConflict) {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schema":      "perpay:webhook-ack:v1",
		"ack":         true,
		"event_id":    verified.Event.EventID,
		"delivery_id": verified.DeliveryID,
	})
}

// ===== 管理端 =====

func (a *API) handleAdminPerPay(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	store.ExpirePerPayOrders()
	writeJSON(w, http.StatusOK, mergeOK(perpaySnapshot()))
}

func mergeOK(body map[string]any) map[string]any {
	body["ok"] = true
	return body
}

func (a *API) handleAdminPerPayPost(w http.ResponseWriter, r *http.Request) {
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
		if err := service.ValidatePerPayConfigPatch(body); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		store.SavePerPayConfig(body)
		store.WriteAudit(&admin.UID, "perpay_config_save", "", "", nil,
			map[string]any{
				"enabled": body["enabled"], "clientId": body["clientId"],
				"baseUrl": body["baseUrl"], "notifyEnabled": body["notifyEnabled"],
			}, "保存支付宝（PerPay）配置")
		writeJSON(w, http.StatusOK, mergeOK(perpaySnapshot()))
	case "test":
		status, err := service.TestPerPayConnection()
		snap := mergeOK(perpaySnapshot())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"ok": false, "message": err.Error(), "config": snap["config"],
			})
			return
		}
		store.WriteAudit(&admin.UID, "perpay_test", "", "", nil, nil, "测试 PerPay 连接")
		snap["status"] = status
		writeJSON(w, http.StatusOK, snap)
	case "checkOrder":
		if orderID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单")
			return
		}
		order, err := service.SyncPerPayOrder(orderID, true)
		snap := mergeOK(perpaySnapshot())
		snap["order"] = order
		if err != nil {
			snap["warning"] = err.Error()
		}
		writeJSON(w, http.StatusOK, snap)
	case "checkOrders":
		checked, errs := service.CheckPerPayOrders(30)
		snap := mergeOK(perpaySnapshot())
		snap["checked"] = checked
		snap["errors"] = errs
		writeJSON(w, http.StatusOK, snap)
	case "retryOrder":
		if orderID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单")
			return
		}
		order, err := service.RetryPerPayOrder(orderID, admin, true)
		snap := mergeOK(perpaySnapshot())
		snap["order"] = order
		if err != nil {
			snap["warning"] = err.Error()
		}
		writeJSON(w, http.StatusOK, snap)
	case "closeOrder":
		if orderID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单")
			return
		}
		order, err := service.ClosePerPayOrder(orderID, admin, true)
		snap := mergeOK(perpaySnapshot())
		snap["order"] = order
		if err != nil {
			snap["warning"] = err.Error()
		}
		writeJSON(w, http.StatusOK, snap)
	case "simulate":
		if orderID == 0 {
			fail(w, http.StatusBadRequest, "缺少订单")
			return
		}
		order, err := service.SimulatePerPayOrder(orderID)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		store.WriteAudit(&admin.UID, "perpay_order_simulate", "", itoa(orderID), nil, nil,
			"模拟支付宝到账（测试）")
		snap := mergeOK(perpaySnapshot())
		snap["order"] = order
		writeJSON(w, http.StatusOK, snap)
	default:
		fail(w, http.StatusBadRequest, "未知操作")
	}
}
