package upstream

import (
	"testing"
	"time"
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
