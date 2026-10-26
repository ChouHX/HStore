package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"

	"hstore/backend/internal/alipayv3"
	"hstore/backend/internal/model"
	"hstore/backend/internal/store"
)

const (
	alipayScanOverlapMs = 2 * 60 * 1000
	alipayMaxChunkMs    = 60 * 60 * 1000
	alipayMaxLookbackMs = 7 * 24 * 60 * 60 * 1000
)

var (
	alipayScanMu           sync.Mutex
	lastAlipayQuickCheckMs int64
)

// AlipayScanResult 是一次查账/对账的结果。
type AlipayScanResult struct {
	Status    string `json:"status"`
	Cursor    int64  `json:"cursor"`
	Pages     int    `json:"pages"`
	Entries   int    `json:"entries"`
	Settled   int    `json:"settled"`
	Unmatched int    `json:"unmatched"`
	Ambiguous int    `json:"ambiguous"`
	Error     string `json:"error,omitempty"`
	TraceID   string `json:"traceId,omitempty"`
}

// AlipayCheckoutView 是公开收银台可见字段。
type AlipayCheckoutView struct {
	OrderNo        string   `json:"orderNo"`
	ProductName    string   `json:"productName"`
	CollectionCode string   `json:"collectionCode"`
	RequestedCents int64    `json:"requestedCents"`
	PayableCents   int64    `json:"payableCents"`
	PaymentStatus  string   `json:"paymentStatus"`
	CheckoutStatus string   `json:"checkoutStatus"`
	Status         string   `json:"status"`
	ReceivedCents  *int64   `json:"receivedCents"`
	Credited       *float64 `json:"credited"`
	ExpiresAt      int64    `json:"expiresAt"`
	CreatedAt      int64    `json:"createdAt"`
	PaidAt         *int64   `json:"paidAt"`
}

// ValidateAlipayConfigPatch 校验后台提交的支付宝配置。
func ValidateAlipayConfigPatch(patch map[string]any) error {
	env := "production"
	if v, ok := patch["env"].(string); ok {
		env = strings.ToLower(strings.TrimSpace(v))
		if env != "production" && env != "sandbox" {
			return errors.New("支付宝环境只能是 production 或 sandbox")
		}
	}
	appID := strings.TrimSpace(toString(patch["appId"]))
	if appID != "" && strings.ContainsAny(appID, "\r\n") {
		return errors.New("App ID 不能包含换行")
	}
	if v := strings.TrimSpace(toString(patch["privateKey"])); v != "" {
		if _, err := alipayv3.ParsePrivateKey(v); err != nil {
			return fmt.Errorf("应用私钥无效：%w", err)
		}
	}
	if v := strings.TrimSpace(toString(patch["publicKey"])); v != "" {
		if _, err := alipayv3.ParsePublicKey(v); err != nil {
			return fmt.Errorf("支付宝公钥无效：%w", err)
		}
	}
	if v := strings.TrimSpace(toString(patch["collectionCode"])); v != "" {
		if err := validateCollectionCode(v); err != nil {
			return err
		}
	}
	if v := strings.TrimSpace(toString(patch["productName"])); len([]rune(v)) > 200 {
		return errors.New("商品名称最多 200 个字符")
	}
	minCny, _ := store.ToFloat(patch["minCny"])
	maxCny, _ := store.ToFloat(patch["maxCny"])
	if minCny > 0 && minCny < 0.01 {
		return errors.New("最小充值金额至少为 0.01 元")
	}
	if maxCny > 0 && maxCny > 99999999.98 {
		return errors.New("最大充值金额过大")
	}
	if minCny > 0 && maxCny > 0 && minCny > maxCny {
		return errors.New("最小充值金额不能大于最大充值金额")
	}
	if ttl := int(num(patch["orderTtlSeconds"])); ttl > 0 && (ttl < 60 || ttl > 1800) {
		return errors.New("收银台有效期必须在 60–1800 秒之间")
	}
	if offset := int(num(patch["amountOffsetMaxCents"])); offset > 0 && (offset < 1 || offset > 99) {
		return errors.New("金额加价上限必须在 1–99 分之间")
	}
	if cooldown := int(num(patch["cooldownSeconds"])); cooldown > 0 && (cooldown < 60 || cooldown > 3600) {
		return errors.New("金额复用冷却必须在 60–3600 秒之间")
	}
	active := int(num(patch["activeScanSeconds"]))
	normal := int(num(patch["normalScanSeconds"]))
	if active > 0 && (active < 5 || active > 3600) {
		return errors.New("活跃查账间隔必须在 5–3600 秒之间")
	}
	if normal > 0 && (normal < 5 || normal > 3600) {
		return errors.New("常规查账间隔必须在 5–3600 秒之间")
	}
	if active > 0 && normal > 0 && active > normal {
		return errors.New("活跃查账间隔不能大于常规查账间隔")
	}
	if safety := int(num(patch["safetyLagSeconds"])); safety < 0 || safety > 300 {
		return errors.New("查账安全延迟必须在 0–300 秒之间")
	}
	if lookback := int(num(patch["lookbackSeconds"])); lookback > 0 && (lookback < 60 || lookback > 604800) {
		return errors.New("首次查账回看时长必须在 60–604800 秒之间")
	}
	return nil
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) float64 {
	f, _ := store.ToFloat(v)
	return f
}

func validateCollectionCode(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("经营码链接格式不正确")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("经营码链接必须使用 http/https")
	}
	if u.User != nil || u.Fragment != "" {
		return errors.New("经营码链接不能包含凭据或片段")
	}
	if !strings.EqualFold(u.Hostname(), "qr.alipay.com") {
		return errors.New("经营码链接必须来自 qr.alipay.com")
	}
	return nil
}

func alipayClient() (*store.AlipaySecretConfig, *alipayv3.Client, error) {
	cfg := store.GetAlipaySecretConfig()
	if !cfg.Configured {
		return &cfg, nil, errors.New("尚未配置支付宝 App ID、密钥或经营码")
	}
	client, err := alipayv3.NewClient(alipayv3.Config{
		AppID:           cfg.AppID,
		PrivateKey:      cfg.PrivateKey,
		AlipayPublicKey: cfg.PublicKey,
		Endpoint:        cfg.Endpoint,
		Timeout:         15 * time.Second,
	})
	if err != nil {
		return &cfg, nil, err
	}
	return &cfg, client, nil
}

// AlipayPublicConfig 是用户端可见配置。
func AlipayPublicConfig() map[string]any {
	cfg := store.GetAlipayConfig()
	return map[string]any{
		"enabled":     cfg.Enabled,
		"configured":  cfg.Configured,
		"minCny":      cfg.MinCny,
		"maxCny":      cfg.MaxCny,
		"productName": cfg.ProductName,
	}
}

// CreateAlipayOrder 创建经营码充值订单。
func CreateAlipayOrder(user *model.User, cny float64) (*model.AlipayOrder, error) {
	return createAlipayOrder(user, cny, false)
}

// CreateGuestAlipayOrder 为游客购买创建精确金额订单，不做 1–99 分加价。
func CreateGuestAlipayOrder(user *model.User, cny float64) (*model.AlipayOrder, error) {
	return createAlipayOrder(user, cny, true)
}

func createAlipayOrder(user *model.User, cny float64, exactAmount bool) (*model.AlipayOrder, error) {
	cfg := store.GetAlipaySecretConfig()
	if !cfg.Enabled {
		return nil, errors.New("支付宝充值未开启")
	}
	if !cfg.Configured {
		return nil, errors.New("管理员尚未配置支付宝经营码")
	}
	if err := validateCollectionCode(cfg.CollectionCode); err != nil {
		return nil, err
	}
	if math.IsNaN(cny) || math.IsInf(cny, 0) || cny <= 0 {
		return nil, errors.New("请输入充值金额")
	}
	requested := int64(math.Round(cny * 100))
	if exactAmount {
		// 商品直购走精确金额：不足 1 分向上取整（0.064 → 0.07）
		requested = ceilToCents(cny)
	}
	if requested < 1 {
		return nil, errors.New("充值金额过小")
	}
	amountCny := float64(requested) / 100
	if !exactAmount {
		if amountCny < cfg.MinCny {
			return nil, fmt.Errorf("单笔充值不能低于 ¥%s", store.TrimFloat(cfg.MinCny))
		}
		if amountCny > cfg.MaxCny {
			return nil, fmt.Errorf("单笔充值不能高于 ¥%s", store.TrimFloat(cfg.MaxCny))
		}
	}
	if _, _, err := alipayClient(); err != nil {
		return nil, err
	}
	ttlSeconds := cfg.OrderTTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	cooldownSeconds := cfg.CooldownSeconds
	if exactAmount {
		// 商品直购使用精确售价，结束后立即释放金额；不额外加价。
		cooldownSeconds = 0
	}
	if cooldownSeconds < 0 {
		cooldownSeconds = 0
	}
	offsetMax := int64(cfg.AmountOffsetMaxCents)
	if exactAmount {
		offsetMax = 0
	} else if offsetMax <= 0 {
		offsetMax = 1
	}
	store.ExpireAlipayOrders()
	orderNo := store.NewAlipayOrderNo()
	token := store.AlipayCheckoutToken(orderNo)
	order, err := store.CreateAlipayOrder(
		user.UID,
		orderNo,
		store.HashAlipayToken(token),
		cfg.CollectionCode,
		requested,
		offsetMax,
		int64(ttlSeconds)*1000,
		int64(cooldownSeconds)*1000,
	)
	if err != nil {
		return nil, err
	}
	return order, nil
}

// GetAlipayOrderForUser 校验订单归属。
func GetAlipayOrderForUser(id int, user *model.User) (*model.AlipayOrder, bool) {
	order := store.GetAlipayOrder(id)
	if order == nil {
		return nil, false
	}
	if order.UID != user.UID && user.Role != "admin" {
		return nil, false
	}
	return order, true
}

// GetAlipayCheckout 公开收银台查询，token 即凭证。
func GetAlipayCheckout(token string) (*AlipayCheckoutView, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("收银台链接不正确")
	}
	store.ExpireAlipayOrders()
	order := store.GetAlipayOrderByToken(strings.TrimSpace(token))
	if order == nil {
		return nil, errors.New("订单不存在")
	}
	cfg := store.GetAlipayConfig()
	return &AlipayCheckoutView{
		OrderNo:     order.OrderNo,
		ProductName: cfg.ProductName,
		CollectionCode: func() string {
			if order.CollectionCode != "" {
				return order.CollectionCode
			}
			return cfg.CollectionCode
		}(),
		RequestedCents: order.RequestedCents,
		PayableCents:   order.PayableCents,
		PaymentStatus:  order.PaymentStatus,
		CheckoutStatus: order.CheckoutStatus,
		Status:         order.Status,
		ReceivedCents:  order.ReceivedCents,
		Credited:       order.Credited,
		ExpiresAt:      order.ExpiresAt,
		CreatedAt:      order.CreatedAt,
		PaidAt:         order.PaidAt,
	}, nil
}

// CloseAlipayOrder 关闭未支付订单。
func CloseAlipayOrder(id int, user *model.User) (*model.AlipayOrder, error) {
	order, ok := GetAlipayOrderForUser(id, user)
	if !ok {
		return nil, errors.New("订单不存在或无权操作")
	}
	if order.PaymentStatus == "CONFIRMED" {
		return order, errors.New("订单已支付，不能关闭")
	}
	return store.CloseAlipayOrder(id)
}

// QuickCheckAlipayOrder 在用户收银台轮询时直接查询最近账单，缩短到账展示延迟。
// 与后台轮询共用扫描锁；已有扫描进行中时直接返回本地状态，不阻塞页面。
func QuickCheckAlipayOrder(ctx context.Context, id int) (*model.AlipayOrder, error) {
	order := store.GetAlipayOrder(id)
	if order == nil {
		return nil, errors.New("订单不存在")
	}
	if order.PaymentStatus != "UNPAID" || order.CheckoutStatus != "OPEN" {
		return order, nil
	}
	if !alipayScanMu.TryLock() {
		return order, nil
	}
	defer alipayScanMu.Unlock()

	// 收银台轮询必须快速返回：这里给一个硬上限，避免账单接口慢时把请求拖死。
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	nowMs := time.Now().UnixMilli()
	if nowMs-lastAlipayQuickCheckMs < 3000 {
		return store.GetAlipayOrder(id), nil
	}
	lastAlipayQuickCheckMs = nowMs

	_, client, err := alipayClient()
	if err != nil {
		return order, err
	}
	order = store.GetAlipayOrder(id)
	if order == nil || order.PaymentStatus != "UNPAID" || order.CheckoutStatus != "OPEN" {
		return order, nil
	}
	start := order.CreatedAt - alipayScanOverlapMs
	if start < nowMs-alipayMaxLookbackMs {
		start = nowMs - alipayMaxLookbackMs
	}
	if start >= nowMs {
		start = nowMs - 1000
	}
	result := &AlipayScanResult{}
	if err := scanAlipayWindow(ctx, client, start, nowMs, result); err != nil {
		return store.GetAlipayOrder(id), err
	}
	// scanAlipayWindow 已处理新流水；这里补一次待分配流水，覆盖极短窗口的边界。
	ReconcilePendingAlipay(50)
	FulfillPendingGuestPurchases(10)
	return store.GetAlipayOrder(id), nil
}

// AlipayScanDue 根据当前是否有待支付订单选择活跃/常规查账间隔。
func AlipayScanDue() bool {
	cfg := store.GetAlipayConfig()
	if !cfg.Configured {
		return false
	}
	state := store.GetAlipayScanState()
	if state.LastAttemptAt == 0 {
		return true
	}
	interval := cfg.NormalScanSeconds
	if interval <= 0 {
		interval = 60
	}
	if store.CountOpenAlipayOrders() > 0 {
		interval = cfg.ActiveScanSeconds
		if interval <= 0 {
			interval = 8
		}
	}
	if interval < 5 {
		interval = 5
	}
	return time.Now().UnixMilli()-state.LastAttemptAt >= int64(interval)*1000
}

// ScanAlipay 拉取支付宝账务明细并自动对账。
func ScanAlipay(ctx context.Context) (*AlipayScanResult, error) {
	alipayScanMu.Lock()
	defer alipayScanMu.Unlock()
	return scanAlipayLocked(ctx)
}

func scanAlipayLocked(ctx context.Context) (*AlipayScanResult, error) {
	cfg, client, err := alipayClient()
	if err != nil {
		store.MarkAlipayScanAttempt(err.Error(), "", false, 0)
		return &AlipayScanResult{Status: "failed", Error: err.Error()}, err
	}
	store.ExpireAlipayOrders()
	nowMs := time.Now().UnixMilli()
	end := nowMs - int64(cfg.SafetyLagSeconds)*1000
	state := store.GetAlipayScanState()
	start := state.CursorAt - alipayScanOverlapMs
	lookback := cfg.LookbackSeconds
	if lookback <= 0 {
		lookback = 3600
	}
	if state.CursorAt == 0 || start <= 0 {
		start = end - int64(lookback)*1000
	}
	if end <= start {
		store.MarkAlipayScanAttempt("", "", true, state.CursorAt)
		return &AlipayScanResult{Status: "skipped", Cursor: state.CursorAt}, nil
	}
	if end-start > alipayMaxLookbackMs {
		start = end - alipayMaxLookbackMs
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Minute {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}

	result := &AlipayScanResult{Status: "ok", Cursor: start}
	for chunkStart := start; chunkStart < end; {
		chunkEnd := chunkStart + alipayMaxChunkMs
		if chunkEnd > end {
			chunkEnd = end
		}
		if err := scanAlipayWindow(ctx, client, chunkStart, chunkEnd, result); err != nil {
			result.Status = "failed"
			result.Error = err.Error()
			store.MarkAlipayScanAttempt(err.Error(), result.TraceID, false, 0)
			return result, err
		}
		chunkStart = chunkEnd
	}
	// 补一轮待匹配流水，覆盖上次已入库但匹配失败/歧义的情况。
	settled, unmatched, ambiguous := ReconcilePendingAlipay(200)
	result.Settled += settled
	result.Unmatched += unmatched
	result.Ambiguous += ambiguous
	FulfillPendingGuestPurchases(20)
	result.Cursor = end
	store.MarkAlipayScanAttempt("", result.TraceID, true, end)
	return result, nil
}

func scanAlipayWindow(ctx context.Context, client *alipayv3.Client, startMs, endMs int64, result *AlipayScanResult) error {
	cst := time.FixedZone("CST", 8*60*60)
	startText := time.UnixMilli(startMs).In(cst).Format("2006-01-02 15:04:05")
	endText := time.UnixMilli(endMs).In(cst).Format("2006-01-02 15:04:05")
	if startText >= endText {
		return nil
	}
	pageNo := 1
	for pageNo <= 100 {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := client.QueryAccountLog(ctx, startText, endText, pageNo, 500)
		if err != nil {
			return err
		}
		result.Pages++
		if page.TraceID != "" {
			result.TraceID = page.TraceID
		}
		for _, detail := range page.Details {
			state := "IGNORED"
			if detail.Direction == "CREDIT" {
				state = "UNALLOCATED"
			}
			id, inserted, err := store.InsertAlipayLedgerEntry(model.AlipayLedgerEntry{
				ExternalEventID:   detail.ExternalEventID,
				AccountLogID:      detail.AccountLogID,
				OccurredAt:        detail.OccurredAt,
				OccurredPrecision: detail.PrecisionMs,
				AmountCents:       detail.AmountCents,
				Direction:         detail.Direction,
				AlipayOrderNo:     detail.AlipayOrderNo,
				MerchantOrderNo:   detail.MerchantOrderNo,
				TransMemo:         detail.TransMemo,
				OtherAccount:      detail.OtherAccount,
				State:             state,
				RawJSON:           detail.RawJSON,
			})
			if err != nil {
				return err
			}
			if !inserted {
				continue
			}
			result.Entries++
			if detail.Direction != "CREDIT" {
				continue
			}
			outcome, err := store.ReconcileAlipayLedgerEntry(id)
			if err != nil {
				return err
			}
			switch outcome.Kind {
			case "settled":
				result.Settled++
			case "unmatched":
				result.Unmatched++
			case "ambiguous":
				result.Ambiguous++
			}
		}
		if len(page.Details) == 0 || page.PageNo*page.PageSize >= page.TotalSize {
			break
		}
		pageNo++
	}
	return nil
}

// ReconcilePendingAlipay 重新尝试匹配未分配流水。
func ReconcilePendingAlipay(limit int) (settled, unmatched, ambiguous int) {
	entries := store.ListAlipayPendingLedger(limit)
	for _, entry := range entries {
		outcome, err := store.ReconcileAlipayLedgerEntry(int64(entry.ID))
		if err != nil {
			continue
		}
		switch outcome.Kind {
		case "settled":
			settled++
		case "unmatched":
			unmatched++
		case "ambiguous":
			ambiguous++
		}
	}
	return
}

// ManualMatchAlipay 管理员手工关联流水。
func ManualMatchAlipay(orderID int, ledgerID int64) (*model.AlipayOrder, string, error) {
	entry := store.GetAlipayLedgerEntry(ledgerID)
	order := store.GetAlipayOrder(orderID)
	if entry == nil || order == nil {
		return nil, "", errors.New("订单或流水不存在")
	}
	warning := ""
	if entry.AmountCents != order.PayableCents {
		warning = fmt.Sprintf("流水金额 ¥%s 与应付金额 ¥%s 不一致，已按申请金额入账",
			store.TrimFloat(float64(entry.AmountCents)/100), store.TrimFloat(float64(order.PayableCents)/100))
	}
	updated, err := store.ManualMatchAlipay(orderID, ledgerID)
	if err == nil {
		_ = FulfillGuestPurchaseByPayment("alipay", orderID)
	}
	return updated, warning, err
}

// TestAlipayConnection 查询最近 5 分钟账务明细，验证密钥与权限。
func TestAlipayConnection(ctx context.Context) (map[string]any, error) {
	cfg, client, err := alipayClient()
	if err != nil {
		return nil, err
	}
	cst := time.FixedZone("CST", 8*60*60)
	endMs := time.Now().UnixMilli() - int64(cfg.SafetyLagSeconds)*1000
	startMs := endMs - 5*60*1000
	start := time.UnixMilli(startMs).In(cst).Format("2006-01-02 15:04:05")
	end := time.UnixMilli(endMs).In(cst).Format("2006-01-02 15:04:05")
	page, err := client.QueryAccountLog(ctx, start, end, 1, 20)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"entries": len(page.Details), "total": page.TotalSize,
		"traceId": page.TraceID, "windowStart": start, "windowEnd": end,
	}, nil
}

// ExpireAlipayOrders 释放已过期订单的金额槽。
func ExpireAlipayOrders() int {
	return store.ExpireAlipayOrders()
}
