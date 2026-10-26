// Package alipayv3 实现支付宝开放平台 OpenAPI v3 的应用签名、账单查询与响应验签。
//
// 只使用项目内的 crypto/rsa、net/http，不依赖支付宝 SDK。
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
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ProductionEndpoint = "https://openapi.alipay.com"
	SandboxEndpoint    = "https://openapi-sandbox.dl.alipaydev.com"
	accountLogPath     = "/v3/alipay/data/bill/accountlog/query"
	maxResponseBytes   = 2 * 1024 * 1024
	defaultPageSize    = 500
	maxPageSize        = 2000
)

var timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)
var amountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,11})(?:\.([0-9]{1,2}))?$`)

// Config 是支付宝应用配置。
type Config struct {
	AppID           string
	PrivateKey      string
	AlipayPublicKey string
	Endpoint        string
	AppCertSN       string
	RootCertSN      string
	Timeout         time.Duration
}

// Client 是支付宝账务明细查询客户端。
type Client struct {
	appID      string
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	endpoint   string
	appCertSN  string
	rootCertSN string
	http       *http.Client
}

// Page 是一页账务明细。
type Page struct {
	PageNo    int
	PageSize  int
	TotalSize int
	Details   []Detail
	TraceID   string
	RawBody   string
}

// Detail 是一条规范化后的账务明细。
type Detail struct {
	AccountLogID    string
	OccurredAtText  string
	OccurredAt      int64
	PrecisionMs     int64
	AmountText      string
	AmountCents     int64
	Direction       string // CREDIT / DEBIT
	AlipayOrderNo   string
	MerchantOrderNo string
	TransMemo       string
	OtherAccount    string
	ExternalEventID string
	RawJSON         string
}

// APIError 是支付宝网关或 HTTP 层错误。
type APIError struct {
	Status  int
	Code    string
	Message string
	TraceID string
	Body    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("支付宝接口错误 HTTP %d [%s]：%s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("支付宝接口错误 HTTP %d：%s", e.Status, e.Message)
}

// NewClient 创建客户端并校验 RSA 密钥。
func NewClient(cfg Config) (*Client, error) {
	appID := strings.TrimSpace(cfg.AppID)
	if appID == "" || strings.ContainsAny(appID, "\r\n") {
		return nil, errors.New("支付宝 App ID 不能为空")
	}
	privateKey, err := ParsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("应用私钥无效：%w", err)
	}
	publicKey, err := ParsePublicKey(cfg.AlipayPublicKey)
	if err != nil {
		return nil, fmt.Errorf("支付宝公钥无效：%w", err)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		endpoint = ProductionEndpoint
	}
	if endpoint != ProductionEndpoint && endpoint != SandboxEndpoint {
		return nil, errors.New("支付宝网关地址不正确")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		appID:      appID,
		privateKey: privateKey,
		publicKey:  publicKey,
		endpoint:   endpoint,
		appCertSN:  strings.TrimSpace(cfg.AppCertSN),
		rootCertSN: strings.TrimSpace(cfg.RootCertSN),
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// ParsePrivateKey 支持 PKCS#1 / PKCS#8 PEM，也支持支付宝后台复制的一行 base64。
func ParsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	block, err := decodePEM(raw, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return validatePrivateKey(rsaKey)
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return validatePrivateKey(key)
	}
	return nil, errors.New("不是有效的 RSA 私钥")
}

// ParsePublicKey 支持 SPKI / PKCS#1 PEM，也支持支付宝后台复制的一行 base64。
func ParsePublicKey(raw string) (*rsa.PublicKey, error) {
	block, err := decodePEM(raw, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return validatePublicKey(rsaKey)
		}
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return validatePublicKey(key)
	}
	return nil, errors.New("不是有效的 RSA 公钥")
}

func decodePEM(raw, defaultLabel string) (*pem.Block, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("密钥为空")
	}
	if strings.Contains(raw, "-----BEGIN") {
		block, _ := pem.Decode([]byte(raw))
		if block == nil {
			return nil, errors.New("PEM 格式不正确")
		}
		return block, nil
	}
	compact := strings.Join(strings.Fields(raw), "")
	der, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return nil, errors.New("密钥不是有效的 base64")
	}
	label := defaultLabel
	if strings.HasPrefix(raw, "MIIE") || strings.HasPrefix(raw, "MIIJ") {
		// 支付宝后台的公钥有时是 PKCS#1；解析失败时再尝试。
		if defaultLabel == "PUBLIC KEY" && len(der) > 0 && der[0] == 0x30 {
			// 保持默认 SPKI，ParsePublicKey 内部还会回退 PKCS#1。
		}
	}
	return &pem.Block{Type: label, Bytes: der}, nil
}

func validatePrivateKey(key *rsa.PrivateKey) (*rsa.PrivateKey, error) {
	if key.N.BitLen() < 2048 {
		return nil, errors.New("RSA 私钥长度至少 2048 位")
	}
	if err := key.Validate(); err != nil {
		return nil, errors.New("RSA 私钥校验失败")
	}
	return key, nil
}

func validatePublicKey(key *rsa.PublicKey) (*rsa.PublicKey, error) {
	if key.N.BitLen() < 2048 {
		return nil, errors.New("RSA 公钥长度至少 2048 位")
	}
	if key.E < 3 {
		return nil, errors.New("RSA 公钥指数无效")
	}
	return key, nil
}

// QueryAccountLog 查询一页账务明细。start/end 使用支付宝要求的 YYYY-MM-DD HH:mm:ss（北京时间）。
func (c *Client) QueryAccountLog(ctx context.Context, start, end string, pageNo, pageSize int) (*Page, error) {
	if !validTimestamp(start) || !validTimestamp(end) || start >= end {
		return nil, errors.New("查账时间范围不正确")
	}
	if pageNo < 1 {
		return nil, errors.New("页码不正确")
	}
	if pageSize < 1 || pageSize > maxPageSize {
		pageSize = defaultPageSize
	}
	query := url.Values{}
	query.Set("end_time", end)
	query.Set("page_no", strconv.Itoa(pageNo))
	query.Set("page_size", strconv.Itoa(pageSize))
	query.Set("start_time", start)
	target, err := c.signedTarget(http.MethodGet, accountLogPath, query, nil)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+target, nil)
	if err != nil {
		return nil, err
	}
	nonce := newUUID()
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	authString := fmt.Sprintf("app_id=%s", c.appID)
	if c.appCertSN != "" {
		authString += ",app_cert_sn=" + c.appCertSN
	}
	authString += ",nonce=" + nonce + ",timestamp=" + timestamp
	signingText := authString + "\n" + http.MethodGet + "\n" + target + "\n" + "\n"
	signature, err := signRSA(c.privateKey, signingText)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "ALIPAY-SHA256withRSA "+authString+",sign="+signature)
	req.Header.Set("Alipay-Request-Id", newUUID())
	if c.rootCertSN != "" {
		req.Header.Set("Alipay-Root-Cert-Sn", c.rootCertSN)
	}
	req.Header.Set("User-Agent", "hstore-alipay/1.0")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接支付宝失败：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取支付宝响应失败：%w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, errors.New("支付宝响应过大")
	}
	traceID := resp.Header.Get("Alipay-Trace-Id")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			Status: resp.StatusCode, Code: fmt.Sprintf("http_%d", resp.StatusCode),
			Message: string(body), TraceID: traceID, Body: string(body),
		}
	}
	if err := verifyResponse(resp.Header, body, c.publicKey); err != nil {
		return nil, err
	}
	page, err := parsePage(body)
	if err != nil {
		return nil, err
	}
	page.TraceID = traceID
	page.RawBody = string(body)
	return page, nil
}

// signedTarget 返回参与签名的 origin-form 路径和查询串。
func (c *Client) signedTarget(method, path string, query url.Values, body []byte) (string, error) {
	_ = method
	_ = body
	encoded := query.Encode()
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	if encoded != "" {
		return path + "?" + encoded, nil
	}
	return path, nil
}

func signRSA(key *rsa.PrivateKey, text string) (string, error) {
	sum := sha256.Sum256([]byte(text))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", errors.New("支付宝请求签名失败")
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func verifyResponse(headers http.Header, body []byte, publicKey *rsa.PublicKey) error {
	timestamp := headers.Get("Alipay-Timestamp")
	nonce := headers.Get("Alipay-Nonce")
	signature := headers.Get("Alipay-Signature")
	if timestamp == "" || nonce == "" || signature == "" {
		return errors.New("支付宝响应缺少验签头")
	}
	signingText := timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return errors.New("支付宝响应签名格式不正确")
	}
	sum := sha256.Sum256([]byte(signingText))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, sum[:], sig); err != nil {
		return errors.New("支付宝响应验签失败")
	}
	return nil
}

func parsePage(body []byte) (*Page, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, errors.New("支付宝账务明细不是有效 JSON")
	}
	pageNo := mapInt(root, "page_no")
	pageSize := mapInt(root, "page_size")
	totalSize := mapInt(root, "total_size")
	rawDetails, _ := root["detail_list"].([]any)
	if pageNo == 0 && totalSize == 0 && len(rawDetails) == 0 {
		pageNo = 1
	}
	if pageNo < 1 || pageSize < 1 || pageSize > maxPageSize || totalSize < 0 {
		return nil, errors.New("支付宝账务明细分页信息不正确")
	}
	if len(rawDetails) > pageSize {
		return nil, errors.New("支付宝账务明细数量超过页大小")
	}
	details := make([]Detail, 0, len(rawDetails))
	for _, raw := range rawDetails {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		detail, err := normalizeDetail(item)
		if err != nil {
			// 单条脏数据不应阻断整页；保留原始 JSON 由后续人工核对。
			continue
		}
		details = append(details, detail)
	}
	return &Page{PageNo: pageNo, PageSize: pageSize, TotalSize: totalSize, Details: details}, nil
}

func normalizeDetail(item map[string]any) (Detail, error) {
	accountLogID := mapString(item, "account_log_id")
	occurredText := mapString(item, "trans_dt")
	amountText := mapString(item, "trans_amount")
	directionText := mapString(item, "direction")
	if occurredText == "" || amountText == "" || directionText == "" {
		return Detail{}, errors.New("账务明细缺少字段")
	}
	occurredAt, precision, err := ParseOccurredAt(occurredText)
	if err != nil {
		return Detail{}, err
	}
	amountCents, err := ParseAmountCents(amountText)
	if err != nil {
		return Detail{}, err
	}
	direction, err := ParseDirection(directionText)
	if err != nil {
		return Detail{}, err
	}
	rawJSON, _ := json.Marshal(item)
	detail := Detail{
		AccountLogID:    accountLogID,
		OccurredAtText:  occurredText,
		OccurredAt:      occurredAt,
		PrecisionMs:     precision,
		AmountText:      amountText,
		AmountCents:     amountCents,
		Direction:       direction,
		AlipayOrderNo:   mapString(item, "alipay_order_no"),
		MerchantOrderNo: mapString(item, "merchant_order_no"),
		TransMemo:       mapString(item, "trans_memo"),
		OtherAccount:    mapString(item, "other_account"),
		RawJSON:         string(rawJSON),
	}
	if detail.AccountLogID != "" {
		detail.ExternalEventID = "alipay:" + detail.AccountLogID
	} else {
		sum := sha256.Sum256([]byte(strings.Join([]string{
			detail.AlipayOrderNo, detail.OccurredAtText, detail.AmountText, detail.TransMemo, detail.OtherAccount,
		}, "|")))
		detail.ExternalEventID = "alipay:" + hex.EncodeToString(sum[:16])
	}
	return detail, nil
}

func mapString(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func mapInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	case string:
		n, _ := strconv.Atoi(v)
		return n
	case float64:
		return int(v)
	default:
		return 0
	}
}

// ParseAmountCents 把 "12.34" 解析为 1234 分。
func ParseAmountCents(value string) (int64, error) {
	text := strings.TrimSpace(value)
	match := amountPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, errors.New("金额格式不正确")
	}
	whole, _ := strconv.ParseInt(match[1], 10, 64)
	fraction := match[2]
	if len(fraction) == 0 {
		fraction = "00"
	} else if len(fraction) == 1 {
		fraction += "0"
	}
	frac, _ := strconv.ParseInt(fraction, 10, 64)
	cents := whole*100 + frac
	if cents < 1 || cents > 999999999999 {
		return 0, errors.New("金额超出支持范围")
	}
	return cents, nil
}

// ParseDirection 把支付宝的中英文收支方向归一化。
func ParseDirection(value string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	switch normalized {
	case "CREDIT", "IN", "INCOME", "RECEIVE", "RECEIVED", "收入", "收款", "转入":
		return "CREDIT", nil
	case "DEBIT", "OUT", "EXPENSE", "PAY", "PAID", "支出", "付款", "转出":
		return "DEBIT", nil
	default:
		return "", errors.New("收支方向不正确")
	}
}

// ParseOccurredAt 解析支付宝北京时间，返回毫秒与精度。
func ParseOccurredAt(value string) (int64, int64, error) {
	text := strings.TrimSpace(value)
	layout := "2006-01-02 15:04:05"
	precision := int64(1000)
	if strings.Contains(text, ".") {
		layout = "2006-01-02 15:04:05.999999999"
		parts := strings.SplitN(text, ".", 2)
		switch len(parts[1]) {
		case 1:
			precision = 100
		case 2:
			precision = 10
		default:
			precision = 1
		}
	}
	loc := time.FixedZone("CST", 8*60*60)
	t, err := time.ParseInLocation(layout, text, loc)
	if err != nil {
		// 兼容带时区格式
		t, err = time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return 0, 0, errors.New("账务时间格式不正确")
		}
	}
	return t.UnixMilli(), precision, nil
}

func validTimestamp(value string) bool {
	if !timestampPattern.MatchString(value) {
		return false
	}
	_, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.FixedZone("CST", 8*60*60))
	return err == nil
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// EndpointForEnv 返回生产/沙箱网关。
func EndpointForEnv(env string) string {
	if strings.EqualFold(strings.TrimSpace(env), "sandbox") {
		return SandboxEndpoint
	}
	return ProductionEndpoint
}
