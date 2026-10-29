package store

import (
	"testing"

	"hstore/backend/internal/db"
)

// TestMailProviderReady 覆盖两种发信通道的就绪判断。
func TestMailProviderReady(t *testing.T) {
	smtp := SMTPConfig{
		Enabled: true, Provider: MailProviderSMTP,
		Host: "smtp.gmail.com", Port: 465, Username: "a@gmail.com", Password: "pw",
	}
	if !smtp.Ready() {
		t.Fatal("SMTP 配置完整时应就绪")
	}
	smtp.Password = ""
	if smtp.Ready() {
		t.Fatal("SMTP 缺密码不应就绪")
	}

	resend := SMTPConfig{
		Enabled: true, Provider: MailProviderResend,
		ResendDomain: "opcuu.com", ResendAPIKey: "re_abc",
	}
	if !resend.Ready() {
		t.Fatal("Resend 只需域名 + Key，应就绪（不需要邮箱密码）")
	}
	resend.ResendAPIKey = ""
	if resend.Ready() {
		t.Fatal("Resend 缺 Key 不应就绪")
	}
	resend.ResendAPIKey = "re_abc"
	resend.ResendDomain = ""
	if resend.Ready() {
		t.Fatal("Resend 缺域名不应就绪")
	}
	resend.ResendDomain = "opcuu.com"
	resend.Enabled = false
	if resend.Ready() {
		t.Fatal("关闭后不应就绪")
	}
}

// TestMailProviderDailyLimit 每日封顶按通道区分。
func TestMailProviderDailyLimit(t *testing.T) {
	if got := (SMTPConfig{Provider: MailProviderSMTP}).DailyLimit(); got != RegisterDailyMailLimit {
		t.Fatalf("SMTP 上限应为 %d，实际 %d", RegisterDailyMailLimit, got)
	}
	if got := (SMTPConfig{Provider: MailProviderResend}).DailyLimit(); got != ResendDailyMailLimit {
		t.Fatalf("Resend 上限应为 %d，实际 %d", ResendDailyMailLimit, got)
	}
}

// TestGetSMTPConfigDefaultsProvider 老配置（没有 provider 字段）应回落到 SMTP，
// 避免升级后突然改用未配置的 Resend 而发不出验证码。
func TestGetSMTPConfigDefaultsProvider(t *testing.T) {
	if _, err := db.Open(); err != nil {
		t.Fatal(err)
	}
	if err := SaveSMTPConfig(SMTPConfig{
		Host: "smtp.example.com", Port: 465, Security: "ssl",
		Username: "u@example.com", Password: "pw", FromName: "Old", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	cfg := GetSMTPConfig()
	if cfg.Provider != MailProviderSMTP {
		t.Fatalf("历史配置应默认走 SMTP，实际 %q", cfg.Provider)
	}
	if !cfg.Ready() {
		t.Fatal("历史配置应仍可用")
	}
}
