package httpapi

import (
	"net/http"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/store"
	"hstore/backend/internal/upstream"
)

// cookieSecure 判断当前请求是否走 HTTPS，决定会话 Cookie 是否带 Secure。
// 服务在 1Panel openresty 后面，TLS 在边缘终结，所以还要看 X-Forwarded-Proto。
// 本地 http 调试时两者都不满足，Cookie 不带 Secure，仍然可用。
func cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("X-Forwarded-Proto")), "https")
}

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
	ip := clientIP(r)
	// 先限流再校验：Authenticate 是 scrypt，代价很高，不能让它被无限调用。
	if !allowLogin(ip) {
		fail(w, http.StatusTooManyRequests, "登录尝试过于频繁，请稍后再试")
		return
	}
	// 按账号的失败锁定。提示对所有账号统一措辞，不泄露账号是否存在。
	if loginLocked(body.Username) {
		fail(w, http.StatusTooManyRequests, "登录失败次数过多，请 15 分钟后再试")
		return
	}
	user := store.Authenticate(body.Username, body.Password)
	if user == nil {
		locked := noteLoginFailure(body.Username)
		reason := "用户名或密码错误"
		if locked {
			reason = "用户名或密码错误，账号已临时锁定"
		}
		target := body.Username
		if target == "" {
			target = ip
		}
		store.WriteAudit(nil, "login_failed", "user", target, nil, nil, reason+"（IP "+ip+"）")
		fail(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if user.Status != "active" {
		fail(w, http.StatusForbidden, "账号已被冻结")
		return
	}
	clearLoginFailures(body.Username)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    db.SignSession(user.UID),
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 7,
	})
	store.WriteAudit(&user.UID, "login", "user", itoa(user.UID), nil, nil, "登录成功（IP "+ip+"）")
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

// handleUpstreamAccount 返回上游（天启）账户余额，属于内部信息，仅管理员可用。
func (a *API) handleUpstreamAccount(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
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

// handleChangePassword 用户自助修改密码：必须校验原密码。
func (a *API) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var body struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	if e := store.ValidatePassword(body.NewPassword); e != nil {
		fail(w, http.StatusBadRequest, e.Error())
		return
	}
	ok2, msg := store.ChangePassword(user.UID, body.OldPassword, body.NewPassword)
	if !ok2 {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	store.WriteAudit(&user.UID, "password_change", "user", itoa(user.UID), nil, nil, "修改登录密码")
	ok(w, map[string]any{"message": "密码已修改"})
}
