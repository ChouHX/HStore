package mail

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestResendFromAddress(t *testing.T) {
	cases := []struct {
		domain, name, want string
	}{
		{"opcuu.com", "HShop", "HShop <noreply@opcuu.com>"},
		{"opcuu.com", "", "noreply@opcuu.com"},
		// 直接填完整地址时原样使用，不再拼 noreply@
		{"billing@opcuu.com", "HShop", "HShop <billing@opcuu.com>"},
		// 显示名里的尖括号 / 引号 / 换行必须清掉，否则破坏 From 头
		{`opcuu.com`, `A<b>"c"`, "Abc <noreply@opcuu.com>"},
		{"opcuu.com", "A\r\nB", "AB <noreply@opcuu.com>"},
		{"  opcuu.com  ", "  HShop  ", "HShop <noreply@opcuu.com>"},
	}
	for _, c := range cases {
		if got := ResendFromAddress(c.domain, c.name); got != c.want {
			t.Errorf("ResendFromAddress(%q,%q) = %q, want %q", c.domain, c.name, got, c.want)
		}
	}
}

func TestResendSendValidatesConfig(t *testing.T) {
	if err := ResendSend(context.Background(), "", "opcuu.com", "X", "a@b.com", "s", "t", ""); err == nil {
		t.Fatal("缺少 API Key 应报错")
	}
	if err := ResendSend(context.Background(), "re_x", "", "X", "a@b.com", "s", "t", ""); err == nil {
		t.Fatal("缺少域名应报错")
	}
	if err := ResendSend(context.Background(), "re_x", "opcuu.com", "X", "", "s", "t", ""); err == nil {
		t.Fatal("缺少收件人应报错")
	}
}

// TestResendClientIgnoresProxy 锁定「Resend 必须直连」：容器里的
// HTTPS_PROXY 只放行天启域名，发信走代理会被关闭。
func TestResendClientIgnoresProxy(t *testing.T) {
	tr, ok := resendClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("resendClient.Transport 类型异常: %T", resendClient.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("resendClient 必须直连：Proxy 应为 nil")
	}
}

func TestIsResendQuota(t *testing.T) {
	if IsResendQuota(errors.New("boom")) {
		t.Fatal("普通错误不应判定为配额问题")
	}
	if !IsResendQuota(&ResendError{StatusCode: 429, Message: "slow down"}) {
		t.Fatal("429 应判定为限流")
	}
	if !IsResendQuota(&ResendError{StatusCode: 403, Name: "daily_quota_exceeded"}) {
		t.Fatal("配额错误应被识别")
	}
	// SMTP 的配额判断仍然有效
	if !IsQuotaError(errors.New("5.4.5 Daily user sending quota exceeded")) {
		t.Fatal("SMTP 配额错误应被识别")
	}
	if IsQuotaError(errors.New("connection refused")) {
		t.Fatal("普通错误不应判定为配额问题")
	}
}

func TestResendErrorMessage(t *testing.T) {
	e := &ResendError{StatusCode: 422, Name: "validation_error", Message: "Invalid `from` field"}
	if !strings.Contains(e.Error(), "Invalid") {
		t.Fatalf("错误信息应包含服务端说明: %s", e.Error())
	}
	empty := &ResendError{StatusCode: 500}
	if !strings.Contains(empty.Error(), "500") {
		t.Fatalf("无 message 时应带状态码: %s", empty.Error())
	}
}
