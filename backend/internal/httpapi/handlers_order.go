package httpapi

import (
	"net/http"
	"strings"

	"hstore/backend/internal/model"
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
		Contact string              `json:"contact"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	source := "user"
	if user.Role == "admin" && body.Source == "admin" {
		source = "admin"
	}
	// 下单必须留联系方式，否则买家丢了页面就无法按订单号 / 联系方式找回订单。
	contact, e := service.ValidateContact(body.Contact)
	if e != nil {
		fail(w, http.StatusBadRequest, e.Error())
		return
	}
	res := service.PlaceOrder(service.PlaceOrderOptions{
		User: user, GoodsID: body.GoodsID, Count: body.Count,
		Source: source, Payment: "balance", Attach: body.Attach, Contact: contact,
	})
	if !res.OK {
		fail(w, res.Status, res.Message)
		return
	}
	ok(w, map[string]any{"order": hideUpstreamForUser(res.Order, user.Role)})
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
	// 用户传入的是本地单号，查上游必须用上游单号。
	upstreamNo := owned.UpstreamOrderNo
	if upstreamNo == "" {
		// 没有上游单号（历史单或管理员手工建的单）就不查上游，直接返回本地记录。
		ok(w, map[string]any{"order": hideUpstreamForUser(owned, user.Role)})
		return
	}
	detail, e := upstream.GetOrderDetail(upstreamNo)
	if e != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok": false, "message": e.Error(), "order": hideUpstreamForUser(owned, user.Role),
		})
		return
	}
	order := store.ApplyRemoteOrder(*detail)
	if order != nil && order.Status == 3 && order.Settled == 0 && order.Source == "user" && order.UserUID != nil {
		store.SettleConsumption(*order.UserUID, order.SaleMoney, order.OrderNumber)
		store.MarkSettled(order.OrderNumber)
		order = store.GetOrder(order.OrderNumber)
	}
	ok(w, map[string]any{"order": hideUpstreamForUser(order, user.Role)})
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
		// 个人订单接口只看自己的订单；管理员的跨用户订单在后台 /admin/orders 里看。
		UID: user.UID,
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
	// CardOwner 查不到时返回空串。不能因为「查不到」就把 cardID 直接透传给上游
	// 作废接口：那样任意未登记的 cardID 都能被批量提交（骚扰上游 / 越权作废他人卡密）。
	// 管理员保留原行为（后台需要处理未入库的卡密）。
	if user.Role != "admin" {
		if owner == "" {
			fail(w, http.StatusForbidden, "无权操作该卡密")
			return
		}
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
	var body struct {
		Code string `json:"code"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	user := sessionUser(r)
	if user == nil {
		// 旧 Redeem 的兑换码本来就不要求登录；匿名兑换只允许「商品码」，
		// 余额码仍然必须登录，避免把余额发到临时游客账号上。
		if !allowQuery(clientIP(r)) {
			fail(w, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
			return
		}
		row := store.GetCodeByCode(body.Code)
		if row == nil || row.GoodsID == nil {
			fail(w, http.StatusUnauthorized, "请先登录后再兑换")
			return
		}
		guest, e := store.CreateGuestUser()
		if e != nil {
			fail(w, http.StatusInternalServerError, "兑换失败，请稍后重试")
			return
		}
		user = guest
	}
	res := service.Redeem(user.UID, body.Code)
	fresh := store.GetUser(user.UID)
	payload := map[string]any{
		"ok": res.OK, "message": res.Message, "kind": res.Kind,
		"amount": res.Amount, "order": hideUpstreamForUser(res.Order, user.Role), "orderNumber": res.OrderNumber,
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": res.OK, "message": res.Message,
		"order": hideUpstreamForUser(res.Order, user.Role)})
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
		Account     string `json:"account"`
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
	// 自定义导入账号：直接读取，不再依赖订单 / 兑换码
	if strings.TrimSpace(body.Account) != "" {
		res, e := service.MailListByAccount(body.Account, body.Folder, body.Page, body.PageSize, body.WithBody)
		if e != nil {
			fail(w, http.StatusBadRequest, e.Error())
			return
		}
		ok(w, map[string]any{"result": res, "cardId": "", "email": res.Email})
		return
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
		Account     string `json:"account"`
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
	if strings.TrimSpace(body.Account) != "" {
		res, e := service.MailDetailByAccount(body.Account, body.Folder, body.MessageID)
		if e != nil {
			fail(w, http.StatusBadRequest, e.Error())
			return
		}
		ok(w, map[string]any{"result": res})
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

// handleOrderQuery 是公开的订单查询：凭订单号或下单时的联系方式。
// 游客不需要登录，所以按 IP 限流，防止被拿来枚举订单号。
func (a *API) handleOrderQuery(w http.ResponseWriter, r *http.Request) {
	if !allowQuery(clientIP(r)) {
		fail(w, http.StatusTooManyRequests, "查询过于频繁，请稍后再试")
		return
	}
	// orderNumbers：前端把本地保存的订单号一次性带上来，直接回填列表
	if raw := strings.TrimSpace(r.URL.Query().Get("orderNumbers")); raw != "" {
		ok(w, map[string]any{"items": store.QueryOrdersByNumbers(strings.Split(raw, ",")), "total": 0})
		return
	}
	// tokens：本机保存的游客单凭证，批量回填（游客单在发货前没有订单号）
	if raw := strings.TrimSpace(r.URL.Query().Get("tokens")); raw != "" {
		ok(w, map[string]any{"items": store.QueryGuestByTokens(strings.Split(raw, ",")), "total": 0})
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	if keyword == "" {
		fail(w, http.StatusBadRequest, "请输入订单号或联系方式")
		return
	}
	if len([]rune(keyword)) > 64 {
		fail(w, http.StatusBadRequest, "查询内容过长")
		return
	}
	items := store.QueryPublicOrders(keyword, 100)
	ok(w, map[string]any{"items": items, "total": len(items), "keyword": keyword})
}

// handleOrderQueryDetail 是公开订单详情：必须给出第二重校验凭证，避免订单号被枚举后
// 直接拿到卡密。凭证有两种：
//   - 下单时填写的联系方式（商城购买、游客购买的单都有 contact）；
//   - 兑换码（兑换码取件单没有 contact，兑换码本身就是凭证）。
func (a *API) handleOrderQueryDetail(w http.ResponseWriter, r *http.Request) {
	if !allowQuery(clientIP(r)) {
		fail(w, http.StatusTooManyRequests, "查询过于频繁，请稍后再试")
		return
	}
	orderNumber := strings.TrimSpace(r.URL.Query().Get("orderNumber"))
	contact := strings.TrimSpace(r.URL.Query().Get("contact"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if orderNumber == "" || (contact == "" && code == "") {
		fail(w, http.StatusBadRequest, "请同时提供订单号与联系方式（或兑换码）")
		return
	}
	order := store.GetOrder(orderNumber)
	if order == nil {
		fail(w, http.StatusNotFound, "订单不存在")
		return
	}
	if !publicOrderCredentialOK(order, contact, code) {
		fail(w, http.StatusForbidden, "联系方式或兑换码与订单不匹配")
		return
	}
	// 公开查询永远不返回上游单号。
	ok(w, map[string]any{"order": hideUpstreamForUser(order, "guest")})
}

// publicOrderCredentialOK 校验公开查询的第二重凭证：联系方式或兑换码，任一匹配即可。
// 兑换码取件单（含自营卡密单）下单链路不写 contact，只能用兑换码验证。
func publicOrderCredentialOK(order *model.LocalOrder, contact, code string) bool {
	if contact != "" && order.Contact != "" &&
		store.NormalizeContact(order.Contact) == store.NormalizeContact(contact) {
		return true
	}
	if code != "" {
		if row := store.GetCodeByCode(code); row != nil && row.OrderNumber == order.OrderNumber {
			return true
		}
	}
	return false
}
