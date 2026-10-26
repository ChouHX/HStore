package upstream

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const wafUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var (
	scriptPattern = regexp.MustCompile(`(?is)<script[^>]*>(.*?)</script>`)
)

// jsDocument 是给挑战脚本用的最小 document 垫片。
type jsDocument struct {
	Cookie string
}

func (d *jsDocument) CreateElement(tag string) map[string]any {
	return map[string]any{"style": map[string]any{}, "tagName": tag}
}
func (d *jsDocument) GetElementsByTagName(tag string) []any { return []any{} }
func (d *jsDocument) AddEventListener(args ...any)          {}
func (d *jsDocument) SetAttribute(args ...any)              {}

type jsLocation struct {
	Href     string
	Host     string
	Hostname string
	Protocol string
	Pathname string
	Search   string
	Hash     string
}

func (l *jsLocation) Reload()  {}
func (l *jsLocation) Replace() {}
func (l *jsLocation) Assign()  {}

type jsNavigator struct {
	UserAgent     string
	Platform      string
	Language      string
	Languages     []string
	CookieEnabled bool
}

type jsStorage struct{}

func (s *jsStorage) GetItem(string) any { return nil }
func (s *jsStorage) SetItem(...any)     {}
func (s *jsStorage) RemoveItem(...any)  {}
func (s *jsStorage) Clear()             {}

// RefreshWafCookie 执行站点 JS 挑战，拿到放行 Cookie（无需浏览器）。
func RefreshWafCookie(baseURL string) wafResult {
	root := strings.TrimRight(baseURL, "/")
	req, e := http.NewRequest(http.MethodGet, root+"/", nil)
	if e != nil {
		return wafResult{Message: "网络错误，无法访问站点"}
	}
	req.Header.Set("User-Agent", wafUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	res, e := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if e != nil {
		return wafResult{Message: "网络错误，无法访问站点"}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	text := string(raw)

	jar := map[string]string{}
	for _, c := range res.Header.Values("Set-Cookie") {
		kv := strings.SplitN(c, ";", 2)[0]
		if i := strings.Index(kv, "="); i > 0 {
			jar[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
		}
	}

	matches := scriptPattern.FindAllStringSubmatch(text, -1)
	var scripts []string
	for _, m := range matches {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			scripts = append(scripts, m[1])
		}
	}

	joinCookie := func() string {
		var parts []string
		for k, v := range jar {
			if k == "" || v == "" {
				continue
			}
			parts = append(parts, k+"="+v)
		}
		return strings.Join(parts, "; ")
	}

	if len(scripts) == 0 || !strings.Contains(text, "arg1") {
		cookie := joinCookie()
		if cookie != "" {
			return wafResult{OK: true, Cookie: cookie}
		}
		return wafResult{Message: "站点未返回挑战，也未能获取 Cookie"}
	}

	u, _ := url.Parse(root + "/")
	doc := &jsDocument{}
	loc := &jsLocation{Href: root + "/", Protocol: "https:", Pathname: "/"}
	if u != nil {
		loc.Host = u.Host
		loc.Hostname = u.Hostname()
		loc.Protocol = u.Scheme + ":"
	}

	vm := goja.New()
	_ = vm.Set("document", doc)
	_ = vm.Set("location", loc)
	_ = vm.Set("navigator", &jsNavigator{
		UserAgent: wafUserAgent, Platform: "Linux x86_64",
		Language: "zh-CN", Languages: []string{"zh-CN", "zh"}, CookieEnabled: true,
	})
	_ = vm.Set("screen", map[string]any{"width": 1920, "height": 1080})
	_ = vm.Set("history", map[string]any{"pushState": func(...any) {}, "replaceState": func(...any) {}})
	_ = vm.Set("localStorage", &jsStorage{})
	_ = vm.Set("sessionStorage", &jsStorage{})
	_ = vm.Set("atob", func(s string) string { return string(mustBase64(s)) })
	_ = vm.Set("btoa", func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) })
	_ = vm.Set("setTimeout", func(call goja.FunctionCall) goja.Value {
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			_, _ = fn(goja.Undefined())
		}
		return goja.Undefined()
	})
	_ = vm.Set("setInterval", func(call goja.FunctionCall) goja.Value { return goja.Undefined() })
	_ = vm.Set("clearTimeout", func(...any) {})
	_ = vm.Set("clearInterval", func(...any) {})
	global := vm.GlobalObject()
	_ = vm.Set("window", global)
	_ = vm.Set("self", global)
	_ = vm.Set("top", global)
	_ = vm.Set("parent", global)
	_ = vm.Set("frames", global)
	_ = vm.Set("console", map[string]any{
		"log": func(...any) {}, "warn": func(...any) {}, "error": func(...any) {}, "info": func(...any) {},
	})

	for _, s := range scripts {
		// 脚本末尾通常会调用 location.reload()，垫片不会抛错；其它异常忽略
		_, _ = vm.RunString(s)
	}

	if doc.Cookie != "" {
		first := strings.SplitN(doc.Cookie, ";", 2)[0]
		if i := strings.Index(first, "="); i > 0 {
			jar[strings.TrimSpace(first[:i])] = strings.TrimSpace(first[i+1:])
		}
	}
	for _, k := range []string{"expires", "path", "max-age", "domain"} {
		delete(jar, k)
	}

	cookie := joinCookie()
	if cookie == "" {
		return wafResult{Message: "挑战脚本未产生 Cookie"}
	}
	return wafResult{OK: true, Cookie: cookie}
}

func mustBase64(s string) []byte {
	out, e := base64.StdEncoding.DecodeString(s)
	if e != nil {
		return nil
	}
	return out
}
