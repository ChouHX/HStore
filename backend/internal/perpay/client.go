// Package perpay 实现 PerPay 业务 API 的 HMAC 请求签名、订单接口与回调验签。
//
// 协议要点（来自 PerPay USAGE.md / openapi.yaml）：
//   - 请求签名原文 8 行：协议、版本、方法、origin-form 路径、Unix 秒、base64url nonce、客户端 ID、请求体 SHA-256
//   - 回调通知时间戳是 Unix 毫秒，签名原文 7 行，须先按原始请求体验签
//   - 只有服务端查询或验签通知显示 CONFIRMED 时才能给用户入账
package perpay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	apiVersion      = "v1"
	maxResponseSize = 256 * 1024
)

var (
	clientIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)
	merchantNoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	uuidPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	hexDigestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// APIError 是 PerPay 返回的非 2xx 错误。
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("PerPay 返回 HTTP %d：%s", e.Status, e.Message)
	}
	return fmt.Sprintf("PerPay 返回 HTTP %d [%s]：%s", e.Status, e.Code, e.Message)
}

// Config 是 PerPay 接入参数。
type Config struct {
	BaseURL       string
	ClientID      string
	APISecret     string
	WebhookSecret string
}

// ValidateOrigin 校验 PerPay 地址必须是纯 origin（HTTPS，或本机 HTTP）。
func ValidateOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("请填写 PerPay 地址")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("PerPay 地址格式不正确")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("PerPay 地址只填写来源（如 https://pay.example.com），不要包含路径、凭据或参数")
	}
	if u.Scheme != "https" {
		localhost := u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
		if !localhost {
			return "", errors.New("PerPay 地址必须使用 HTTPS（本机调试可用 http://localhost）")
		}
	}
	return u.Scheme + "://" + u.Host, nil
}

// ValidateClientID 校验 PerPay 客户端 ID。
func ValidateClientID(clientID string) error {
	if !clientIDPattern.MatchString(strings.TrimSpace(clientID)) {
		return errors.New("PerPay 客户端 ID 格式不正确")
	}
	return nil
}

// DecodeSecret 解码 PerPay 生成的 32 字节 base64url 密钥。
func DecodeSecret(value, name string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("请填写 %s", name)
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != value {
		return nil, fmt.Errorf("%s 必须是 PerPay 生成的 32 字节 base64url 密钥", name)
	}
	return raw, nil
}

// ValidMerchantOrderNo 校验商户订单号。
func ValidMerchantOrderNo(v string) bool { return merchantNoPattern.MatchString(v) }

// NewUUID 生成 UUID v4。
func NewUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x-%04x-4%03x-8%03x-%012x",
			time.Now().UnixNano()&0xffffffff, time.Now().UnixNano()&0xffff,
			time.Now().UnixNano()&0xfff, time.Now().UnixNano()&0xfff,
			time.Now().UnixNano()&0xffffffffffff)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Client 是 PerPay 业务 API 客户端。
type Client struct {
	origin    string
	clientID  string
	secret    []byte
	http      *http.Client
	userAgent string
}

// NewClient 创建客户端。
func NewClient(cfg Config) (*Client, error) {
	origin, err := ValidateOrigin(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	clientID := strings.TrimSpace(cfg.ClientID)
	if err := ValidateClientID(clientID); err != nil {
		return nil, err
	}
	secret, err := DecodeSecret(cfg.APISecret, "API 密钥")
	if err != nil {
		return nil, err
	}
	return &Client{
		origin:   origin,
		clientID: clientID,
		secret:   secret,
		http: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		userAgent: "hstore-perpay/1.0",
	}, nil
}

// signHeaders 生成一整套签名请求头。
func (c *Client) signHeaders(method, target string, body []byte) (http.Header, error) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, errors.New("无法生成 PerPay 请求 nonce")
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	bodyDigest := sha256Hex(body)

	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(strings.Join([]string{
		"PERPAY-HMAC-SHA256", apiVersion, strings.ToUpper(method), target,
		timestamp, nonce, c.clientID, bodyDigest,
	}, "\n")))

	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("X-PerPay-Client-Id", c.clientID)
	h.Set("X-PerPay-Timestamp", timestamp)
	h.Set("X-PerPay-Nonce", nonce)
	h.Set("X-PerPay-Signature-Version", apiVersion)
	h.Set("X-PerPay-Signature", hex.EncodeToString(mac.Sum(nil)))
	return h, nil
}

func (c *Client) request(ctx context.Context, method, target string, payload any) (json.RawMessage, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("生成 PerPay 请求失败：%w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.origin+target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	headers, err := c.signHeaders(method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header = headers
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 PerPay：%w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取 PerPay 响应失败：%w", err)
	}
	if len(raw) > maxResponseSize {
		return nil, errors.New("PerPay 响应过大")
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type"))), "application/json") {
		return nil, fmt.Errorf("PerPay 未返回 JSON（HTTP %d），请检查地址与反向代理", resp.StatusCode)
	}

	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.New("PerPay 响应不是有效的 JSON")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, Message: "请求失败"}
		if env.Error != nil {
			if env.Error.Code != "" {
				apiErr.Code = env.Error.Code
			}
			if env.Error.Message != "" {
				apiErr.Message = env.Error.Message
			}
		}
		return nil, apiErr
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, errors.New("PerPay 返回的数据格式不正确")
	}
	return env.Data, nil
}

// CreateOrderRequest 是创建订单的签名请求体。
type CreateOrderRequest struct {
	IdempotencyKey  string  `json:"idempotency_key"`
	MerchantOrderNo string  `json:"merchant_order_no"`
	AmountCents     int64   `json:"amount_cents"`
	ProductName     string  `json:"product_name"`
	Note            *string `json:"note,omitempty"`
	NotifyURL       *string `json:"notify_url,omitempty"`
	ReturnURL       *string `json:"return_url,omitempty"`
}

// Order 是 PerPay 订单投影。
type Order struct {
	OrderID              string       `json:"order_id"`
	MerchantOrderNo      string       `json:"merchant_order_no"`
	RequestedAmountCents int64        `json:"requested_amount_cents"`
	PayableAmountCents   int64        `json:"payable_amount_cents"`
	ReceivedAmountCents  *int64       `json:"received_amount_cents"`
	Currency             string       `json:"currency"`
	ProductName          string       `json:"product_name"`
	Note                 *string      `json:"note"`
	ReturnURL            *string      `json:"return_url"`
	Checkout             Checkout     `json:"checkout"`
	Payment              Payment      `json:"payment"`
	Refund               Refund       `json:"refund"`
	Notification         Notification `json:"notification"`
	CreatedAt            string       `json:"created_at"`
	UpdatedAt            string       `json:"updated_at"`
	Version              int          `json:"version"`
}

type Checkout struct {
	Status      string  `json:"status"`
	Token       string  `json:"token"`
	StateURL    string  `json:"state_url"`
	CheckoutURL string  `json:"checkout_url"`
	ExpiresAt   string  `json:"expires_at"`
	ClosedAt    *string `json:"closed_at"`
}

type Payment struct {
	Status              string `json:"status"`
	Basis               string `json:"basis"`
	ReceivedAmountCents *int64 `json:"received_amount_cents"`
}

type Refund struct {
	Status string `json:"status"`
}

type Notification struct {
	NotifyURL *string `json:"notify_url"`
}

// ValidateOrder 校验订单投影的金额、状态与订单号。
func ValidateOrder(o *Order) error {
	if !uuidPattern.MatchString(o.OrderID) {
		return errors.New("PerPay 订单 ID 格式不正确")
	}
	if !merchantNoPattern.MatchString(o.MerchantOrderNo) {
		return errors.New("PerPay 商户订单号格式不正确")
	}
	if o.Currency != "CNY" {
		return errors.New("PerPay 订单币种不是 CNY")
	}
	if o.RequestedAmountCents < 1 || o.PayableAmountCents < o.RequestedAmountCents {
		return errors.New("PerPay 订单金额不正确")
	}
	if o.ReceivedAmountCents != nil && *o.ReceivedAmountCents < 0 {
		return errors.New("PerPay 实收金额不正确")
	}
	switch o.Payment.Status {
	case "UNPAID", "CONFIRMED", "DISPUTED":
	default:
		return errors.New("PerPay 付款状态不正确")
	}
	if o.Version < 1 {
		return errors.New("PerPay 订单版本不正确")
	}
	return nil
}

// CreateOrder 创建或幂等重放订单。
func (c *Client) CreateOrder(ctx context.Context, payload CreateOrderRequest) (*Order, error) {
	raw, err := c.request(ctx, http.MethodPost, "/api/v1/orders", payload)
	if err != nil {
		return nil, err
	}
	var o Order
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, errors.New("解析 PerPay 订单失败")
	}
	if err := ValidateOrder(&o); err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOrder 按 PerPay 内部订单 ID 查询。
func (c *Client) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	if !uuidPattern.MatchString(orderID) {
		return nil, errors.New("PerPay 订单 ID 格式不正确")
	}
	raw, err := c.request(ctx, http.MethodGet, "/api/v1/orders/"+orderID, nil)
	if err != nil {
		return nil, err
	}
	var o Order
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, errors.New("解析 PerPay 订单失败")
	}
	if err := ValidateOrder(&o); err != nil {
		return nil, err
	}
	return &o, nil
}

// GetOrderByMerchantNo 按商户订单号查询。
func (c *Client) GetOrderByMerchantNo(ctx context.Context, merchantNo string) (*Order, error) {
	if !merchantNoPattern.MatchString(merchantNo) {
		return nil, errors.New("商户订单号格式不正确")
	}
	raw, err := c.request(ctx, http.MethodGet, "/api/v1/orders/by-merchant-no/"+merchantNo, nil)
	if err != nil {
		return nil, err
	}
	var o Order
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, errors.New("解析 PerPay 订单失败")
	}
	if err := ValidateOrder(&o); err != nil {
		return nil, err
	}
	return &o, nil
}

// CloseOrder 关闭未支付收银台。
func (c *Client) CloseOrder(ctx context.Context, orderID string) (*Order, error) {
	if !uuidPattern.MatchString(orderID) {
		return nil, errors.New("PerPay 订单 ID 格式不正确")
	}
	raw, err := c.request(ctx, http.MethodPost, "/api/v1/orders/"+orderID+"/actions/close", nil)
	if err != nil {
		return nil, err
	}
	var o Order
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, errors.New("解析 PerPay 订单失败")
	}
	if err := ValidateOrder(&o); err != nil {
		return nil, err
	}
	return &o, nil
}

// SystemStatus 是本业务网站在 PerPay 的状态。
type SystemStatus struct {
	Status               string  `json:"status"`
	Code                 *string `json:"code"`
	ClientID             string  `json:"client_id"`
	AcceptingOrders      bool    `json:"accepting_orders"`
	NotificationsEnabled bool    `json:"notifications_enabled"`
}

// GetSystemStatus 通过签名接口检查网站接入状态。
func (c *Client) GetSystemStatus(ctx context.Context) (*SystemStatus, error) {
	raw, err := c.request(ctx, http.MethodGet, "/api/v1/system/status", nil)
	if err != nil {
		return nil, err
	}
	var s SystemStatus
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, errors.New("解析 PerPay 状态失败")
	}
	if s.ClientID != c.clientID {
		return nil, errors.New("PerPay 返回的客户端 ID 与配置不一致")
	}
	return &s, nil
}

// ===== 回调通知 =====

// WebhookEvent 是 PerPay 通知正文的固定字段。
type WebhookEvent struct {
	Schema               string  `json:"schema"`
	EventID              string  `json:"event_id"`
	EventType            string  `json:"event_type"`
	OrderID              string  `json:"order_id"`
	MerchantOrderNo      string  `json:"merchant_order_no"`
	ProductName          string  `json:"product_name"`
	Note                 *string `json:"note"`
	Currency             string  `json:"currency"`
	RequestedAmountCents int64   `json:"requested_amount_cents"`
	PayableAmountCents   int64   `json:"payable_amount_cents"`
	ReceivedAmountCents  *int64  `json:"received_amount_cents"`
	PaymentStatus        string  `json:"payment_status"`
	PaymentBasis         string  `json:"payment_basis"`
	RefundStatus         string  `json:"refund_status"`
	OrderVersion         int     `json:"order_version"`
	OccurredAt           int64   `json:"occurred_at"`
}

// VerifiedWebhook 是验签通过的通知。
type VerifiedWebhook struct {
	Event      WebhookEvent
	Digest     string
	DeliveryID string
	KeyID      string
}

// VerifyWebhook 对原始请求体验签并校验通知结构。
func VerifyWebhook(headers http.Header, body []byte, secret []byte, now time.Time) (*VerifiedWebhook, error) {
	version := headers.Get("X-PerPay-Webhook-Version")
	keyID := headers.Get("X-PerPay-Webhook-Key-Id")
	timestamp := headers.Get("X-PerPay-Webhook-Timestamp")
	deliveryID := headers.Get("X-PerPay-Webhook-Delivery-Id")
	eventID := headers.Get("X-PerPay-Webhook-Event-Id")
	attempt := headers.Get("X-PerPay-Webhook-Attempt")
	signature := headers.Get("X-PerPay-Webhook-Signature")

	if version != "1" || !uuidPattern.MatchString(keyID) || !uuidPattern.MatchString(deliveryID) ||
		!uuidPattern.MatchString(eventID) || !validUnixMillis(timestamp) ||
		!regexp.MustCompile(`^[1-9][0-9]{0,8}$`).MatchString(attempt) ||
		!strings.HasPrefix(signature, "v1=") || !hexDigestPattern.MatchString(strings.TrimPrefix(signature, "v1=")) {
		return nil, errors.New("通知头部无效")
	}
	ts, _ := parseInt64(timestamp)
	diff := now.UnixMilli() - ts
	if diff < 0 {
		diff = -diff
	}
	if diff > 5*60*1000 {
		return nil, errors.New("通知投递时间偏差超过五分钟")
	}

	digest := sha256Hex(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strings.Join([]string{
		"perpay:webhook:v1", keyID, timestamp, deliveryID, eventID, attempt, digest,
	}, "\n")))
	expected := "v1=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return nil, errors.New("通知签名不正确")
	}

	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, errors.New("通知不是有效的 JSON")
	}
	if event.Schema != "perpay:outbox-event:v2" || event.EventID != eventID {
		return nil, errors.New("通知 schema 或事件 ID 不正确")
	}
	switch event.EventType {
	case "PAYMENT_CONFIRMED", "PAYMENT_DISPUTED", "REFUND_UPDATED":
	default:
		return nil, errors.New("通知事件类型不正确")
	}
	if !uuidPattern.MatchString(event.OrderID) || !merchantNoPattern.MatchString(event.MerchantOrderNo) ||
		event.Currency != "CNY" || event.RequestedAmountCents < 1 ||
		event.PayableAmountCents < event.RequestedAmountCents || event.OrderVersion < 1 {
		return nil, errors.New("通知订单字段不正确")
	}
	if event.ReceivedAmountCents != nil && *event.ReceivedAmountCents < 0 {
		return nil, errors.New("通知实收金额不正确")
	}
	switch event.PaymentStatus {
	case "UNPAID", "CONFIRMED", "DISPUTED":
	default:
		return nil, errors.New("通知付款状态不正确")
	}
	if (event.EventType == "PAYMENT_CONFIRMED" && event.PaymentStatus != "CONFIRMED") ||
		(event.EventType == "PAYMENT_DISPUTED" && event.PaymentStatus != "DISPUTED") ||
		(event.EventType == "REFUND_UPDATED" && event.RefundStatus == "NONE") {
		return nil, errors.New("通知类型与订单状态不一致")
	}

	return &VerifiedWebhook{Event: event, Digest: digest, DeliveryID: deliveryID, KeyID: keyID}, nil
}

func validUnixMillis(v string) bool {
	if v == "" || len(v) > 19 {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return v != "0"
}

func parseInt64(v string) (int64, error) {
	var out int64
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		out = out*10 + int64(c-'0')
	}
	return out, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
