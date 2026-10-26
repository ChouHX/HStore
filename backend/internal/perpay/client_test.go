package perpay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestValidateOrigin(t *testing.T) {
	valid := []string{"https://pay.example.com", "http://127.0.0.1:6190", "http://localhost:6190"}
	for _, raw := range valid {
		if _, err := ValidateOrigin(raw); err != nil {
			t.Fatalf("expected %q valid: %v", raw, err)
		}
	}
	invalid := []string{"", "ftp://pay.example.com", "http://pay.example.com", "https://pay.example.com/path", "https://pay.example.com?x=1", "https://user:pass@pay.example.com"}
	for _, raw := range invalid {
		if _, err := ValidateOrigin(raw); err == nil {
			t.Fatalf("expected %q invalid", raw)
		}
	}
}

func TestDecodeSecret(t *testing.T) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	decoded, err := DecodeSecret(encoded, "API 密钥")
	if err != nil || len(decoded) != 32 {
		t.Fatalf("decode failed: %v", err)
	}
	if _, err := DecodeSecret("short", "API 密钥"); err == nil {
		t.Fatal("expected short secret invalid")
	}
	if _, err := DecodeSecret(base64.StdEncoding.EncodeToString(raw), "API 密钥"); err == nil {
		t.Fatal("expected padded secret invalid")
	}
}

func TestVerifyWebhook(t *testing.T) {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)

	event := WebhookEvent{
		Schema:               "perpay:outbox-event:v2",
		EventID:              "11111111-1111-4111-8111-111111111111",
		EventType:            "PAYMENT_CONFIRMED",
		OrderID:              "22222222-2222-4222-8222-222222222222",
		MerchantOrderNo:      "PP20260101000000ABCDEF12",
		ProductName:          "账户余额充值",
		Currency:             "CNY",
		RequestedAmountCents: 2500,
		PayableAmountCents:   2501,
		ReceivedAmountCents:  int64Ptr(2501),
		PaymentStatus:        "CONFIRMED",
		PaymentBasis:         "INFERRED",
		RefundStatus:         "NONE",
		OrderVersion:         2,
		OccurredAt:           time.Now().UnixMilli(),
	}
	body, _ := json.Marshal(event)
	headers, digest := signedWebhookHeaders(t, secret, body, event.EventID, time.Now())

	verified, err := VerifyWebhook(headers, body, secret, time.Now())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Event.MerchantOrderNo != event.MerchantOrderNo || verified.Digest != digest {
		t.Fatal("verified event mismatch")
	}

	// 篡改正文必须失败
	badBody := append([]byte(nil), body...)
	badBody[len(badBody)-2] ^= 1
	if _, err := VerifyWebhook(headers, badBody, secret, time.Now()); err == nil {
		t.Fatal("expected tampered body rejected")
	}

	// 时间偏差超过 5 分钟必须失败
	if _, err := VerifyWebhook(headers, body, secret, time.Now().Add(6*time.Minute)); err == nil {
		t.Fatal("expected stale timestamp rejected")
	}
}

func TestVerifyWebhookStatusMismatch(t *testing.T) {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	event := WebhookEvent{
		Schema:               "perpay:outbox-event:v2",
		EventID:              "11111111-1111-4111-8111-111111111111",
		EventType:            "PAYMENT_CONFIRMED",
		OrderID:              "22222222-2222-4222-8222-222222222222",
		MerchantOrderNo:      "PP20260101000000ABCDEF12",
		ProductName:          "x",
		Currency:             "CNY",
		RequestedAmountCents: 100,
		PayableAmountCents:   100,
		PaymentStatus:        "UNPAID",
		RefundStatus:         "NONE",
		OrderVersion:         1,
	}
	body, _ := json.Marshal(event)
	headers, _ := signedWebhookHeaders(t, secret, body, event.EventID, time.Now())
	if _, err := VerifyWebhook(headers, body, secret, time.Now()); err == nil {
		t.Fatal("expected event/status mismatch rejected")
	}
}

func signedWebhookHeaders(t *testing.T, secret, body []byte, eventID string, now time.Time) (http.Header, string) {
	t.Helper()
	keyID := NewUUID()
	deliveryID := NewUUID()
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	attempt := "1"
	digest := sha256Hex(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("perpay:webhook:v1\n" + keyID + "\n" + timestamp + "\n" + deliveryID + "\n" + eventID + "\n" + attempt + "\n" + digest))
	signature := "v1=" + hex.EncodeToString(mac.Sum(nil))

	h := http.Header{}
	h.Set("X-PerPay-Webhook-Version", "1")
	h.Set("X-PerPay-Webhook-Key-Id", keyID)
	h.Set("X-PerPay-Webhook-Timestamp", timestamp)
	h.Set("X-PerPay-Webhook-Delivery-Id", deliveryID)
	h.Set("X-PerPay-Webhook-Event-Id", eventID)
	h.Set("X-PerPay-Webhook-Attempt", attempt)
	h.Set("X-PerPay-Webhook-Signature", signature)
	return h, digest
}

func int64Ptr(v int64) *int64 { return &v }
