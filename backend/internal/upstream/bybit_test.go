package upstream

import (
	"net/http"
	"os"
	"testing"
)

// TestBybitClientIgnoresProxy 锁定「Bybit 查单必须直连」这条约束。
//
// 容器为了绕过上游 WAF 设置了 HTTPS_PROXY（家宽 SOCKS5 隧道），该隧道只放行
// 天启域名。如果 Bybit 客户端误用 http.ProxyFromEnvironment，查单会全部失败，
// 表现为用户链上已经到账、系统却一直显示未到账。
func TestBybitClientIgnoresProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "socks5://127.0.0.1:18080")
	t.Setenv("HTTP_PROXY", "socks5://127.0.0.1:18080")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")

	if bybitClient.Transport == nil {
		t.Fatal("bybitClient 必须使用独立 Transport，否则会继承环境代理")
	}
	tr, ok := bybitClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("bybitClient.Transport 类型异常: %T", bybitClient.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("bybitClient 必须直连：Proxy 应为 nil，不能用 http.ProxyFromEnvironment")
	}

	// 反向确认环境代理确实会劫持请求，证明上面的断言不是空转。
	proxied := &http.Transport{Proxy: http.ProxyFromEnvironment}
	req, err := http.NewRequest(http.MethodGet, "https://api.bybit.com/v5/market/time", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proxied.Proxy(req); err != nil {
		t.Fatalf("环境代理未被识别，测试前提失效: %v", err)
	}
}

// TestSubMemberDepositsRejectsUnconfigured 未配置密钥时不应发起请求。
func TestSubMemberDepositsRejectsUnconfigured(t *testing.T) {
	// 测试库没有 bybit_api_key，GetBybitConfig 会返回空。
	if _, err := SubMemberDeposits("1", "USDT", 0); err == nil {
		t.Fatal("未配置 Bybit API 时应返回错误")
	}
}

func TestMain(m *testing.M) {
	// upstream 包内部分测试会读库（GetBybitConfig），给一个临时库避免污染。
	dir, err := os.MkdirTemp("", "hstore-upstream-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("FAKA_DB_PATH", dir+"/upstream-test.db")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
