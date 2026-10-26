package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"hstore/backend/internal/model"
	payclient "hstore/backend/internal/perpay"
	"hstore/backend/internal/store"
)

const perpayRequestTimeout = 20 * time.Second

type perpayClient struct {
	cfg    store.PerPaySecretConfig
	client *payclient.Client
}

func newPerPayClient() (*perpayClient, error) {
	cfg := store.GetPerPaySecretConfig()
	if !cfg.Configured {
		return nil, errors.New("尚未配置 PerPay 地址、客户端 ID 或 API 密钥")
	}
	client, err := payclient.NewClient(payclient.Config{
		BaseURL:       cfg.BaseURL,
		ClientID:      cfg.ClientID,
		APISecret:     cfg.APISecret,
		WebhookSecret: cfg.WebhookSecret,
	})
	if err != nil {
		return nil, err
	}
	return &perpayClient{cfg: cfg, client: client}, nil
}

func perpayPublicOrigin(raw string) (string, error) {
	origin, err := payclient.ValidateOrigin(raw)
	if err != nil {
		return "", fmt.Errorf("站点公开地址无效：%v", err)
	}
	return origin, nil
}

// ValidatePerPayConfigPatch 校验保存在 sync_meta 的配置字段。
func ValidatePerPayConfigPatch(patch map[string]any) error {
	if v, ok := patch["baseUrl"].(string); ok && strings.TrimSpace(v) != "" {
		if _, err := payclient.ValidateOrigin(v); err != nil {
			return err
		}
	}
	if v, ok := patch["clientId"].(string); ok && strings.TrimSpace(v) != "" {
		if err := payclient.ValidateClientID(v); err != nil {
			return err
		}
	}
	if v, ok := patch["apiSecret"].(string); ok && strings.TrimSpace(v) != "" {
		if _, err := payclient.DecodeSecret(v, "API 密钥"); err != nil {
			return err
		}
	}
	if v, ok := patch["webhookSecret"].(string); ok && strings.TrimSpace(v) != "" {
		if _, err := payclient.DecodeSecret(v, "通知密钥"); err != nil {
			return err
		}
	}
	if v, ok := patch["publicBaseUrl"].(string); ok && strings.TrimSpace(v) != "" {
		if _, err := perpayPublicOrigin(v); err != nil {
			return err
		}
	}
	if v, ok := patch["productName"].(string); ok {
		name := strings.TrimSpace(v)
		if name != "" && len([]rune(name)) > 200 {
			return errors.New("商品名称最多 200 个字符")
		}
	}
	minCny, _ := toFloat(patch["minCny"])
	maxCny, _ := toFloat(patch["maxCny"])
	if minCny > 0 && maxCny > 0 && minCny > maxCny {
		return errors.New("最小充值金额不能大于最大充值金额")
	}
	if minCny > 0 && minCny < 0.01 {
		return errors.New("最小充值金额至少为 0.01 元")
	}
	if maxCny > 99999999.98 {
		return errors.New("最大充值金额超过 PerPay 单笔上限")
	}
	return nil
}

func toFloat(v any) (float64, bool) {
	f, ok := store.ToFloat(v)
	return f, ok
}

// CreatePerPayOrder 创建本地订单后调用 PerPay 创建或幂等重放。
// 返回的 order 在失败时也可能非空，便于前端使用同一商户订单号重试。
func CreatePerPayOrder(user *model.User, cny float64) (*model.PerPayOrder, error) {
	cfg := store.GetPerPaySecretConfig()
	if !cfg.Enabled {
		return nil, errors.New("支付宝充值未开启")
	}
	if !cfg.Configured {
		return nil, errors.New("管理员尚未配置 PerPay")
	}
	if math.IsNaN(cny) || math.IsInf(cny, 0) || cny <= 0 {
		return nil, errors.New("请输入充值金额")
	}
	amountCents := int64(math.Round(cny * 100))
	if amountCents < 1 {
		return nil, errors.New("充值金额过小")
	}
	if amountCents > 9999999998 {
		return nil, errors.New("充值金额超过 PerPay 单笔上限")
	}
	amountCny := float64(amountCents) / 100
	if amountCny < cfg.MinCny {
		return nil, fmt.Errorf("单笔充值不能低于 ¥%s", store.TrimFloat(cfg.MinCny))
	}
	if amountCny > cfg.MaxCny {
		return nil, fmt.Errorf("单笔充值不能高于 ¥%s", store.TrimFloat(cfg.MaxCny))
	}

	if _, err := newPerPayClient(); err != nil {
		return nil, err
	}

	merchantNo := store.NewPerPayMerchantNo()
	for i := 0; i < 5 && store.GetPerPayOrderByMerchantNo(merchantNo) != nil; i++ {
		merchantNo = store.NewPerPayMerchantNo()
	}

	productName := sanitizePerPayText(cfg.ProductName, 200)
	if productName == "" {
		productName = "账户余额充值"
	}
	note := sanitizePerPayText(fmt.Sprintf("用户 %s (UID %d)", user.Username, user.UID), 500)
	payload := payclient.CreateOrderRequest{
		IdempotencyKey:  merchantNo,
		MerchantOrderNo: merchantNo,
		AmountCents:     amountCents,
		ProductName:     productName,
		Note:            &note,
	}
	if cfg.WebhookReady {
		if origin, err := perpayPublicOrigin(cfg.PublicBaseURL); err == nil {
			notifyURL := origin + "/api/webhooks/perpay"
			payload.NotifyURL = &notifyURL
		}
	}
	if cfg.ReturnEnabled && strings.TrimSpace(cfg.PublicBaseURL) != "" {
		if origin, err := perpayPublicOrigin(cfg.PublicBaseURL); err == nil {
			returnURL := origin + "/account/recharge?perpay=" + url.QueryEscape(merchantNo)
			payload.ReturnURL = &returnURL
		}
	}
	requestJSON, _ := json.Marshal(payload)

	id, err := store.InsertPerPayOrder(user.UID, merchantNo, amountCents, string(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("创建本地充值订单失败：%w", err)
	}

	return submitPerPayOrder(id, payload)
}

// RetryPerPayOrder 使用创建时保存的原始请求体重试，保持幂等键不变。
func RetryPerPayOrder(id int, user *model.User, isAdmin bool) (*model.PerPayOrder, error) {
	local := store.GetPerPayOrder(id)
	if local == nil {
		return nil, errors.New("订单不存在")
	}
	if !isAdmin && local.UID != user.UID {
		return nil, errors.New("无权操作")
	}
	if local.PaidAt != nil {
		return local, nil
	}
	if local.Status == "waiting" && local.PerPayOrderID != nil {
		order, _ := SyncPerPayOrder(id, true)
		return order, nil
	}
	if local.RequestJSON == "" {
		return local, errors.New("订单缺少原始请求，无法重试")
	}
	var payload payclient.CreateOrderRequest
	if err := json.Unmarshal([]byte(local.RequestJSON), &payload); err != nil {
		return local, errors.New("订单原始请求已损坏")
	}
	if payload.MerchantOrderNo != local.MerchantOrderNo {
		return local, errors.New("订单原始请求与本地记录不一致")
	}
	store.ResetPerPayOrderForRetry(id)
	return submitPerPayOrder(id, payload)
}

func submitPerPayOrder(id int, payload payclient.CreateOrderRequest) (*model.PerPayOrder, error) {
	client, err := newPerPayClient()
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	ctx, cancel := context.WithTimeout(context.Background(), perpayRequestTimeout)
	defer cancel()
	remote, err := client.client.CreateOrder(ctx, payload)
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	if remote.MerchantOrderNo != payload.MerchantOrderNo {
		err := errors.New("PerPay 返回了另一笔业务订单")
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	snap, err := snapshotFromRemote(client.cfg, remote)
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	order, err := store.ApplyPerPaySnapshot(id, snap, "api")
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	return order, nil
}

// SyncPerPayOrder 服务端查单并推进本地状态；force=false 时对 5 秒内查过的订单直接返回本地状态。
func SyncPerPayOrder(id int, force bool) (*model.PerPayOrder, error) {
	local := store.GetPerPayOrder(id)
	if local == nil {
		return nil, errors.New("订单不存在")
	}
	if local.PaidAt != nil && !force {
		return local, nil
	}
	if !force && local.LastCheckedAt > 0 && time.Now().UnixMilli()-local.LastCheckedAt < 5000 {
		return local, nil
	}
	if !store.IsPerPayConfigured() {
		return local, errors.New("尚未配置 PerPay")
	}
	client, err := newPerPayClient()
	if err != nil {
		return local, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), perpayRequestTimeout)
	defer cancel()

	var remote *payclient.Order
	if local.PerPayOrderID != nil {
		remote, err = client.client.GetOrder(ctx, *local.PerPayOrderID)
	} else {
		remote, err = client.client.GetOrderByMerchantNo(ctx, local.MerchantOrderNo)
	}
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	if remote.MerchantOrderNo != local.MerchantOrderNo {
		err := errors.New("PerPay 返回了另一笔业务订单")
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	snap, err := snapshotFromRemote(client.cfg, remote)
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	order, err := store.ApplyPerPaySnapshot(id, snap, "api")
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	return order, nil
}

// CheckPerPayOrders 轮询所有等待中的订单。
func CheckPerPayOrders(limit int) (int, []string) {
	store.ExpirePerPayOrders()
	if !store.IsPerPayConfigured() {
		return 0, nil
	}
	orders := store.ListPendingPerPayOrders(limit)
	checked := 0
	errs := []string{}
	for _, o := range orders {
		if o.Status == "creating" && o.PerPayOrderID == nil {
			// 创建请求是否已到达 PerPay 未知；不自动重复提交，保留给用户/管理员重试。
			continue
		}
		if _, err := SyncPerPayOrder(o.ID, true); err != nil {
			errs = append(errs, fmt.Sprintf("#%d %s: %v", o.ID, o.MerchantOrderNo, err))
		}
		checked++
	}
	return checked, errs
}

// ClosePerPayOrder 关闭收银台；已付款订单不允许关闭。
func ClosePerPayOrder(id int, user *model.User, isAdmin bool) (*model.PerPayOrder, error) {
	local := store.GetPerPayOrder(id)
	if local == nil {
		return nil, errors.New("订单不存在")
	}
	if !isAdmin && local.UID != user.UID {
		return nil, errors.New("无权操作")
	}
	if local.PaidAt != nil || local.Status == "paid" || local.Status == "disputed" {
		return local, errors.New("订单已支付，不能关闭")
	}
	if local.PerPayOrderID == nil {
		store.MarkPerPayOrderClosed(id)
		return store.GetPerPayOrder(id), nil
	}
	client, err := newPerPayClient()
	if err != nil {
		return local, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), perpayRequestTimeout)
	defer cancel()
	remote, err := client.client.CloseOrder(ctx, *local.PerPayOrderID)
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	snap, err := snapshotFromRemote(client.cfg, remote)
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	order, err := store.ApplyPerPaySnapshot(id, snap, "api")
	if err != nil {
		store.SetPerPayOrderError(id, err.Error())
		return store.GetPerPayOrder(id), err
	}
	return order, nil
}

// SimulatePerPayOrder 管理员测试：直接按 CONFIRMED 入账，不经过真实支付。
func SimulatePerPayOrder(id int) (*model.PerPayOrder, error) {
	local := store.GetPerPayOrder(id)
	if local == nil {
		return nil, errors.New("订单不存在")
	}
	if local.PaidAt != nil {
		return local, errors.New("订单已入账")
	}
	orderID := payclient.NewUUID()
	if local.PerPayOrderID != nil {
		orderID = *local.PerPayOrderID
	}
	received := local.AmountCents
	expires := time.Now().UnixMilli()
	version := local.OrderVersion + 1
	if version < 1 {
		version = 1
	}
	snap := store.PerPaySnapshot{
		OrderID:              orderID,
		MerchantOrderNo:      local.MerchantOrderNo,
		RequestedAmountCents: local.AmountCents,
		PayableAmountCents:   local.AmountCents,
		ReceivedAmountCents:  &received,
		PaymentStatus:        "CONFIRMED",
		PaymentBasis:         "MANUAL",
		RefundStatus:         "NONE",
		CheckoutStatus:       "CLOSED",
		CheckoutURL:          local.CheckoutURL,
		CheckoutExpiresAt:    &expires,
		Version:              version,
	}
	return store.ApplyPerPaySnapshot(id, snap, "simulate")
}

// TestPerPayConnection 调用签名接口检查 PerPay 接入状态。
func TestPerPayConnection() (*payclient.SystemStatus, error) {
	client, err := newPerPayClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), perpayRequestTimeout)
	defer cancel()
	return client.client.GetSystemStatus(ctx)
}

// PerPayWebhookSnapshot 把已验签通知转换为本地快照。
func PerPayWebhookSnapshot(event payclient.WebhookEvent) store.PerPaySnapshot {
	return store.PerPaySnapshot{
		OrderID:              event.OrderID,
		MerchantOrderNo:      event.MerchantOrderNo,
		RequestedAmountCents: event.RequestedAmountCents,
		PayableAmountCents:   event.PayableAmountCents,
		ReceivedAmountCents:  event.ReceivedAmountCents,
		PaymentStatus:        event.PaymentStatus,
		PaymentBasis:         event.PaymentBasis,
		RefundStatus:         event.RefundStatus,
		CheckoutStatus:       "",
		CheckoutURL:          "",
		Version:              event.OrderVersion,
	}
}

func snapshotFromRemote(cfg store.PerPaySecretConfig, remote *payclient.Order) (store.PerPaySnapshot, error) {
	checkoutURL := remote.Checkout.CheckoutURL
	if checkoutURL != "" {
		if err := validateCheckoutURL(cfg.BaseURL, checkoutURL); err != nil {
			return store.PerPaySnapshot{}, err
		}
	}
	var expires *int64
	if remote.Checkout.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, remote.Checkout.ExpiresAt); err == nil {
			v := t.UnixMilli()
			expires = &v
		}
	}
	received := remote.ReceivedAmountCents
	if received == nil {
		received = remote.Payment.ReceivedAmountCents
	}
	return store.PerPaySnapshot{
		OrderID:              remote.OrderID,
		MerchantOrderNo:      remote.MerchantOrderNo,
		RequestedAmountCents: remote.RequestedAmountCents,
		PayableAmountCents:   remote.PayableAmountCents,
		ReceivedAmountCents:  received,
		PaymentStatus:        remote.Payment.Status,
		PaymentBasis:         remote.Payment.Basis,
		RefundStatus:         remote.Refund.Status,
		CheckoutStatus:       remote.Checkout.Status,
		CheckoutURL:          checkoutURL,
		CheckoutExpiresAt:    expires,
		Version:              remote.Version,
	}, nil
}

// sanitizePerPayText 去掉控制字符并按 Unicode 字符数截断，避免被 PerPay 拒绝。
func sanitizePerPayText(value string, maxRunes int) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value))
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes)
}

func validateCheckoutURL(baseURL, raw string) error {
	base, err := payclient.ValidateOrigin(baseURL)
	if err != nil {
		return err
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("PerPay 收银台地址不正确")
	}
	if u.Scheme+"://"+u.Host != base {
		return errors.New("PerPay 收银台地址不属于配置的 PerPay 来源")
	}
	if !strings.HasPrefix(u.Path, "/checkout/") {
		return errors.New("PerPay 收银台地址路径不正确")
	}
	return nil
}
