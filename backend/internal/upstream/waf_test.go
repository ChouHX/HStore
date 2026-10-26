package upstream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func TestRefreshWafCookieOnceRecoversPanic(t *testing.T) {
	old := refreshWafCookieFn
	defer func() { refreshWafCookieFn = old }()

	refreshWafCookieFn = func(string) wafResult { panic("boom") }
	res := RefreshWafCookieOnce("https://example.invalid")
	if res.OK {
		t.Fatalf("expected failure, got %+v", res)
	}
	if res.Message == "" {
		t.Fatal("expected readable error message")
	}

	refreshWafCookieFn = func(string) wafResult { return wafResult{OK: true, Cookie: "acw_tc=ok"} }
	res = RefreshWafCookieOnce("https://example.invalid")
	if !res.OK || res.Cookie != "acw_tc=ok" {
		t.Fatalf("stuck after panic: %+v", res)
	}
}

func TestRefreshWafCookieOnceDeduplicates(t *testing.T) {
	old := refreshWafCookieFn
	defer func() { refreshWafCookieFn = old }()

	calls := 0
	refreshWafCookieFn = func(string) wafResult {
		calls++
		time.Sleep(20 * time.Millisecond)
		return wafResult{OK: true, Cookie: "acw_tc=dedupe"}
	}
	done := make(chan wafResult, 2)
	go func() { done <- RefreshWafCookieOnce("https://example.invalid") }()
	time.Sleep(2 * time.Millisecond)
	go func() { done <- RefreshWafCookieOnce("https://example.invalid") }()
	for i := 0; i < 2; i++ {
		if res := <-done; !res.OK || res.Cookie != "acw_tc=dedupe" {
			t.Fatalf("unexpected result: %+v", res)
		}
	}
	if calls != 1 {
		t.Fatalf("expected one refresh call, got %d", calls)
	}
}

func TestBrowserEnvDocumentCookie(t *testing.T) {
	vm := goja.New()
	jar := newCookieJar()
	jar.set("acw_tc=initial")
	runCallbacks := newBrowserEnv(vm, jar, "https://tianqi996.com")
	if _, err := vm.RunString(`
		var el = document.createElement("div");
		el.style.display = "none";
		el.setAttribute("data-x", "1");
		document.cookie = "acw_sc__v2=challenge; path=/; max-age=3600";
		var ua = navigator.userAgent;
		var ctx = document.createElement("canvas").getContext("2d");
		ctx.fillRect(0, 0, 1, 1);
		setTimeout(function(){ document.cookie = "delayed=1"; }, 1000);
	`); err != nil {
		t.Fatalf("script error: %v", err)
	}
	runCallbacks()
	cookie := jar.String()
	if !strings.Contains(cookie, "acw_sc__v2=challenge") || !strings.Contains(cookie, "delayed=1") {
		t.Fatalf("cookie not captured: %s", cookie)
	}
	if strings.Contains(cookie, "path=") || strings.Contains(cookie, "max-age=") {
		t.Fatalf("cookie attributes leaked: %s", cookie)
	}
}

func TestBrowserEnvListener(t *testing.T) {
	vm := goja.New()
	jar := newCookieJar()
	run := newBrowserEnv(vm, jar, "https://tianqi996.com")
	if _, err := vm.RunString(`
		window.addEventListener("load", function(){ document.cookie = "loaded=1"; });
	`); err != nil {
		t.Fatal(err)
	}
	run()
	if !strings.Contains(jar.String(), "loaded=1") {
		t.Fatalf("load callback not executed: %s", jar.String())
	}
}

func TestRefreshWafCookieExecutesChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "acw_tc=initial; Path=/; HttpOnly")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><script>
			var arg1 = 'challenge';
			document.cookie = "acw_sc__v2=ok; path=/; max-age=3600";
			setTimeout(function(){ document.cookie = "after=1"; }, 100);
			window.onload = function(){ document.cookie = "loaded=1"; };
		</script></body></html>`))
	}))
	defer srv.Close()

	res := RefreshWafCookie(srv.URL)
	if !res.OK {
		t.Fatalf("refresh failed: %+v", res)
	}
	for _, want := range []string{"acw_tc=initial", "acw_sc__v2=ok", "after=1", "loaded=1"} {
		if !strings.Contains(res.Cookie, want) {
			t.Fatalf("cookie %q missing %q", res.Cookie, want)
		}
	}
}
