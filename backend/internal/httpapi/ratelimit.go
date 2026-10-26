package httpapi

import (
	"net"
	"net/http"
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

// clientIP 取真实来源 IP，优先代理头。
func clientIP(r *http.Request) string {
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// 取最左侧的客户端地址
		if i := strings.Index(v, ","); i > 0 {
			v = v[:i]
		}
		return strings.TrimSpace(v)
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
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
