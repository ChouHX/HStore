package httpapi

import (
	stdhtml "html"
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
	if store.RegisterMailCountToday() >= cfg.DailyLimit() {
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
	html := registerMailHTML(cfg.FromName, code, email)
	if err := mail.Send(r.Context(), cfg, mail.SendOptions{
		To: email, Subject: subject, Text: text, HTML: html,
	}); err != nil {
		store.DeleteRegisterCode(email, code)
		if mail.IsQuotaError(err) {
			fail(w, http.StatusTooManyRequests, "当日注册量已达上限，请明日再试")
			return
		}
		log.Printf("register mail send failed (%s): %v", cfg.Provider, err)
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

// handleAdminSMTPGet 返回发信配置（不返回明文密钥）。
func (a *API) handleAdminSMTPGet(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	cfg := store.GetSMTPConfig()
	ok(w, map[string]any{
		"provider": cfg.Provider,
		"host":     cfg.Host, "port": cfg.Port, "security": cfg.Security,
		"username": cfg.Username, "fromName": cfg.FromName, "enabled": cfg.Enabled,
		"hasPassword": cfg.Password != "", "passwordMasked": store.MaskSecret(cfg.Password),
		"resendDomain":     cfg.ResendDomain,
		"hasResendApiKey":  cfg.ResendAPIKey != "",
		"resendKeyMasked":  store.MaskSecret(cfg.ResendAPIKey),
		"resendFrom":       mail.ResendFromAddress(cfg.ResendDomain, cfg.FromName),
		"ready":            cfg.Ready(),
		"todaySent":        store.RegisterMailCountToday(),
		"dailyLimit":       cfg.DailyLimit(),
		"smtpDailyLimit":   store.RegisterDailyMailLimit,
		"resendDailyLimit": store.ResendDailyMailLimit,
	})
}

// handleAdminSMTPPost 保存发信配置；密钥留空表示不修改。
func (a *API) handleAdminSMTPPost(w http.ResponseWriter, r *http.Request) {
	admin := requireAdmin(w, r)
	if admin == nil {
		return
	}
	var body struct {
		Action       string `json:"action"`
		TestTo       string `json:"testTo"`
		Provider     string `json:"provider"`
		Host         string `json:"host"`
		Port         int    `json:"port"`
		Security     string `json:"security"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		FromName     string `json:"fromName"`
		Enabled      bool   `json:"enabled"`
		ResendDomain string `json:"resendDomain"`
		ResendAPIKey string `json:"resendApiKey"`
	}
	if e := readJSON(r, &body); e != nil {
		fail(w, http.StatusBadRequest, "参数错误")
		return
	}

	// 发送测试邮件：用当前已保存的配置，不改配置。
	if body.Action == "test" {
		to := normalizeEmail(body.TestTo)
		if !emailPattern.MatchString(to) {
			fail(w, http.StatusBadRequest, "请输入正确的测试收件邮箱")
			return
		}
		cfg := store.GetSMTPConfig()
		if !cfg.Ready() {
			fail(w, http.StatusBadRequest, "发信配置未完成，请先保存")
			return
		}
		text := "这是一封来自 " + cfg.FromName + " 的测试邮件。\n\n如果你收到它，说明发信配置正常。"
		html := "<div style=\"font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC',sans-serif;font-size:14px;color:#1d2129;line-height:1.7\">" +
			"<p><strong>发信配置正常。</strong></p><p>这是一封来自 " + stdhtml.EscapeString(cfg.FromName) + " 的测试邮件。</p></div>"
		if err := mail.Send(r.Context(), cfg, mail.SendOptions{
			To: to, Subject: "【" + cfg.FromName + "】发信配置测试", Text: text, HTML: html,
		}); err != nil {
			log.Printf("smtp test send failed (%s): %v", cfg.Provider, err)
			fail(w, http.StatusBadGateway, "发送失败："+err.Error())
			return
		}
		store.WriteAudit(&admin.UID, "settings_smtp_test", "", to, nil, nil, "发送测试邮件")
		ok(w, map[string]any{"message": "测试邮件已发送至 " + to})
		return
	}

	cfg := store.GetSMTPConfig()
	if body.Provider == string(store.MailProviderResend) {
		cfg.Provider = store.MailProviderResend
	} else {
		cfg.Provider = store.MailProviderSMTP
	}
	cfg.Host = strings.TrimSpace(body.Host)
	cfg.Port = body.Port
	cfg.Security = body.Security
	cfg.Username = strings.TrimSpace(body.Username)
	cfg.FromName = strings.TrimSpace(body.FromName)
	cfg.Enabled = body.Enabled
	cfg.ResendDomain = strings.TrimSpace(body.ResendDomain)
	if body.Password != "" {
		cfg.Password = strings.ReplaceAll(body.Password, " ", "")
	}
	if body.ResendAPIKey != "" {
		cfg.ResendAPIKey = strings.TrimSpace(body.ResendAPIKey)
	}
	if cfg.FromName == "" {
		cfg.FromName = "HShop"
	}

	// 按通道校验：只校验当前启用通道的必填项，另一通道允许留空。
	if cfg.Provider == store.MailProviderResend {
		if cfg.ResendDomain == "" || cfg.ResendAPIKey == "" {
			fail(w, http.StatusBadRequest, "请填写 Resend 发信域名与 API Key")
			return
		}
		if !strings.Contains(cfg.ResendDomain, ".") {
			fail(w, http.StatusBadRequest, "发信域名格式不正确，例如 opcuu.com")
			return
		}
	} else {
		if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 || cfg.Username == "" {
			fail(w, http.StatusBadRequest, "请填写完整的 SMTP 服务器、端口和账号")
			return
		}
		if cfg.Security != "ssl" && cfg.Security != "starttls" {
			fail(w, http.StatusBadRequest, "加密方式只能是 ssl 或 starttls")
			return
		}
	}
	if err := store.SaveSMTPConfig(cfg); err != nil {
		fail(w, http.StatusInternalServerError, "保存失败")
		return
	}
	store.WriteAudit(&admin.UID, "settings_smtp", "", string(cfg.Provider), nil,
		map[string]any{"provider": cfg.Provider, "fromName": cfg.FromName}, "更新发信配置")
	ok(w, nil)
}

// registerMailHTML 生成注册验证码邮件的富文本版本（内联样式，兼容 Gmail / Outlook）。
func registerMailHTML(brand, code, email string) string {
	b := stdhtml.EscapeString(brand)
	return `<!doctype html>
<html lang="zh-CN">
<body style="margin:0;padding:0;background:#f2f3f5;">
  <div style="display:none;max-height:0;overflow:hidden;opacity:0;">您的验证码是 ` + code + `，10 分钟内有效。</div>
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f2f3f5;padding:32px 12px;">
    <tr><td align="center">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:520px;background:#ffffff;border-radius:14px;border:1px solid #e5e6eb;">
        <tr><td style="padding:28px 32px 8px 32px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;">
          <div style="font-size:18px;font-weight:700;color:#1d2129;letter-spacing:.2px;">` + b + `</div>
          <div style="margin-top:6px;font-size:12px;color:#86909c;">邮箱验证码 · 注册账号</div>
        </td></tr>
        <tr><td style="padding:8px 32px 0 32px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;">
          <p style="margin:16px 0 0 0;font-size:14px;line-height:1.7;color:#4e5969;">你好，</p>
          <p style="margin:6px 0 0 0;font-size:14px;line-height:1.7;color:#4e5969;">你正在使用 <strong style="color:#1d2129;">` + email + `</strong> 注册 ` + b + `，请在页面中输入以下验证码完成验证：</p>
        </td></tr>
        <tr><td style="padding:20px 32px 0 32px;">
          <div style="background:#f2f7ff;border:1px solid #bedaff;border-radius:10px;padding:18px 12px;text-align:center;">
            <div style="font-family:'SFMono-Regular',Consolas,monospace;font-size:34px;font-weight:700;letter-spacing:10px;color:#165dff;line-height:1.2;">` + code + `</div>
            <div style="margin-top:8px;font-size:12px;color:#86909c;">验证码 10 分钟内有效</div>
          </div>
        </td></tr>
        <tr><td style="padding:18px 32px 0 32px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;">
          <p style="margin:0;font-size:12px;line-height:1.7;color:#86909c;">为了账号安全，请勿将验证码转发或告知他人。如果这不是你的操作，请直接忽略本邮件，你的邮箱不会被注册。</p>
        </td></tr>
        <tr><td style="padding:22px 32px 28px 32px;">
          <div style="border-top:1px solid #f2f3f5;padding-top:14px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;font-size:11px;color:#a9aeb8;">
            此邮件由系统自动发送，请勿直接回复。
          </div>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`
}
