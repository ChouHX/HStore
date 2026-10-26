package httpapi

import (
	"net/http"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	if body.Username == "" || body.Password == "" {
		fail(w, http.StatusBadRequest, "请输入用户名和密码")
		return
	}
	user := store.Authenticate(body.Username, body.Password)
	if user == nil {
		fail(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if user.Status != "active" {
		fail(w, http.StatusForbidden, "账号已被冻结")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    db.SignSession(user.UID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 7,
	})
	ok(w, map[string]any{"user": user})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1})
	ok(w, nil)
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"user": sessionUser(r)})
}

func (a *API) handleAnnouncements(w http.ResponseWriter, r *http.Request) {
	normal, popup := store.PublicAnnouncements()
	ok(w, map[string]any{"normal": normal, "popup": popup})
}

func (a *API) handleSiteConfig(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"redeemPurchaseUrl": store.GetRedeemPurchaseURL()})
}

func (a *API) handleUpstreamAccount(w http.ResponseWriter, r *http.Request) {
	if !upstream.IsConfigured() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "balance": nil, "message": "未配置 App-Id"})
		return
	}
	balance, e := upstream.GetAccount()
	if e != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "balance": nil, "message": e.Error()})
		return
	}
	ok(w, map[string]any{"balance": balance})
}

func (a *API) handleMyProfile(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	fresh := store.GetUser(user.UID)
	if fresh == nil {
		fail(w, http.StatusNotFound, "用户不存在")
		return
	}
	ok(w, map[string]any{
		"user":       fresh,
		"levels":     store.ListLevels(),
		"orderCount": store.CountUserOrders(fresh.UID),
	})
}

func (a *API) handleMyLedger(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	page, pageSize, offset := parsePage(r, 12)
	items := store.ListLedger(user.UID, pageSize, offset)
	total := store.CountLedger(user.UID)
	ok(w, paged(items, total, page, pageSize))
}

// ts 供各 handler 统一取时间戳。
func ts() int64 { return time.Now().UnixMilli() }
