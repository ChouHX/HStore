package upstream

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// ProxyStatus 描述容器访问上游时使用的出口代理（家宽 SOCKS5 隧道）状态。
//
// 上游 WAF 按出口 IP 拦截，生产环境依赖家宽设备反向隧道出网；
// 家宽断电/断网时隧道端口会消失，必须让管理员一眼看到代理是否还活着。
type ProxyStatus struct {
	Configured bool   `json:"configured"`
	Address    string `json:"address"`
	Protocol   string `json:"protocol"`
	Reachable  bool   `json:"reachable"`
	LatencyMs  int64  `json:"latencyMs"`
	Error      string `json:"error,omitempty"`
}

// UpstreamProbe 是一次真实的上游连通性探测结果。
type UpstreamProbe struct {
	Checked   bool   `json:"checked"`
	OK        bool   `json:"ok"`
	Blocked   bool   `json:"blocked"`
	LatencyMs int64  `json:"latencyMs"`
	Message   string `json:"message,omitempty"`
}

// proxyEnv 读取容器实际生效的出口代理，顺序与 net/http 一致。
func proxyEnv() string {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// CheckProxy 只检查代理端口是否可用：TCP 连接 + SOCKS5 握手。
// 不依赖上游配置，管理员打开后台时也能快速看到隧道状态。
func CheckProxy() ProxyStatus {
	raw := proxyEnv()
	if raw == "" {
		return ProxyStatus{}
	}
	st := ProxyStatus{Configured: true, Address: raw, Protocol: "unknown"}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		// 兼容 "host:port" 这种不带 scheme 的写法
		parsed = &url.URL{Scheme: "socks5", Host: raw}
	}
	st.Protocol = strings.ToLower(parsed.Scheme)
	// 不返回 user:pass，避免后台页面泄露代理凭据
	st.Address = parsed.Host
	if parsed.Port() == "" {
		port := "1080"
		if st.Protocol == "http" || st.Protocol == "https" {
			port = "8080"
		}
		st.Address = net.JoinHostPort(parsed.Hostname(), port)
	}

	start := time.Now()
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).Dial("tcp", st.Address)
	if err != nil {
		st.Error = "无法连接代理：" + err.Error()
		st.LatencyMs = time.Since(start).Milliseconds()
		return st
	}
	defer conn.Close()

	if st.Protocol == "socks5" || st.Protocol == "socks5h" {
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		// SOCKS5 greeting：VER=5, NMETHODS=1, METHOD=0（无认证）
		if _, err = conn.Write([]byte{0x05, 0x01, 0x00}); err == nil {
			reply := make([]byte, 2)
			if _, err = conn.Read(reply); err == nil {
				if reply[0] != 0x05 || reply[1] == 0xFF {
					st.Error = "代理端口可连接，但 SOCKS5 握手失败（可能需要认证）"
					st.LatencyMs = time.Since(start).Milliseconds()
					return st
				}
			}
		}
		if err != nil {
			st.Error = "SOCKS5 握手失败：" + err.Error()
			st.LatencyMs = time.Since(start).Milliseconds()
			return st
		}
	}

	st.Reachable = true
	st.LatencyMs = time.Since(start).Milliseconds()
	return st
}

// ProbeUpstream 走真实接口验证「代理 → 上游 WAF → 业务接口」整条链路。
// 只在管理员主动刷新时调用，避免把 WAF 挑战刷新打成风暴。
func ProbeUpstream() UpstreamProbe {
	if !IsConfigured() {
		return UpstreamProbe{Checked: false, Message: "未配置上游客户编号 / API 密钥"}
	}
	start := time.Now()
	_, err := GetDirs()
	probe := UpstreamProbe{Checked: true, LatencyMs: time.Since(start).Milliseconds()}
	if err == nil || IsEmptyDataError(err) {
		probe.OK = true
		return probe
	}
	probe.Blocked = isWafBlocked(err)
	probe.Message = err.Error()
	return probe
}

func isWafBlocked(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	msg := apiErr.Message
	for _, marker := range []string{"WAF", "非 JSON", "人机校验", "ESA"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
