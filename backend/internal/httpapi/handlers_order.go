package httpapi

import (
	"net/http"

	"hstore/backend/internal/service"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

func (a *API) handleOrderCreate(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		GoodsID int                 `json:"goodsId"`
		Count   int                 `json:"count"`
		Attach  []map[string]string `json:"attach"`
		Source  string              `json:"source"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	source := "user"
	if user.Role == "admin" && body.Source == "admin" {
		source = "admin"
	}
	res := service.PlaceOrder(service.PlaceOrderOptions{
		User: user, GoodsID: body.GoodsID, Count: body.Count,
		Source: source, Payment: "balance", Attach: body.Attach,
	})
	if !res.OK {
		fail(w, res.Status, res.Message)
		return
	}
	ok(w, map[string]any{"order": res.Order})
}

func (a *API) handleOrderDetail(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		OrderNumber string `json:"orderNumber"`
	}
	if e := readJSON(r, &body); e != nil || body.OrderNumber == "" {
		fail(w, http.StatusBadRequest, "缺少订单号")
		return
	}
	owned := store.GetOrder(body.OrderNumber)
	if owned == nil {
		fail(w, http.StatusNotFound, "订单不存在")
		return
	}
	if user.Role != "admin" && (owned.UserUID == nil || *owned.UserUID != user.UID) {
		fail(w, http.StatusForbidden, "无权操作")
		return
	}
	detail, e := upstream.GetOrderDetail(body.OrderNumber)
	if e != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok": false, "message": e.Error(), "order": store.GetOrder(body.OrderNumber),
		})
		return
	}
	order := store.ApplyRemoteOrder(*detail)
	if order != nil && order.Status == 3 && order.Settled == 0 && order.Source == "user" && order.UserUID != nil {
		store.SettleConsumption(*order.UserUID, order.SaleMoney, order.OrderNumber)
		store.MarkSettled(order.OrderNumber)
		order = store.GetOrder(order.OrderNumber)
	}
	ok(w, map[string]any{"order": order})
}

func (a *API) handleOrderList(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	page, pageSize, offset := parsePage(r, 10)
	statusGroup := r.URL.Query().Get("status")
	var statuses []int
	switch statusGroup {
	case "done":
		statuses = []int{3}
	case "processing":
		statuses = []int{1, 2, 7, 8}
	}
	f := store.OrderFilter{
		Statuses: statuses,
		Keyword:  r.URL.Query().Get("keyword"),
		Limit:    pageSize,
		Offset:   offset,
	}
	if user.Role != "admin" {
		f.UID = user.UID
	}
	items := store.ListOrders(f)
	total := store.CountOrders(f)
	ok(w, paged(items, total, page, pageSize))
}

func (a *API) handleCardVoid(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		CardID string `json:"cardId"`
	}
	if e := readJSON(r, &body); e != nil || body.CardID == "" {
		fail(w, http.StatusBadRequest, "缺少卡券ID")
		return
	}
	owner := store.CardOwner(body.CardID)
	if owner != "" && user.Role != "admin" {
		order := store.GetOrder(owner)
		if order == nil || order.UserUID == nil || *order.UserUID != user.UID {
			fail(w, http.StatusForbidden, "无权操作")
			return
		}
	}
	status, result, e := upstream.VoidCard(body.CardID)
	if e != nil {
		fail(w, http.StatusInternalServerError, e.Error())
		return
	}
	if status == 0 {
		status = 2
	}
	if result == "" {
		result = "作废成功"
	}
	store.UpdateCardStatus(body.CardID, status, result)
	var order any
	if owner != "" {
		order = store.GetOrder(owner)
	}
	ok(w, map[string]any{"status": status, "order": order})
}

func (a *API) handleRedeem(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	res := service.Redeem(user.UID, body.Code)
	fresh := store.GetUser(user.UID)
	payload := map[string]any{
		"ok": res.OK, "message": res.Message, "kind": res.Kind,
		"amount": res.Amount, "order": res.Order, "orderNumber": res.OrderNumber,
		"user": fresh,
	}
	if fresh != nil {
		payload["balance"] = fresh.Balance
	} else {
		payload["balance"] = res.Balance
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *API) handlePickup(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		Code        string `json:"code"`
		OrderNumber string `json:"orderNumber"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	res := service.Pickup(user.UID, body.Code, body.OrderNumber, user.Role == "admin")
	writeJSON(w, http.StatusOK, map[string]any{"ok": res.OK, "message": res.Message, "order": res.Order})
}

// mailboxActor 解析取件的调用方：登录返回 uid / isAdmin，未登录返回 0 / false。
// 取件页是公开页面，凭兑换码即可访问，所以这里不用 requireUser。
// 匿名调用按 IP 限流，防止脚本批量试探兑换码。
func mailboxActor(w http.ResponseWriter, r *http.Request) (uid int, isAdmin, ok bool) {
	if user := sessionUser(r); user != nil {
		return user.UID, user.Role == "admin", true
	}
	if !allowPickup(clientIP(r)) {
		fail(w, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
		return 0, false, false
	}
	return 0, false, true
}

func (a *API) handleMailboxList(w http.ResponseWriter, r *http.Request) {
	uid, isAdmin, proceed := mailboxActor(w, r)
	if !proceed {
		return
	}
	var body struct {
		Code        string `json:"code"`
		OrderNumber string `json:"orderNumber"`
		CardID      string `json:"cardId"`
		Folder      string `json:"folder"`
		Page        int    `json:"page"`
		PageSize    int    `json:"pageSize"`
		WithBody    bool   `json:"withBody"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	if body.Page < 1 {
		body.Page = 1
	}
	if body.PageSize < 1 {
		body.PageSize = 20
	}
	res, card, e := service.MailList(uid, body.Code, body.OrderNumber, body.CardID,
		body.Folder, body.Page, body.PageSize, body.WithBody, isAdmin)
	if e != nil {
		fail(w, http.StatusBadRequest, e.Error())
		return
	}
	ok(w, map[string]any{"result": res, "cardId": card.CardID, "email": card.CardNo})
}

func (a *API) handleMailboxDetail(w http.ResponseWriter, r *http.Request) {
	uid, isAdmin, proceed := mailboxActor(w, r)
	if !proceed {
		return
	}
	var body struct {
		Code        string `json:"code"`
		OrderNumber string `json:"orderNumber"`
		CardID      string `json:"cardId"`
		Folder      string `json:"folder"`
		MessageID   string `json:"messageId"`
	}
	if e := readJSON(r, &body); e != nil || body.MessageID == "" {
		fail(w, http.StatusBadRequest, "缺少邮件 ID")
		return
	}
	res, _, e := service.MailDetail(uid, body.Code, body.OrderNumber, body.CardID,
		body.Folder, body.MessageID, isAdmin)
	if e != nil {
		fail(w, http.StatusBadRequest, e.Error())
		return
	}
	ok(w, map[string]any{"result": res})
}

// handleMailboxCards 返回当前用户所有「可取出邮箱」的卡券清单。
// 取件页据此直接列出邮箱，用户无需记住订单号或兑换码。
func (a *API) handleMailboxCards(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	items := store.UserMailboxCards(user.UID, queryInt(r, "limit"))
	ok(w, map[string]any{"items": items, "total": len(items)})
}
