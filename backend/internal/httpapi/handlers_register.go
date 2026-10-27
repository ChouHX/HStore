package httpapi

import (
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/mail"
	"hstore/backend/internal/store"
)

var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

const registerCodeResendInterval = 60 * 1000 // 同一邮箱 60 秒内只能发一次

func normalizeEmail(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

// handleRegisterCode 发送注册验证码。
func (a *API) handleRegisterCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	email := normalizeEmail(body.Email)
	if len(email) > 128 || !emailPattern.MatchString(email) {
		fail(w, http.StatusBadRequest, "请输入正确的邮箱地址")
		return
	}
	if store.EmailRegistered(email) {
		fail(w, http.StatusBadRequest, "该邮箱已注册，请直接登录")
		return
	}
	if !allowRegisterCode(clientIP(r)) {
		fail(w, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
		return
	}
	cfg := store.GetSMTPConfig()
	if !cfg.Ready() {
		fail(w, http.StatusServiceUnavailable, "注册功能暂未开放，请联系管理员")
		return
	}
	if store.RegisterMailCountToday() >= store.RegisterDailyMailLimit {
		fail(w, http.StatusTooManyRequests, "当日注册量已达上限，请明日再试")
		return
	}
	if ts := store.LatestRegisterCodeAt(email); ts > 0 && time.Now().UnixMilli()-ts < registerCodeResendInterval {
		fail(w, http.StatusTooManyRequests, "验证码已发送，请 60 秒后再试")
		return
	}
	code, err := store.NewRegisterCode(email, clientIP(r))
	if err != nil {
		fail(w, http.StatusInternalServerError, "验证码生成失败，请稍后重试")
		return
	}
	subject := "【" + cfg.FromName + "】注册验证码"
	text := "您正在注册 " + cfg.FromName + " 账号。\n\n验证码：" + code + "\n\n验证码 10 分钟内有效，请勿泄露给他人。如果这不是您的操作，请忽略本邮件。"
	if err := mail.SMTPSend(cfg.Host, cfg.Port, cfg.Security, cfg.Username, cfg.Password, cfg.FromName, email, subject, text); err != nil {
		store.DeleteRegisterCode(email, code)
		if mail.IsSMTPDailyLimit(err) {
			fail(w, http.StatusTooManyRequests, "当日注册量已达上限，请明日再试")
			return
		}
		log.Printf("register mail send failed: %v", err)
		fail(w, http.StatusBadGateway, "验证码发送失败，请稍后重试")
		return
	}
	ok(w, map[string]any{"message": "验证码已发送，请查收邮件（含垃圾邮件文件夹）"})
}

// handleRegister 校验验证码并创建账号，成功后直接登录。
func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	email := normalizeEmail(body.Email)
	if len(email) > 128 || !emailPattern.MatchString(email) {
		fail(w, http.StatusBadRequest, "请输入正确的邮箱地址")
		return
	}
	code := strings.TrimSpace(body.Code)
	if len(code) != 6 {
		fail(w, http.StatusBadRequest, "请输入 6 位数字验证码")
		return
	}
	if e := store.ValidatePassword(body.Password); e != nil {
		fail(w, http.StatusBadRequest, e.Error())
		return
	}
	if len([]rune(body.Nickname)) > 32 {
		fail(w, http.StatusBadRequest, "昵称最多 32 个字符")
		return
	}
	if store.EmailRegistered(email) {
		fail(w, http.StatusBadRequest, "该邮箱已注册，请直接登录")
		return
	}
	if ok2, msg := store.VerifyRegisterCode(email, code); !ok2 {
		fail(w, http.StatusBadRequest, msg)
		return
	}
	user, err := store.CreateRegisteredUser(email, body.Password, strings.TrimSpace(body.Nickname))
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if user == nil {
		fail(w, http.StatusInternalServerError, "注册失败，请稍后重试")
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
	store.WriteAudit(&user.UID, "register", "user", itoa(user.UID), nil, nil, "邮箱验证码注册")
	ok(w, map[string]any{"user": user})
}

// handleAdminSMTPGet 返回发信配置（不返回明文密码）。
func (a *API) handleAdminSMTPGet(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	cfg := store.GetSMTPConfig()
	ok(w, map[string]any{
		"host": cfg.Host, "port": cfg.Port, "security": cfg.Security,
		"username": cfg.Username, "fromName": cfg.FromName, "enabled": cfg.Enabled,
		"hasPassword": cfg.Password != "", "passwordMasked": store.MaskSecret(cfg.Password),
		"todaySent": store.RegisterMailCountToday(), "dailyLimit": store.RegisterDailyMailLimit,
	})
}

// handleAdminSMTPPost 保存发信配置；密码留空表示不修改。
func (a *API) handleAdminSMTPPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Security string `json:"security"`
		Username string `json:"username"`
		Password string `json:"password"`
		FromName string `json:"fromName"`
		Enabled  bool   `json:"enabled"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}
	cfg := store.GetSMTPConfig()
	cfg.Host = strings.TrimSpace(body.Host)
	cfg.Port = body.Port
	cfg.Security = body.Security
	cfg.Username = strings.TrimSpace(body.Username)
	cfg.FromName = strings.TrimSpace(body.FromName)
	cfg.Enabled = body.Enabled
	if body.Password != "" {
		cfg.Password = strings.ReplaceAll(body.Password, " ", "")
	}
	if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 || cfg.Username == "" {
		fail(w, http.StatusBadRequest, "请填写完整的 SMTP 服务器、端口和账号")
		return
	}
	if cfg.Security != "ssl" && cfg.Security != "starttls" {
		fail(w, http.StatusBadRequest, "加密方式只能是 ssl 或 starttls")
		return
	}
	if cfg.FromName == "" {
		cfg.FromName = "HShop"
	}
	if err := store.SaveSMTPConfig(cfg); err != nil {
		fail(w, http.StatusInternalServerError, "保存失败")
		return
	}
	store.WriteAudit(&admin.UID, "settings_smtp", "", "", nil, nil, "更新 SMTP 发信配置")
	ok(w, nil)
}
