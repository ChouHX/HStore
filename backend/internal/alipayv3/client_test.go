package alipayv3

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testKeyPair(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key, &key.PublicKey
}

func privatePEM(key *rsa.PrivateKey) string {
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func publicPEM(key *rsa.PublicKey) string {
	der, _ := x509.MarshalPKIXPublicKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestParseKeysAndRawBase64(t *testing.T) {
	key, pub := testKeyPair(t)
	privPEM := privatePEM(key)
	pubPEM := publicPEM(pub)
	if _, err := ParsePrivateKey(privPEM); err != nil {
		t.Fatalf("private pem: %v", err)
	}
	if _, err := ParsePublicKey(pubPEM); err != nil {
		t.Fatalf("public pem: %v", err)
	}
	// 支付宝后台常给一行 base64（无 PEM 头）
	privDER, _ := x509.MarshalPKCS8PrivateKey(key)
	pubDER, _ := x509.MarshalPKIXPublicKey(pub)
	if _, err := ParsePrivateKey(base64.StdEncoding.EncodeToString(privDER)); err != nil {
		t.Fatalf("private raw: %v", err)
	}
	if _, err := ParsePublicKey(base64.StdEncoding.EncodeToString(pubDER)); err != nil {
		t.Fatalf("public raw: %v", err)
	}
}

func TestParseAmountAndTime(t *testing.T) {
	cases := map[string]int64{"0.01": 1, "10": 1000, "12.34": 1234, "9999999999.99": 999999999999}
	for input, want := range cases {
		got, err := ParseAmountCents(input)
		if err != nil || got != want {
			t.Fatalf("amount %s = %d, %v; want %d", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "0", "1.234", "-1.00", "abc"} {
		if _, err := ParseAmountCents(bad); err == nil {
			t.Fatalf("expected invalid amount %q", bad)
		}
	}
	ms, precision, err := ParseOccurredAt("2026-10-26 12:34:56")
	if err != nil {
		t.Fatal(err)
	}
	if precision != 1000 {
		t.Fatalf("precision = %d", precision)
	}
	want := time.Date(2026, 10, 26, 12, 34, 56, 0, time.FixedZone("CST", 8*3600)).UnixMilli()
	if ms != want {
		t.Fatalf("ms = %d, want %d", ms, want)
	}
	_, p1, err := ParseOccurredAt("2026-10-26 12:34:56.1")
	if err != nil || p1 != 100 {
		t.Fatalf("fraction precision = %d, %v", p1, err)
	}
	_, p2, err := ParseOccurredAt("2026-10-26 12:34:56.12")
	if err != nil || p2 != 10 {
		t.Fatalf("fraction precision = %d, %v", p2, err)
	}
	_, p3, err := ParseOccurredAt("2026-10-26 12:34:56.123")
	if err != nil || p3 != 1 {
		t.Fatalf("fraction precision = %d, %v", p3, err)
	}
	if _, _, err := ParseOccurredAt("2026-13-99 99:99:99"); err == nil {
		t.Fatal("expected invalid timestamp")
	}
}

func TestParseDirection(t *testing.T) {
	for _, v := range []string{"收入", "CREDIT", "in", "收款"} {
		if got, err := ParseDirection(v); err != nil || got != "CREDIT" {
			t.Fatalf("direction %s = %s, %v", v, got, err)
		}
	}
	for _, v := range []string{"支出", "DEBIT", "out", "付款"} {
		if got, err := ParseDirection(v); err != nil || got != "DEBIT" {
			t.Fatalf("direction %s = %s, %v", v, got, err)
		}
	}
}

func TestParsePage(t *testing.T) {
	body := []byte(`{"page_no":1,"page_size":20,"total_size":1,"detail_list":[{"account_log_id":"20261026001","trans_dt":"2026-10-26 12:34:56","trans_amount":"12.34","direction":"收入","alipay_order_no":"2026102600001","merchant_order_no":"","trans_memo":"经营码收款","other_account":"a***@b.com"}]}`)
	page, err := parsePage(body)
	if err != nil {
		t.Fatal(err)
	}
	if page.PageNo != 1 || page.PageSize != 20 || page.TotalSize != 1 || len(page.Details) != 1 {
		t.Fatalf("unexpected page: %+v", page)
	}
	d := page.Details[0]
	if d.AmountCents != 1234 || d.Direction != "CREDIT" || d.AccountLogID != "20261026001" {
		t.Fatalf("unexpected detail: %+v", d)
	}
	if d.ExternalEventID != "alipay:20261026001" {
		t.Fatalf("external id = %s", d.ExternalEventID)
	}
}

func TestVerifyResponse(t *testing.T) {
	key, pub := testKeyPair(t)
	body := []byte(`{"page_no":1,"page_size":20,"total_size":0,"detail_list":[]}`)
	ts := "1770000000000"
	nonce := "abc"
	sum := sha256.Sum256([]byte(ts + "\n" + nonce + "\n" + string(body) + "\n"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	h := http.Header{}
	h.Set("Alipay-Timestamp", ts)
	h.Set("Alipay-Nonce", nonce)
	h.Set("Alipay-Signature", base64.StdEncoding.EncodeToString(sig))
	if err := verifyResponse(h, body, pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
	body[0] = 'x'
	if err := verifyResponse(h, body, pub); err == nil {
		t.Fatal("expected tampered body rejected")
	}
}

func TestSignedTargetEncoding(t *testing.T) {
	c := &Client{}
	q := url.Values{}
	q.Set("start_time", "2026-10-26 12:00:00")
	q.Set("page_no", "1")
	q.Set("page_size", "20")
	q.Set("end_time", "2026-10-26 12:05:00")
	target, err := c.signedTarget(http.MethodGet, "/v3/alipay/data/bill/accountlog/query", q, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "/v3/alipay/data/bill/accountlog/query?end_time=2026-10-26%2012%3A05%3A00&page_no=1&page_size=20&start_time=2026-10-26%2012%3A00%3A00"
	if target != want {
		t.Fatalf("target = %s\nwant   = %s", target, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestQueryAccountLogSignsAndVerifies(t *testing.T) {
	appKey, appPub := testKeyPair(t)
	respKey, respPub := testKeyPair(t)
	body := []byte(`{"page_no":1,"page_size":20,"total_size":1,"detail_list":[{"account_log_id":"20261026001","trans_dt":"2026-10-26 12:34:56","trans_amount":"12.34","direction":"收入","alipay_order_no":"2026102600001"}]}`)
	client := &Client{
		appID:      "2026000000000000",
		privateKey: appKey,
		publicKey:  respPub,
		endpoint:   ProductionEndpoint,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			auth := req.Header.Get("Authorization")
			parts := splitOnce(auth, ",sign=")
			if parts == nil {
				t.Fatalf("authorization header missing sign: %s", auth)
			}
			authPart := strings.TrimPrefix(parts[0], "ALIPAY-SHA256withRSA ")
			signingText := authPart + "\nGET\n" + req.URL.RequestURI() + "\n\n"
			sig, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				t.Fatalf("decode request signature: %v", err)
			}
			sum := sha256.Sum256([]byte(signingText))
			if err := rsa.VerifyPKCS1v15(appPub, crypto.SHA256, sum[:], sig); err != nil {
				t.Fatalf("request signature invalid: %v", err)
			}
			if req.URL.Query().Get("end_time") != "2026-10-26 12:05:00" {
				t.Fatalf("end_time missing or decoded: %s", req.URL.RawQuery)
			}
			ts := "1770000000000"
			nonce := "respnonce"
			respSum := sha256.Sum256([]byte(ts + "\n" + nonce + "\n" + string(body) + "\n"))
			respSig, _ := rsa.SignPKCS1v15(rand.Reader, respKey, crypto.SHA256, respSum[:])
			h := http.Header{}
			h.Set("Content-Type", "application/json")
			h.Set("Alipay-Timestamp", ts)
			h.Set("Alipay-Nonce", nonce)
			h.Set("Alipay-Signature", base64.StdEncoding.EncodeToString(respSig))
			h.Set("Alipay-Trace-Id", "trace-1")
			return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(bytes.NewReader(body))}, nil
		})},
	}
	page, err := client.QueryAccountLog(context.Background(), "2026-10-26 12:00:00", "2026-10-26 12:05:00", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Details) != 1 || page.Details[0].AmountCents != 1234 || page.TraceID != "trace-1" {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func splitOnce(value, sep string) []string {
	idx := strings.Index(value, sep)
	if idx < 0 {
		return nil
	}
	return []string{value[:idx], value[idx+len(sep):]}
}

func TestNewClientRejectsInvalidEndpoint(t *testing.T) {
	key, pub := testKeyPair(t)
	_, err := NewClient(Config{
		AppID: "2026000000000000", PrivateKey: privatePEM(key),
		AlipayPublicKey: publicPEM(pub), Endpoint: "https://example.com",
	})
	if err == nil {
		t.Fatal("expected endpoint rejected")
	}
	if _, err := NewClient(Config{AppID: "", PrivateKey: privatePEM(key), AlipayPublicKey: publicPEM(pub)}); err == nil {
		t.Fatal("expected empty app id rejected")
	}
}
