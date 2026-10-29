package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// resendEndpoint 是 Resend 发信接口。
const resendEndpoint = "https://api.resend.com/emails"

// resendClient 强制直连，不走 HTTP(S)_PROXY。
//
// 容器为绕过上游 WAF 配了 HTTPS_PROXY 指向家宽隧道，该隧道只放行天启域名；
// 发信走代理会被直接关闭。与 bybitClient 同理，这里显式禁用代理。
var resendClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		Proxy:               nil,
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	},
}

// ResendError 是 Resend 返回的业务错误。
type ResendError struct {
	StatusCode int
	Name       string
	Message    string
}

func (e *ResendError) Error() string {
	if e.Message != "" {
		return "Resend: " + e.Message
	}
	return fmt.Sprintf("Resend 请求失败（HTTP %d）", e.StatusCode)
}

// resendRequest 是发信请求体。
type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Html    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
	ReplyTo string   `json:"reply_to,omitempty"`
}

// ResendSend 通过 Resend API 发信。
//
// domain 是已在 Resend 控制台验证过的域名（如 opcuu.com），
// fromName 是发件人显示名；正文同时提供 text 与 html 以提升送达率。
func ResendSend(ctx context.Context, apiKey, domain, fromName, to, subject, text, html string) error {
	return ResendSendContext(ctx, apiKey, domain, fromName, to, subject, text, html)
}

// ResendSendContext 是 ResendSend 的可取消版本。
func ResendSendContext(ctx context.Context, apiKey, domain, fromName, to, subject, text, html string) error {
	apiKey = strings.TrimSpace(apiKey)
	domain = strings.TrimSpace(domain)
	if apiKey == "" {
		return errors.New("Resend API Key 未配置")
	}
	if domain == "" {
		return errors.New("Resend 发信域名未配置")
	}
	if to == "" {
		return errors.New("收件人为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	from := ResendFromAddress(domain, fromName)
	payload := resendRequest{
		From:    from,
		To:      []string{to},
		Subject: subject,
		Html:    html,
		Text:    text,
		ReplyTo: from,
	}
	// 纯文本邮件：没有 html 时只发 text，避免发空 html。
	if strings.TrimSpace(html) == "" {
		payload.Html = ""
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendEndpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := resendClient.Do(req)
	if err != nil {
		return fmt.Errorf("连接 Resend 失败：%v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	var apiErr struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &apiErr)
	msg := apiErr.Message
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return &ResendError{StatusCode: res.StatusCode, Name: apiErr.Name, Message: msg}
}

// ResendFromAddress 组装 From 头：有显示名时用 `名称 <noreply@domain>`。
func ResendFromAddress(domain, fromName string) string {
	domain = strings.TrimSpace(domain)
	fromName = strings.TrimSpace(fromName)
	// 域名配置允许直接填完整地址（user@domain），此时原样使用。
	addr := domain
	if !strings.Contains(domain, "@") {
		addr = "noreply@" + domain
	}
	if fromName == "" {
		return addr
	}
	// 显示名里带尖括号或引号会破坏 From 头，去掉。
	clean := strings.NewReplacer("<", "", ">", "", "\"", "", "\r", "", "\n", "").Replace(fromName)
	return fmt.Sprintf("%s <%s>", clean, addr)
}

// IsResendQuota 判断是否为 Resend 配额 / 限流错误。
func IsResendQuota(err error) bool {
	var re *ResendError
	if !errors.As(err, &re) {
		return false
	}
	if re.StatusCode == http.StatusTooManyRequests {
		return true
	}
	msg := strings.ToLower(re.Name + " " + re.Message)
	return strings.Contains(msg, "quota") || strings.Contains(msg, "rate_limit") ||
		strings.Contains(msg, "daily_limit") || strings.Contains(msg, "too many")
}
