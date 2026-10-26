package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

// SessionCookie 是会话 Cookie 名。
const SessionCookie = "faka_session"

// API 聚合所有 HTTP 处理器。
type API struct {
	mux *http.ServeMux
}

// New 创建 API 并注册路由。
func New() *API {
	a := &API{mux: http.NewServeMux()}
	a.routes()
	return a
}

// Handler 返回 http.Handler。
func (a *API) Handler() http.Handler { return a.mux }

// ===== 响应辅助 =====

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func ok(w http.ResponseWriter, body map[string]any) {
	if body == nil {
		body = map[string]any{}
	}
	body["ok"] = true
	writeJSON(w, http.StatusOK, body)
}

func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "message": message})
}

// readJSON 解析请求体；空 body 不报错。
func readJSON(r *http.Request, out any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20))
	if e := dec.Decode(out); e != nil {
		if e.Error() == "EOF" {
			return nil
		}
		return e
	}
	return nil
}

// sessionUser 从 Cookie 解析当前用户。
func sessionUser(r *http.Request) *model.User {
	c, e := r.Cookie(SessionCookie)
	if e != nil || c.Value == "" {
		return nil
	}
	uid, ok := db.VerifySession(c.Value)
	if !ok {
		return nil
	}
	return store.GetUser(uid)
}

// requireAdmin 校验管理员，返回用户；未通过时已写好响应。
func requireAdmin(w http.ResponseWriter, r *http.Request) *model.User {
	u := sessionUser(r)
	if u == nil {
		fail(w, http.StatusUnauthorized, "请先登录")
		return nil
	}
	if u.Role != "admin" {
		fail(w, http.StatusForbidden, "需要管理员权限")
		return nil
	}
	return u
}

func requireUser(w http.ResponseWriter, r *http.Request) *model.User {
	u := sessionUser(r)
	if u == nil {
		fail(w, http.StatusUnauthorized, "请先登录")
		return nil
	}
	return u
}

// ===== 分页 =====

func parsePage(r *http.Request, defSize int) (page, pageSize, offset int) {
	q := r.URL.Query()
	page, _ = strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(q.Get("pageSize"))
	if pageSize < 1 {
		pageSize = defSize
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, (page - 1) * pageSize
}

func paged(items any, total, page, pageSize int) map[string]any {
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	return map[string]any{
		"items": items, "total": total, "page": page, "pageSize": pageSize, "totalPages": totalPages,
	}
}

func queryInt(r *http.Request, key string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	return v
}

func queryIntList(r *http.Request, key string) []int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(raw, ",") {
		if n, e := strconv.Atoi(strings.TrimSpace(part)); e == nil && n != 0 {
			out = append(out, n)
		}
	}
	return out
}

func intPtr(v int) *int { return &v }

// ===== 小工具 =====

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func itoa(v int) string { return strconv.Itoa(v) }
