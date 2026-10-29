package mail

import (
	"context"

	"hstore/backend/internal/store"
)

// SendOptions 是统一发信的入参。
type SendOptions struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Send 按后台配置的通道（smtp / resend）发信。
//
// 调用方只需要关心「发一封信」，不必区分 SMTP 还是 Resend；
// 这样注册验证码等业务在切换通道时无需改代码。
func Send(ctx context.Context, cfg store.SMTPConfig, opt SendOptions) error {
	if cfg.Provider == store.MailProviderResend {
		return ResendSendContext(ctx, cfg.ResendAPIKey, cfg.ResendDomain, cfg.FromName,
			opt.To, opt.Subject, opt.Text, opt.HTML)
	}
	return SMTPSendRich(cfg.Host, cfg.Port, cfg.Security, cfg.Username, cfg.Password,
		cfg.FromName, opt.To, opt.Subject, opt.Text, opt.HTML)
}

// IsQuotaError 判断错误是否为发信配额 / 限流（两种通道统一判断）。
func IsQuotaError(err error) bool {
	return IsSMTPDailyLimit(err) || IsResendQuota(err)
}
