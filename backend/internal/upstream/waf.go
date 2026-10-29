package upstream

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const wafUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var (
	scriptPattern = regexp.MustCompile(`(?is)<script[^>]*>(.*?)</script>`)
	// 阿里云 ESA 滑块验证码标志：由服务端签名（u_asig），无法通过本地 JS 计算绕过。
	esaCaptchaPattern = regexp.MustCompile(`(?i)initAliyunCaptcha|aliyunCaptcha-sliding|captcha-element`)
)

// blockedByESA 判断响应是否为阿里云 ESA 的自定义规则拦截或滑块验证页。
// 这类页面返回 HTTP 200，但内容是 HTML 而非 JSON，容易被误判为“挑战成功”。
func blockedByESA(headers http.Header, body string) bool {
	if strings.Contains(strings.ToLower(headers.Get("X-Tengine-Error")), "denied") {
		return true
	}
	return esaCaptchaPattern.MatchString(body)
}

// wafResult 是一次挑战执行结果。
type wafResult struct {
	OK      bool
	Cookie  string
	Message string
}

// cookieJar 保存挑战页 Set-Cookie 和脚本写入的 document.cookie。
type cookieJar struct {
	values  map[string]string
	changed bool
}

func newCookieJar() *cookieJar {
	return &cookieJar{values: map[string]string{}}
}

func (j *cookieJar) set(raw string) {
	if j.parse(raw) {
		j.changed = true
	}
}

func (j *cookieJar) addHeader(raw string) {
	_ = j.parse(raw)
}

func (j *cookieJar) parse(raw string) bool {
	changed := false
	for _, part := range strings.Split(raw, ";") {
		kv := strings.TrimSpace(part)
		if kv == "" {
			continue
		}
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(kv[:idx])
		value := strings.TrimSpace(kv[idx+1:])
		switch strings.ToLower(key) {
		case "path", "domain", "expires", "max-age", "samesite", "secure", "httponly", "priority":
			continue
		}
		if j.values[key] != value {
			j.values[key] = value
			changed = true
		}
	}
	return changed
}

func (j *cookieJar) String() string {
	if len(j.values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(j.values))
	for key := range j.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if j.values[key] == "" {
			continue
		}
		parts = append(parts, key+"="+j.values[key])
	}
	return strings.Join(parts, "; ")
}

func noopValue(vm *goja.Runtime) goja.Value {
	return vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() })
}

// newBrowserEnv 构造最小但足够执行常见阿里云 WAF 挑战的浏览器环境。
func newBrowserEnv(vm *goja.Runtime, jar *cookieJar, root string) func() {
	global := vm.GlobalObject()
	callbacks := []goja.Callable{}

	addListener := func(call goja.FunctionCall) goja.Value {
		eventType := strings.ToLower(call.Argument(0).String())
		if fn, ok := goja.AssertFunction(call.Argument(1)); ok {
			switch eventType {
			case "load", "domcontentloaded", "readystatechange":
				callbacks = append(callbacks, fn)
			}
		}
		return goja.Undefined()
	}
	removeListener := noopValue(vm)
	noop := noopValue(vm)
	undefined := goja.Undefined()

	u, _ := url.Parse(strings.TrimRight(root, "/") + "/")
	loc := vm.NewObject()
	_ = loc.Set("href", u.String())
	_ = loc.Set("protocol", u.Scheme+":")
	_ = loc.Set("host", u.Host)
	_ = loc.Set("hostname", u.Hostname())
	_ = loc.Set("port", u.Port())
	_ = loc.Set("pathname", u.Path)
	_ = loc.Set("search", u.RawQuery)
	_ = loc.Set("hash", u.Fragment)
	_ = loc.Set("origin", u.Scheme+"://"+u.Host)
	_ = loc.Set("reload", noop)
	_ = loc.Set("replace", noop)
	_ = loc.Set("assign", noop)
	_ = loc.Set("toString", func(goja.FunctionCall) goja.Value { return vm.ToValue(u.String()) })

	storageFactory := func() *goja.Object {
		store := map[string]string{}
		obj := vm.NewObject()
		_ = obj.Set("getItem", func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(store[call.Argument(0).String()])
		})
		_ = obj.Set("setItem", func(call goja.FunctionCall) goja.Value {
			store[call.Argument(0).String()] = call.Argument(1).String()
			return undefined
		})
		_ = obj.Set("removeItem", func(call goja.FunctionCall) goja.Value {
			delete(store, call.Argument(0).String())
			return undefined
		})
		_ = obj.Set("clear", func(goja.FunctionCall) goja.Value {
			store = map[string]string{}
			return undefined
		})
		_ = obj.Set("key", func(call goja.FunctionCall) goja.Value {
			keys := make([]string, 0, len(store))
			for key := range store {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			idx := int(call.Argument(0).ToInteger())
			if idx < 0 || idx >= len(keys) {
				return goja.Null()
			}
			return vm.ToValue(keys[idx])
		})
		return obj
	}

	styleFactory := func() *goja.Object {
		style := vm.NewObject()
		_ = style.Set("setProperty", noop)
		_ = style.Set("removeProperty", noop)
		_ = style.Set("getPropertyValue", func(goja.FunctionCall) goja.Value { return vm.ToValue("") })
		return style
	}

	canvasContextFactory := func() *goja.Object {
		ctx := vm.NewObject()
		for _, name := range []string{
			"save", "restore", "beginPath", "closePath", "moveTo", "lineTo", "arc", "arcTo",
			"rect", "fill", "stroke", "fillRect", "strokeRect", "clearRect", "fillText",
			"strokeText", "translate", "rotate", "scale", "transform", "setTransform",
			"clip", "drawImage", "putImageData", "createImageData", "createPattern",
			"setLineDash", "quadraticCurveTo", "bezierCurveTo", "ellipse",
		} {
			_ = ctx.Set(name, noop)
		}
		_ = ctx.Set("measureText", func(goja.FunctionCall) goja.Value {
			out := vm.NewObject()
			_ = out.Set("width", 10)
			return out
		})
		_ = ctx.Set("getImageData", func(call goja.FunctionCall) goja.Value {
			out := vm.NewObject()
			_ = out.Set("width", 1)
			_ = out.Set("height", 1)
			_ = out.Set("data", vm.NewArray(0, 0, 0, 0))
			return out
		})
		_ = ctx.Set("createLinearGradient", func(goja.FunctionCall) goja.Value {
			out := vm.NewObject()
			_ = out.Set("addColorStop", noop)
			return out
		})
		_ = ctx.Set("getParameter", func(call goja.FunctionCall) goja.Value {
			switch strings.ToUpper(call.Argument(0).String()) {
			case "VENDOR":
				return vm.ToValue("Google Inc.")
			case "RENDERER":
				return vm.ToValue("ANGLE (Google, Vulkan 1.3.0)")
			case "VERSION":
				return vm.ToValue("WebGL 1.0")
			default:
				return vm.ToValue("")
			}
		})
		_ = ctx.Set("getExtension", func(goja.FunctionCall) goja.Value { return goja.Null() })
		_ = ctx.Set("getSupportedExtensions", func(goja.FunctionCall) goja.Value { return vm.NewArray() })
		return ctx
	}

	var elementFactory func(string) *goja.Object
	elementFactory = func(tag string) *goja.Object {
		el := vm.NewObject()
		upper := strings.ToUpper(tag)
		_ = el.Set("tagName", upper)
		_ = el.Set("nodeName", upper)
		_ = el.Set("nodeType", 1)
		_ = el.Set("style", styleFactory())
		_ = el.Set("attributes", vm.NewObject())
		_ = el.Set("children", vm.NewArray())
		_ = el.Set("childNodes", vm.NewArray())
		_ = el.Set("parentNode", goja.Null())
		_ = el.Set("clientWidth", 1920)
		_ = el.Set("clientHeight", 1080)
		_ = el.Set("offsetWidth", 1920)
		_ = el.Set("offsetHeight", 1080)
		_ = el.Set("scrollWidth", 1920)
		_ = el.Set("scrollHeight", 1080)
		_ = el.Set("innerHTML", "")
		_ = el.Set("outerHTML", "")
		_ = el.Set("textContent", "")
		_ = el.Set("id", "")
		_ = el.Set("className", "")
		classList := vm.NewObject()
		_ = classList.Set("add", noop)
		_ = classList.Set("remove", noop)
		_ = classList.Set("toggle", noop)
		_ = classList.Set("contains", func(goja.FunctionCall) goja.Value { return vm.ToValue(false) })
		_ = el.Set("classList", classList)
		_ = el.Set("appendChild", func(call goja.FunctionCall) goja.Value { return call.Argument(0) })
		_ = el.Set("insertBefore", func(call goja.FunctionCall) goja.Value { return call.Argument(0) })
		_ = el.Set("replaceChild", func(call goja.FunctionCall) goja.Value { return call.Argument(1) })
		_ = el.Set("removeChild", func(call goja.FunctionCall) goja.Value { return call.Argument(0) })
		_ = el.Set("remove", noop)
		_ = el.Set("setAttribute", func(call goja.FunctionCall) goja.Value {
			_ = el.Set(call.Argument(0).String(), call.Argument(1).String())
			return undefined
		})
		_ = el.Set("getAttribute", func(call goja.FunctionCall) goja.Value {
			v := el.Get(call.Argument(0).String())
			if v == nil {
				return vm.ToValue("")
			}
			return v
		})
		_ = el.Set("removeAttribute", noop)
		_ = el.Set("hasAttribute", func(goja.FunctionCall) goja.Value { return vm.ToValue(false) })
		_ = el.Set("addEventListener", addListener)
		_ = el.Set("removeEventListener", removeListener)
		_ = el.Set("dispatchEvent", func(goja.FunctionCall) goja.Value { return vm.ToValue(true) })
		_ = el.Set("getBoundingClientRect", func(goja.FunctionCall) goja.Value {
			out := vm.NewObject()
			for _, key := range []string{"left", "top", "right", "bottom", "width", "height", "x", "y"} {
				_ = out.Set(key, 0)
			}
			return out
		})
		_ = el.Set("getContext", func(goja.FunctionCall) goja.Value { return canvasContextFactory() })
		_ = el.Set("toDataURL", func(goja.FunctionCall) goja.Value {
			return vm.ToValue("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
		})
		_ = el.Set("append", noop)
		_ = el.Set("prepend", noop)
		_ = el.Set("focus", noop)
		_ = el.Set("blur", noop)
		_ = el.Set("click", noop)
		_ = el.Set("querySelector", func(goja.FunctionCall) goja.Value { return goja.Null() })
		_ = el.Set("querySelectorAll", func(goja.FunctionCall) goja.Value { return vm.NewArray() })
		_ = el.Set("getElementsByTagName", func(goja.FunctionCall) goja.Value { return vm.NewArray() })
		_ = el.Set("cloneNode", func(goja.FunctionCall) goja.Value { return elementFactory(tag) })
		return el
	}

	doc := vm.NewObject()
	getCookie := vm.ToValue(func(goja.FunctionCall) goja.Value { return vm.ToValue(jar.String()) })
	setCookie := vm.ToValue(func(call goja.FunctionCall) goja.Value {
		jar.set(call.Argument(0).String())
		return undefined
	})
	_ = doc.DefineAccessorProperty("cookie", getCookie, setCookie, goja.FLAG_TRUE, goja.FLAG_TRUE)
	_ = doc.Set("createElement", func(call goja.FunctionCall) goja.Value {
		return elementFactory(call.Argument(0).String())
	})
	_ = doc.Set("createElementNS", func(call goja.FunctionCall) goja.Value {
		return elementFactory(call.Argument(1).String())
	})
	_ = doc.Set("createTextNode", func(call goja.FunctionCall) goja.Value {
		out := vm.NewObject()
		_ = out.Set("nodeType", 3)
		_ = out.Set("textContent", call.Argument(0).String())
		return out
	})
	_ = doc.Set("createDocumentFragment", func(goja.FunctionCall) goja.Value { return elementFactory("fragment") })
	htmlEl := elementFactory("html")
	headEl := elementFactory("head")
	bodyEl := elementFactory("body")
	scriptEl := elementFactory("script")
	_ = scriptEl.Set("src", "")
	_ = scriptEl.Set("type", "text/javascript")
	_ = scriptEl.Set("parentNode", headEl)
	_ = doc.Set("getElementsByTagName", func(call goja.FunctionCall) goja.Value {
		switch strings.ToLower(call.Argument(0).String()) {
		case "script":
			return vm.NewArray(scriptEl)
		case "html":
			return vm.NewArray(htmlEl)
		case "head":
			return vm.NewArray(headEl)
		case "body":
			return vm.NewArray(bodyEl)
		default:
			return vm.NewArray()
		}
	})
	_ = doc.Set("getElementsByClassName", func(goja.FunctionCall) goja.Value { return vm.NewArray() })
	_ = doc.Set("getElementById", func(goja.FunctionCall) goja.Value { return goja.Null() })
	_ = doc.Set("querySelector", func(call goja.FunctionCall) goja.Value {
		if strings.TrimSpace(call.Argument(0).String()) == "script" {
			return scriptEl
		}
		return goja.Null()
	})
	_ = doc.Set("querySelectorAll", func(call goja.FunctionCall) goja.Value {
		if strings.TrimSpace(call.Argument(0).String()) == "script" {
			return vm.NewArray(scriptEl)
		}
		return vm.NewArray()
	})
	_ = doc.Set("addEventListener", addListener)
	_ = doc.Set("removeEventListener", removeListener)
	_ = doc.Set("dispatchEvent", func(goja.FunctionCall) goja.Value { return vm.ToValue(true) })
	_ = doc.Set("write", noop)
	_ = doc.Set("writeln", noop)
	_ = doc.Set("createEvent", func(goja.FunctionCall) goja.Value {
		out := vm.NewObject()
		_ = out.Set("initEvent", noop)
		_ = out.Set("initCustomEvent", noop)
		_ = out.Set("preventDefault", noop)
		_ = out.Set("stopPropagation", noop)
		return out
	})
	_ = doc.Set("documentElement", htmlEl)
	_ = doc.Set("body", bodyEl)
	_ = doc.Set("head", headEl)
	_ = doc.Set("scripts", vm.NewArray(scriptEl))
	_ = doc.Set("currentScript", scriptEl)
	_ = doc.Set("charset", "UTF-8")
	_ = doc.Set("characterSet", "UTF-8")
	_ = doc.Set("contentType", "text/html")
	_ = doc.Set("readyState", "complete")
	_ = doc.Set("visibilityState", "visible")
	_ = doc.Set("hidden", false)
	_ = doc.Set("referrer", "")
	_ = doc.Set("URL", u.String())
	_ = doc.Set("domain", u.Hostname())
	_ = doc.Set("title", "")
	_ = doc.Set("defaultView", global)
	_ = doc.Set("location", loc)

	navigator := vm.NewObject()
	_ = navigator.Set("userAgent", wafUserAgent)
	_ = navigator.Set("appName", "Netscape")
	_ = navigator.Set("appVersion", wafUserAgent)
	_ = navigator.Set("platform", "Linux x86_64")
	_ = navigator.Set("language", "zh-CN")
	_ = navigator.Set("languages", vm.NewArray("zh-CN", "zh"))
	_ = navigator.Set("cookieEnabled", true)
	_ = navigator.Set("webdriver", false)
	_ = navigator.Set("vendor", "Google Inc.")
	_ = navigator.Set("vendorSub", "")
	_ = navigator.Set("productSub", "20030107")
	_ = navigator.Set("hardwareConcurrency", 8)
	_ = navigator.Set("deviceMemory", 8)
	_ = navigator.Set("maxTouchPoints", 0)
	_ = navigator.Set("plugins", vm.NewArray())
	_ = navigator.Set("mimeTypes", vm.NewArray())
	_ = navigator.Set("sendBeacon", func(goja.FunctionCall) goja.Value { return vm.ToValue(true) })
	_ = navigator.Set("connection", func() *goja.Object {
		out := vm.NewObject()
		_ = out.Set("effectiveType", "4g")
		_ = out.Set("rtt", 50)
		_ = out.Set("downlink", 10)
		_ = out.Set("saveData", false)
		return out
	}())
	_ = navigator.Set("userAgentData", func() *goja.Object {
		out := vm.NewObject()
		_ = out.Set("mobile", false)
		_ = out.Set("platform", "Linux")
		_ = out.Set("brands", vm.NewArray())
		return out
	}())
	_ = navigator.Set("permissions", func() *goja.Object {
		out := vm.NewObject()
		_ = out.Set("query", func(goja.FunctionCall) goja.Value {
			p := vm.NewObject()
			_ = p.Set("state", "denied")
			_ = p.Set("then", func(call goja.FunctionCall) goja.Value {
				if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
					_, _ = fn(goja.Undefined(), p)
				}
				return p
			})
			return p
		})
		return out
	}())

	screen := vm.NewObject()
	_ = screen.Set("width", 1920)
	_ = screen.Set("height", 1080)
	_ = screen.Set("availWidth", 1920)
	_ = screen.Set("availHeight", 1080)
	_ = screen.Set("colorDepth", 24)
	_ = screen.Set("pixelDepth", 24)
	_ = screen.Set("orientation", func() *goja.Object {
		out := vm.NewObject()
		_ = out.Set("angle", 0)
		_ = out.Set("type", "landscape-primary")
		return out
	}())

	matchMedia := func(call goja.FunctionCall) goja.Value {
		out := vm.NewObject()
		_ = out.Set("matches", false)
		_ = out.Set("media", call.Argument(0).String())
		_ = out.Set("onchange", goja.Null())
		_ = out.Set("addListener", noop)
		_ = out.Set("removeListener", noop)
		_ = out.Set("addEventListener", addListener)
		_ = out.Set("removeEventListener", removeListener)
		_ = out.Set("dispatchEvent", func(goja.FunctionCall) goja.Value { return vm.ToValue(true) })
		return out
	}
	getComputedStyle := func(goja.FunctionCall) goja.Value { return styleFactory() }

	setTimeout := func(call goja.FunctionCall) goja.Value {
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			args := call.Arguments
			start := 2
			if start > len(args) {
				start = len(args)
			}
			_, _ = fn(goja.Undefined(), args[start:]...)
		} else if text := call.Argument(0).String(); text != "" {
			_, _ = vm.RunString(text)
		}
		return vm.ToValue(1)
	}
	clearTimer := func(goja.FunctionCall) goja.Value { return undefined }

	// 老 WAF 脚本仍使用已废弃的 Date.prototype.toGMTString。
	_, _ = vm.RunString(`if (typeof Date.prototype.toGMTString !== "function") { Date.prototype.toGMTString = Date.prototype.toUTCString; }`)

	// 全局对象就是 window。
	_ = vm.Set("document", doc)
	_ = vm.Set("location", loc)
	_ = vm.Set("navigator", navigator)
	_ = vm.Set("screen", screen)
	_ = vm.Set("window", global)
	_ = vm.Set("self", global)
	_ = vm.Set("top", global)
	_ = vm.Set("parent", global)
	_ = vm.Set("frames", global)
	_ = vm.Set("innerWidth", 1920)
	_ = vm.Set("innerHeight", 1080)
	_ = vm.Set("outerWidth", 1920)
	_ = vm.Set("outerHeight", 1080)
	_ = vm.Set("devicePixelRatio", 1)
	_ = vm.Set("screenX", 0)
	_ = vm.Set("screenY", 0)
	_ = vm.Set("scrollX", 0)
	_ = vm.Set("scrollY", 0)
	_ = vm.Set("pageXOffset", 0)
	_ = vm.Set("pageYOffset", 0)
	_ = vm.Set("localStorage", storageFactory())
	_ = vm.Set("sessionStorage", storageFactory())
	_ = vm.Set("addEventListener", addListener)
	_ = vm.Set("removeEventListener", removeListener)
	_ = vm.Set("dispatchEvent", func(goja.FunctionCall) goja.Value { return vm.ToValue(true) })
	_ = vm.Set("matchMedia", matchMedia)
	_ = vm.Set("Image", func(goja.ConstructorCall) *goja.Object { return elementFactory("img") })
	_ = vm.Set("Audio", func(goja.ConstructorCall) *goja.Object { return elementFactory("audio") })
	_ = vm.Set("MutationObserver", func(goja.ConstructorCall) *goja.Object {
		out := vm.NewObject()
		_ = out.Set("observe", noop)
		_ = out.Set("disconnect", noop)
		_ = out.Set("takeRecords", func(goja.FunctionCall) goja.Value { return vm.NewArray() })
		return out
	})
	_ = vm.Set("IntersectionObserver", func(goja.ConstructorCall) *goja.Object {
		out := vm.NewObject()
		_ = out.Set("observe", noop)
		_ = out.Set("unobserve", noop)
		_ = out.Set("disconnect", noop)
		_ = out.Set("takeRecords", func(goja.FunctionCall) goja.Value { return vm.NewArray() })
		return out
	})
	_ = vm.Set("XMLHttpRequest", func(goja.ConstructorCall) *goja.Object {
		out := vm.NewObject()
		_ = out.Set("open", noop)
		_ = out.Set("send", noop)
		_ = out.Set("abort", noop)
		_ = out.Set("setRequestHeader", noop)
		_ = out.Set("getAllResponseHeaders", func(goja.FunctionCall) goja.Value { return vm.ToValue("") })
		_ = out.Set("readyState", 4)
		_ = out.Set("status", 200)
		_ = out.Set("responseText", "")
		return out
	})
	_ = vm.Set("getComputedStyle", getComputedStyle)
	_ = vm.Set("requestAnimationFrame", func(call goja.FunctionCall) goja.Value {
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			_, _ = fn(goja.Undefined(), vm.ToValue(0))
		}
		return vm.ToValue(1)
	})
	_ = vm.Set("cancelAnimationFrame", func(goja.FunctionCall) goja.Value { return undefined })
	_ = vm.Set("setTimeout", setTimeout)
	_ = vm.Set("setInterval", setTimeout)
	_ = vm.Set("clearTimeout", clearTimer)
	_ = vm.Set("clearInterval", clearTimer)
	_ = vm.Set("queueMicrotask", func(call goja.FunctionCall) goja.Value {
		if fn, ok := goja.AssertFunction(call.Argument(0)); ok {
			_, _ = fn(goja.Undefined())
		}
		return undefined
	})
	_ = vm.Set("atob", func(call goja.FunctionCall) goja.Value {
		text := call.Argument(0).String()
		out, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			out, err = base64.RawURLEncoding.DecodeString(text)
		}
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(string(out))
	})
	_ = vm.Set("btoa", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(base64.StdEncoding.EncodeToString([]byte(call.Argument(0).String())))
	})
	_ = vm.Set("escape", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(url.QueryEscape(call.Argument(0).String()))
	})
	_ = vm.Set("unescape", func(call goja.FunctionCall) goja.Value {
		out, _ := url.QueryUnescape(call.Argument(0).String())
		return vm.ToValue(out)
	})
	_ = vm.Set("console", map[string]any{
		"log": noop, "warn": noop, "error": noop, "info": noop, "debug": noop,
	})
	_ = vm.Set("performance", func() *goja.Object {
		out := vm.NewObject()
		start := time.Now()
		_ = out.Set("now", func(goja.FunctionCall) goja.Value {
			return vm.ToValue(float64(time.Since(start).Microseconds()) / 1000)
		})
		return out
	}())
	_ = vm.Set("crypto", func() *goja.Object {
		out := vm.NewObject()
		_ = out.Set("getRandomValues", func(call goja.FunctionCall) goja.Value { return call.Argument(0) })
		_ = out.Set("randomUUID", func(goja.FunctionCall) goja.Value {
			return vm.ToValue("00000000-0000-4000-8000-000000000000")
		})
		return out
	}())

	return func() {
		event := vm.NewObject()
		_ = event.Set("type", "load")
		_ = event.Set("target", doc)
		for _, fn := range callbacks {
			_, _ = fn(goja.Undefined(), event)
		}
		for _, name := range []string{"onload", "onreadystatechange", "onDOMContentLoaded"} {
			if fn, ok := goja.AssertFunction(global.Get(name)); ok {
				_, _ = fn(goja.Undefined(), event)
			}
		}
	}
}

// sanitizeChallengeScript 去掉常见 WAF 脚本里的反调试死循环。
//
// 天启当前返回的阿里云挑战脚本会检测 Function.prototype.toString/Node 环境，
// 不满足时进入 while(true)。Node/V8 正常，但 goja 会永久卡住。挑战本身只需要
// 后面的 arg1 置换和 XOR，因此移除反调试 IIFE 和 a0j 内的 new k(...) 调用。
func sanitizeChallengeScript(script string) string {
	startMarker := "(function(){l(this,function(){"
	for {
		start := strings.Index(script, startMarker)
		if start < 0 {
			break
		}
		end := findMatchingParen(script, start)
		if end <= start {
			break
		}
		script = script[:start] + script[end:]
	}
	script = strings.ReplaceAll(script, "new k(a0j)['dQvDam'](),", "")
	return script
}

func findMatchingParen(script string, start int) int {
	depth := 0
	var quote byte
	escaped := false
	for i := start; i < len(script); i++ {
		ch := script[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// RefreshWafCookie 执行站点 JS 挑战，拿到放行 Cookie（无需浏览器）。
func RefreshWafCookie(baseURL string) (result wafResult) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[waf] challenge runtime panic: %v", r)
			result = wafResult{Message: "执行站点挑战脚本异常，请检查上游站点或稍后重试"}
		}
	}()
	root := strings.TrimRight(baseURL, "/")
	req, err := http.NewRequest(http.MethodGet, root+"/", nil)
	if err != nil {
		return wafResult{Message: "网络错误，无法访问站点"}
	}
	req.Header.Set("User-Agent", wafUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	// 复用全局 httpClient，使其与接口请求走同一出口（支持代理）。
	res, err := httpClient.Do(req)
	if err != nil {
		return wafResult{Message: "网络错误，无法访问站点（请检查服务器出网、代理或 DNS）"}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	text := string(raw)

	jar := newCookieJar()
	for _, c := range res.Header.Values("Set-Cookie") {
		jar.addHeader(c)
	}

	// ESA 滑块验证码 / 自定义规则拦截：HTTP 200 但返回 HTML，且 u_asig 由
	// 阿里云服务端签名，本地无法计算。必须明确报错，不能当成“拿到 Cookie 成功”。
	if blockedByESA(res.Header, text) {
		return wafResult{Message: "上游启用了阿里云 ESA 人机校验，服务器出口 IP 被拦截（X-Tengine-Error: denied），无法自动通过；请更换出口 IP 或申请白名单"}
	}

	matches := scriptPattern.FindAllStringSubmatch(text, -1)
	var scripts []string
	for _, m := range matches {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			scripts = append(scripts, m[1])
		}
	}
	if len(scripts) == 0 || !strings.Contains(text, "arg1") {
		if cookie := jar.String(); cookie != "" {
			return wafResult{OK: true, Cookie: cookie}
		}
		return wafResult{Message: "站点未返回挑战，也未能获取 Cookie"}
	}

	vm := goja.New()
	runCallbacks := newBrowserEnv(vm, jar, root)

	// 挑战页通常内联 var arg1=... 的脚本；优先只执行挑战脚本，避免误跑站点正常 bundle。
	challengeScripts := make([]string, 0, len(scripts))
	for _, script := range scripts {
		if strings.Contains(script, "arg1") || strings.Contains(script, "acw_sc__v2") {
			challengeScripts = append(challengeScripts, script)
		}
	}
	if len(challengeScripts) > 0 {
		scripts = challengeScripts
	}

	interrupt := time.AfterFunc(8*time.Second, func() {
		vm.Interrupt("challenge script timeout")
	})
	defer interrupt.Stop()

	firstErr := ""
	interrupted := false
	for _, script := range scripts {
		script = sanitizeChallengeScript(script)
		if _, err := vm.RunString(script); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			log.Printf("[waf] challenge script error: %v", err)
			if strings.Contains(err.Error(), "interrupted") {
				interrupted = true
				break
			}
		}
	}
	if !interrupted {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[waf] challenge callback panic: %v", r)
					if firstErr == "" {
						firstErr = fmt.Sprintf("callback panic: %v", r)
					}
				}
			}()
			runCallbacks()
		}()
	}

	if cookie := jar.String(); cookie != "" && (firstErr == "" || jar.changed) {
		return wafResult{OK: true, Cookie: cookie}
	}
	if firstErr != "" {
		if len(firstErr) > 220 {
			firstErr = firstErr[:220]
		}
		return wafResult{Message: "挑战脚本执行失败：" + firstErr}
	}
	return wafResult{Message: "挑战脚本未产生 Cookie"}
}
