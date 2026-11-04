package httpapi

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// 匿名取件限流：同一 IP 在窗口内允许的调用次数。
// 取件本身会连 IMAP，很慢，正常用户不会高频调用；这里只防脚本批量试探兑换码。
const (
	pickupWindow    = time.Minute
	pickupMaxPerIP  = 12
	pickupSweepEach = 512
)

type ipBucket struct {
	used  int
	reset time.Time
}

var (
	pickupMu      sync.Mutex
	pickupBuckets = map[string]*ipBucket{}
	pickupCalls   int
)

// allowPickup 返回该 IP 是否还有配额，并消耗一次。
func allowPickup(ip string) bool {
	if ip == "" {
		ip = "-"
	}
	now := time.Now()
	pickupMu.Lock()
	defer pickupMu.Unlock()

	pickupCalls++
	if pickupCalls%pickupSweepEach == 0 {
		for k, b := range pickupBuckets {
			if now.After(b.reset) {
				delete(pickupBuckets, k)
			}
		}
	}

	b := pickupBuckets[ip]
	if b == nil || now.After(b.reset) {
		pickupBuckets[ip] = &ipBucket{used: 1, reset: now.Add(pickupWindow)}
		return true
	}
	if b.used >= pickupMaxPerIP {
		return false
	}
	b.used++
	return true
}

// maxIPLen 限制来源 IP 的字符串长度：太长的头一律当非法值丢弃，
// 避免攻击者用超长头把限流 map 的 key 撑爆（内存放大）。
const maxIPLen = 64

// trustCloudflare 表示部署环境里 Go 服务前面是否还有一层 Cloudflare。
// 当前站点是 1Panel openresty 直接反代，默认关闭：此时 CF-Connecting-IP
// 完全由客户端自带，绝不可信。真上了 Cloudflare 再设置 FAKA_TRUST_CF_IP=1 打开。
var trustCloudflare = func() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FAKA_TRUST_CF_IP"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}()

// clientIP 取真实来源 IP，只信任边缘代理「覆盖写」的头。
//
// 部署事实（1Panel openresty 反代到 127.0.0.1:3001）：
//   - X-Real-IP 被边缘覆盖为 $remote_addr，是真实对端，可信；
//   - X-Forwarded-For 被边缘用 $proxy_add_x_forwarded_for 追加，
//     即「客户端自带的内容 + 真实 IP」，最左侧可任意伪造，不可信；
//   - CF-Connecting-IP 只在明确开启 Cloudflare 时才存在且可信。
//
// 因此顺序是 X-Real-IP → （开启时才看）CF-Connecting-IP → RemoteAddr。
// 绝不取 X-Forwarded-For：宁可退化成反代地址（限流更严），
// 也不能让攻击者自带一个假 XFF 就把限流全部绕开。
func clientIP(r *http.Request) string {
	if ip := sanitizeIP(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if trustCloudflare {
		if ip := sanitizeIP(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
	}
	return remoteHost(r)
}

// sanitizeIP 校验并规整代理头里的 IP；非法值（含域名、超长串、空串）返回空。
func sanitizeIP(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > maxIPLen {
		return ""
	}
	// 允许 host:port 形式，取 host 部分
	if host, _, e := net.SplitHostPort(v); e == nil {
		v = host
	}
	ip := net.ParseIP(v)
	if ip == nil {
		return ""
	}
	return ip.String()
}

// remoteHost 取 TCP 对端地址（反代场景下是 127.0.0.1）。
func remoteHost(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		if len(r.RemoteAddr) > maxIPLen {
			return r.RemoteAddr[:maxIPLen]
		}
		return r.RemoteAddr
	}
	return host
}

// 游客下单限流：防止匿名脚本批量创建临时用户和支付订单。
const guestCreateWindow = time.Minute
const guestCreateMaxPerIP = 8

var (
	guestMu      sync.Mutex
	guestBuckets = map[string]*ipBucket{}
	guestCalls   int
)

func allowGuestCreate(ip string) bool {
	if ip == "" {
		ip = "-"
	}
	now := time.Now()
	guestMu.Lock()
	defer guestMu.Unlock()
	guestCalls++
	if guestCalls%pickupSweepEach == 0 {
		for k, b := range guestBuckets {
			if now.After(b.reset) {
				delete(guestBuckets, k)
			}
		}
	}
	b := guestBuckets[ip]
	if b == nil || now.After(b.reset) {
		guestBuckets[ip] = &ipBucket{used: 1, reset: now.Add(guestCreateWindow)}
		return true
	}
	if b.used >= guestCreateMaxPerIP {
		return false
	}
	b.used++
	return true
}

// 订单查询限流：比取件宽松（翻页、重复搜索很常见），但足以挡住脚本枚举。
const (
	queryWindow   = time.Minute
	queryMaxPerIP = 60
)

var (
	queryMu      sync.Mutex
	queryBuckets = map[string]*ipBucket{}
	queryCalls   int
)

func allowQuery(ip string) bool {
	if ip == "" {
		ip = "-"
	}
	now := time.Now()
	queryMu.Lock()
	defer queryMu.Unlock()

	queryCalls++
	if queryCalls%pickupSweepEach == 0 {
		for k, b := range queryBuckets {
			if now.After(b.reset) {
				delete(queryBuckets, k)
			}
		}
	}
	b := queryBuckets[ip]
	if b == nil || now.After(b.reset) {
		queryBuckets[ip] = &ipBucket{used: 1, reset: now.Add(queryWindow)}
		return true
	}
	if b.used >= queryMaxPerIP {
		return false
	}
	b.used++
	return true
}

// 注册验证码限流：防止脚本批量发信耗尽当天配额。
const (
	registerWindow   = time.Minute
	registerMaxPerIP = 5
)

var (
	registerMu      sync.Mutex
	registerBuckets = map[string]*ipBucket{}
	registerCalls   int
)

func allowRegisterCode(ip string) bool {
	if ip == "" {
		ip = "-"
	}
	now := time.Now()
	registerMu.Lock()
	defer registerMu.Unlock()
	registerCalls++
	if registerCalls%pickupSweepEach == 0 {
		for k, b := range registerBuckets {
			if now.After(b.reset) {
				delete(registerBuckets, k)
			}
		}
	}
	b := registerBuckets[ip]
	if b == nil || now.After(b.reset) {
		registerBuckets[ip] = &ipBucket{used: 1, reset: now.Add(registerWindow)}
		return true
	}
	if b.used >= registerMaxPerIP {
		return false
	}
	b.used++
	return true
}

// ===== 登录防护 =====
//
// 两道闸门，都在 scrypt 校验之前执行（先拦再算，避免 CPU 放大攻击）：
//  1. 按 IP 的登录频率限制；
//  2. 按账号的失败计数锁定。
//
// 注意：锁定提示对所有账号统一措辞，不区分账号是否存在。

const (
	loginWindow   = time.Minute
	loginMaxPerIP = 10 // 同一 IP 每分钟最多 10 次登录尝试

	loginFailWindow = 15 * time.Minute // 失败计数统计窗口
	loginFailMax    = 5                // 窗口内失败 5 次即锁定
	loginLockFor    = 15 * time.Minute // 锁定时长
)

var (
	loginMu      sync.Mutex
	loginBuckets = map[string]*ipBucket{}
	loginCalls   int

	loginFailMu sync.Mutex
	loginFails  = map[string]*loginFailBucket{}
	loginFailAt int
)

// loginFailBucket 记录某个账号（或 IP）在窗口内的失败次数与锁定截止时间。
type loginFailBucket struct {
	count int
	first time.Time
	until time.Time
}

// allowLogin 按 IP 限流，返回是否放行。
func allowLogin(ip string) bool {
	if ip == "" {
		ip = "-"
	}
	now := time.Now()
	loginMu.Lock()
	defer loginMu.Unlock()

	loginCalls++
	if loginCalls%pickupSweepEach == 0 {
		for k, b := range loginBuckets {
			if now.After(b.reset) {
				delete(loginBuckets, k)
			}
		}
	}
	b := loginBuckets[ip]
	if b == nil || now.After(b.reset) {
		loginBuckets[ip] = &ipBucket{used: 1, reset: now.Add(loginWindow)}
		return true
	}
	if b.used >= loginMaxPerIP {
		return false
	}
	b.used++
	return true
}

// loginLocked 判断该账号当前是否处于失败锁定中（只读，不计数）。
func loginLocked(account string) bool {
	account = strings.ToLower(strings.TrimSpace(account))
	if account == "" {
		return false
	}
	now := time.Now()
	loginFailMu.Lock()
	defer loginFailMu.Unlock()
	b := loginFails[account]
	return b != nil && now.Before(b.until)
}

// noteLoginFailure 记录一次失败；返回该账号是否因此被锁定。
func noteLoginFailure(account string) bool {
	account = strings.ToLower(strings.TrimSpace(account))
	if account == "" {
		return false
	}
	now := time.Now()
	loginFailMu.Lock()
	defer loginFailMu.Unlock()

	// 顺手清理过期条目，避免 map 无限增长
	loginFailAt++
	if loginFailAt%pickupSweepEach == 0 {
		for k, b := range loginFails {
			if now.After(b.until) && now.Sub(b.first) > loginFailWindow {
				delete(loginFails, k)
			}
		}
	}

	b := loginFails[account]
	if b == nil || now.Sub(b.first) > loginFailWindow {
		b = &loginFailBucket{first: now}
		loginFails[account] = b
	}
	b.count++
	if b.count >= loginFailMax {
		b.until = now.Add(loginLockFor)
		return true
	}
	return false
}

// clearLoginFailures 登录成功后清空该账号的失败计数。
func clearLoginFailures(account string) {
	account = strings.ToLower(strings.TrimSpace(account))
	if account == "" {
		return
	}
	loginFailMu.Lock()
	delete(loginFails, account)
	loginFailMu.Unlock()
}
