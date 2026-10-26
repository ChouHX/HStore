package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// ErrPerPayWebhookConflict 表示同一事件 ID 对应不同正文。
var ErrPerPayWebhookConflict = errors.New("相同事件 ID 对应不同正文，拒绝确认")

// PerPayConfig 是支付宝（PerPay）充值配置。密钥不通过 JSON 返回。
type PerPayConfig struct {
	Enabled           bool    `json:"enabled"`
	BaseURL           string  `json:"baseUrl"`
	ClientID          string  `json:"clientId"`
	APISecretMasked   string  `json:"apiSecretMasked"`
	HasAPISecret      bool    `json:"hasApiSecret"`
	WebhookSecretMask string  `json:"webhookSecretMasked"`
	HasWebhookSecret  bool    `json:"hasWebhookSecret"`
	PublicBaseURL     string  `json:"publicBaseUrl"`
	MinCny            float64 `json:"minCny"`
	MaxCny            float64 `json:"maxCny"`
	ProductName       string  `json:"productName"`
	NotifyEnabled     bool    `json:"notifyEnabled"`
	ReturnEnabled     bool    `json:"returnEnabled"`
	Configured        bool    `json:"configured"`
	WebhookReady      bool    `json:"webhookReady"`
}

// PerPaySecretConfig 是内部使用的完整配置（含密钥）。
type PerPaySecretConfig struct {
	PerPayConfig
	APISecret     string `json:"-"`
	WebhookSecret string `json:"-"`
}

// GetPerPayConfig 读取 PerPay 配置（脱敏）。
func GetPerPayConfig() PerPayConfig {
	full := GetPerPaySecretConfig()
	return full.PerPayConfig
}

// GetPerPaySecretConfig 读取完整 PerPay 配置。
func GetPerPaySecretConfig() PerPaySecretConfig {
	cfg := PerPaySecretConfig{}
	cfg.Enabled = db.GetMeta("perpay_enabled") == "1"
	cfg.BaseURL = db.GetMeta("perpay_base_url")
	cfg.ClientID = db.GetMeta("perpay_client_id")
	cfg.PublicBaseURL = db.GetMeta("perpay_public_base_url")
	cfg.APISecret = db.GetMeta("perpay_api_secret")
	cfg.WebhookSecret = db.GetMeta("perpay_webhook_secret")
	cfg.MinCny = parseFloatDefault(db.GetMeta("perpay_min_cny"), 10)
	cfg.MaxCny = parseFloatDefault(db.GetMeta("perpay_max_cny"), 5000)
	cfg.ProductName = db.GetMeta("perpay_product_name")
	if cfg.ProductName == "" {
		cfg.ProductName = "账户余额充值"
	}
	cfg.NotifyEnabled = db.GetMeta("perpay_notify_enabled") == "1"
	cfg.ReturnEnabled = db.GetMeta("perpay_return_enabled") == "1"
	cfg.Configured = cfg.BaseURL != "" && cfg.ClientID != "" && cfg.APISecret != ""
	cfg.HasAPISecret = cfg.APISecret != ""
	cfg.HasWebhookSecret = cfg.WebhookSecret != ""
	cfg.APISecretMasked = MaskSecret(cfg.APISecret)
	cfg.WebhookSecretMask = MaskSecret(cfg.WebhookSecret)
	cfg.WebhookReady = cfg.NotifyEnabled && cfg.PublicBaseURL != "" && cfg.WebhookSecret != ""
	return cfg
}

// IsPerPayConfigured 是否已填写完整接入参数。
func IsPerPayConfigured() bool {
	return GetPerPaySecretConfig().Configured
}

func parseFloatDefault(v string, def float64) float64 {
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func boolMeta(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// SavePerPayConfig 保存 PerPay 配置；空密钥表示不修改。
func SavePerPayConfig(patch map[string]any) {
	if v, ok := patch["enabled"].(bool); ok {
		_ = db.SetMeta("perpay_enabled", boolMeta(v))
	}
	if v, ok := patch["notifyEnabled"].(bool); ok {
		_ = db.SetMeta("perpay_notify_enabled", boolMeta(v))
	}
	if v, ok := patch["returnEnabled"].(bool); ok {
		_ = db.SetMeta("perpay_return_enabled", boolMeta(v))
	}
	for key, metaKey := range map[string]string{
		"baseUrl":       "perpay_base_url",
		"clientId":      "perpay_client_id",
		"publicBaseUrl": "perpay_public_base_url",
		"productName":   "perpay_product_name",
	} {
		if v, ok := patch[key].(string); ok {
			_ = db.SetMeta(metaKey, strings.TrimSpace(v))
		}
	}
	if v, ok := patch["minCny"].(float64); ok && v > 0 {
		_ = db.SetMeta("perpay_min_cny", strconv.FormatFloat(v, 'f', -1, 64))
	}
	if v, ok := patch["maxCny"].(float64); ok && v > 0 {
		_ = db.SetMeta("perpay_max_cny", strconv.FormatFloat(v, 'f', -1, 64))
	}
	if v, ok := patch["apiSecret"].(string); ok && strings.TrimSpace(v) != "" {
		_ = db.SetMeta("perpay_api_secret", strings.TrimSpace(v))
	}
	if v, ok := patch["webhookSecret"].(string); ok && strings.TrimSpace(v) != "" {
		_ = db.SetMeta("perpay_webhook_secret", strings.TrimSpace(v))
	}
}

// NewPerPayMerchantNo 生成 1–64 位、符合 PerPay 约束的商户订单号。
func NewPerPayMerchantNo() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return "PP" + time.Now().Format("20060102150405") + strings.ToUpper(hex.EncodeToString(buf))
}

// InsertPerPayOrder 在调用 PerPay 前先落库，保证超时重试使用同一业务单号。
func InsertPerPayOrder(uid int, merchantNo string, amountCents int64, requestJSON string) (int, error) {
	res, err := db.Must().Exec(`INSERT INTO perpay_orders(uid,merchant_order_no,amount_cny,amount_cents,status,request_json,created_at,updated_at)
		VALUES(?,?,?,?,'creating',?,?,?)`,
		uid, merchantNo, float64(amountCents)/100, amountCents, requestJSON, now(), now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

const perpayOrderBaseCols = `o.id, o.uid, o.merchant_order_no, o.perpay_order_id, o.amount_cny, o.amount_cents,
	o.payable_cents, o.received_cents, o.status, o.payment_basis, o.checkout_url, o.checkout_status,
	o.checkout_expires_at, o.order_version, o.last_error, o.request_json, o.credited, o.created_at, o.updated_at,
	o.paid_at, o.last_checked_at`

func scanPerPayOrder(sc interface{ Scan(...any) error }) (model.PerPayOrder, error) {
	var o model.PerPayOrder
	var perpayID sql.NullString
	var checkoutExpires, paidAt, payable, received sql.NullInt64
	var creditedValue sql.NullFloat64
	var lastChecked sql.NullInt64
	err := sc.Scan(&o.ID, &o.UID, &o.MerchantOrderNo, &perpayID, &o.AmountCny, &o.AmountCents,
		&payable, &received, &o.Status, &o.PaymentBasis, &o.CheckoutURL, &o.CheckoutStatus,
		&checkoutExpires, &o.OrderVersion, &o.LastError, &o.RequestJSON, &creditedValue,
		&o.CreatedAt, &o.UpdatedAt, &paidAt, &lastChecked)
	if perpayID.Valid {
		v := perpayID.String
		o.PerPayOrderID = &v
	}
	if payable.Valid {
		v := payable.Int64
		o.PayableCents = &v
	}
	if received.Valid {
		v := received.Int64
		o.ReceivedCents = &v
	}
	if checkoutExpires.Valid {
		v := checkoutExpires.Int64
		o.CheckoutExpiresAt = &v
	}
	if creditedValue.Valid {
		v := creditedValue.Float64
		o.Credited = &v
	}
	if paidAt.Valid {
		v := paidAt.Int64
		o.PaidAt = &v
	}
	if lastChecked.Valid {
		o.LastCheckedAt = lastChecked.Int64
	}
	return o, err
}

// GetPerPayOrder 按本地 ID 读取。
func GetPerPayOrder(id int) *model.PerPayOrder {
	row := db.Must().QueryRow("SELECT "+perpayOrderBaseCols+" FROM perpay_orders o WHERE id=?", id)
	o, err := scanPerPayOrder(row)
	if err != nil {
		return nil
	}
	return &o
}

// GetPerPayOrderByMerchantNo 按商户订单号读取。
func GetPerPayOrderByMerchantNo(no string) *model.PerPayOrder {
	row := db.Must().QueryRow("SELECT "+perpayOrderBaseCols+" FROM perpay_orders o WHERE merchant_order_no=?", no)
	o, err := scanPerPayOrder(row)
	if err != nil {
		return nil
	}
	return &o
}

// ListPerPayOrders 返回订单列表；uid 为 0 时返回全部。
func ListPerPayOrders(uid, limit int) []model.PerPayOrder {
	if limit <= 0 {
		limit = 100
	}
	query := "SELECT " + perpayOrderBaseCols + ", COALESCE(u.username,'') FROM perpay_orders o " +
		"LEFT JOIN users u ON u.uid = o.uid "
	args := []any{}
	if uid > 0 {
		query += "WHERE o.uid=? "
		args = append(args, uid)
	}
	query += "ORDER BY o.id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.Must().Query(query, args...)
	if err != nil {
		return []model.PerPayOrder{}
	}
	defer rows.Close()
	out := []model.PerPayOrder{}
	for rows.Next() {
		var o model.PerPayOrder
		var memberID sql.NullString
		var checkoutExpires, paidAt, payable, received sql.NullInt64
		var credited sql.NullFloat64
		var lastChecked sql.NullInt64
		var username string
		if err := rows.Scan(&o.ID, &o.UID, &o.MerchantOrderNo, &memberID, &o.AmountCny, &o.AmountCents,
			&payable, &received, &o.Status, &o.PaymentBasis, &o.CheckoutURL, &o.CheckoutStatus,
			&checkoutExpires, &o.OrderVersion, &o.LastError, &o.RequestJSON, &credited,
			&o.CreatedAt, &o.UpdatedAt, &paidAt, &lastChecked, &username); err != nil {
			continue
		}
		o.Username = username
		if memberID.Valid {
			v := memberID.String
			o.PerPayOrderID = &v
		}
		if payable.Valid {
			v := payable.Int64
			o.PayableCents = &v
		}
		if received.Valid {
			v := received.Int64
			o.ReceivedCents = &v
		}
		if checkoutExpires.Valid {
			v := checkoutExpires.Int64
			o.CheckoutExpiresAt = &v
		}
		if credited.Valid {
			v := credited.Float64
			o.Credited = &v
		}
		if paidAt.Valid {
			v := paidAt.Int64
			o.PaidAt = &v
		}
		if lastChecked.Valid {
			o.LastCheckedAt = lastChecked.Int64
		}
		out = append(out, o)
	}
	return out
}

// ListPendingPerPayOrders 返回需要继续查单的本地订单。
// 等待中订单每轮查询；已过期/关闭的订单在 24 小时内每 5 分钟补查一次，
// 用于接住“有效期内付款、较晚才确认”的迟到流水。
func ListPendingPerPayOrders(limit int) []model.PerPayOrder {
	if limit <= 0 {
		limit = 30
	}
	nowMs := now()
	rows, err := db.Must().Query(`SELECT `+perpayOrderBaseCols+` FROM perpay_orders o
		WHERE o.paid_at IS NULL AND (
			o.status IN ('waiting','creating')
			OR (o.status IN ('expired','closed') AND o.created_at > ? AND o.last_checked_at < ?)
		)
		ORDER BY CASE WHEN o.status IN ('waiting','creating') THEN 0 ELSE 1 END,
			CASE WHEN o.status IN ('waiting','creating') THEN o.id ELSE -o.id END ASC
		LIMIT ?`, nowMs-24*60*60*1000, nowMs-5*60*1000, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []model.PerPayOrder{}
	for rows.Next() {
		if o, err := scanPerPayOrder(rows); err == nil {
			out = append(out, o)
		}
	}
	return out
}

// SetPerPayOrderError 记录创建/查询错误；创建阶段失败会标记 failed 以便重试。
func SetPerPayOrderError(id int, message string) {
	if len(message) > 500 {
		message = message[:500]
	}
	_, _ = db.Must().Exec(`UPDATE perpay_orders SET last_error=?, status=CASE WHEN status='creating' THEN 'failed' ELSE status END,
		updated_at=?, last_checked_at=? WHERE id=? AND paid_at IS NULL`, message, now(), now(), id)
}

// ResetPerPayOrderForRetry 允许使用原请求体重试创建。
func ResetPerPayOrderForRetry(id int) {
	_, _ = db.Must().Exec(`UPDATE perpay_orders SET status='creating', last_error='', updated_at=?
		WHERE id=? AND paid_at IS NULL AND status IN ('creating','failed')`, now(), id)
}

// MarkPerPayOrderClosed 本地标记关闭（没有 PerPay 订单 ID 时使用）。
func MarkPerPayOrderClosed(id int) {
	_, _ = db.Must().Exec(`UPDATE perpay_orders SET status='closed', updated_at=?
		WHERE id=? AND paid_at IS NULL AND status NOT IN ('paid','disputed')`, now(), id)
}

// ExpirePerPayOrders 把已过收银台有效期的等待订单标记为过期；迟到付款仍可由回调/查单补入账。
func ExpirePerPayOrders() int {
	res, err := db.Must().Exec(`UPDATE perpay_orders SET status='expired', updated_at=?
		WHERE status='waiting' AND paid_at IS NULL AND checkout_expires_at IS NOT NULL AND checkout_expires_at < ?`, now(), now())
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// PerPaySnapshot 是本地需要的 PerPay 订单状态快照。
type PerPaySnapshot struct {
	OrderID              string
	MerchantOrderNo      string
	RequestedAmountCents int64
	PayableAmountCents   int64
	ReceivedAmountCents  *int64
	PaymentStatus        string
	PaymentBasis         string
	RefundStatus         string
	CheckoutStatus       string
	CheckoutURL          string
	CheckoutExpiresAt    *int64
	Version              int
}

// ApplyPerPaySnapshot 在一个事务内更新本地订单，CONFIRMED 时只入账一次。
func ApplyPerPaySnapshot(id int, snap PerPaySnapshot, source string) (*model.PerPayOrder, error) {
	d := db.Must()
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	local, err := getPerPayOrderTx(tx, id)
	if err != nil {
		return nil, err
	}
	disposition, err := applyPerPaySnapshotTx(tx, local, snap, source)
	if err != nil {
		return nil, err
	}
	if disposition == "ignored_older_version" {
		_, _ = tx.Exec("UPDATE perpay_orders SET last_checked_at=? WHERE id=?", now(), id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetPerPayOrder(id), nil
}

// ReceivePerPayWebhook 幂等接收通知，并在同一事务内完成状态推进与入账。
func ReceivePerPayWebhook(eventID, deliveryID, digest, eventType string, snap PerPaySnapshot) (*model.PerPayOrder, string, error) {
	d := db.Must()
	tx, err := d.Begin()
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()

	var existingDigest string
	err = tx.QueryRow("SELECT body_digest FROM perpay_webhook_events WHERE event_id=?", eventID).Scan(&existingDigest)
	if err == nil {
		if existingDigest != digest {
			return nil, "", ErrPerPayWebhookConflict
		}
		_ = tx.Commit()
		return nil, "duplicate", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, "", err
	}

	local, err := getPerPayOrderByMerchantNoTx(tx, snap.MerchantOrderNo)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec(`INSERT INTO perpay_webhook_events(event_id,event_type,merchant_order_no,delivery_id,body_digest,order_version,disposition,received_at)
			VALUES(?,?,?,?,?,?,'ignored_unknown_order',?)`, eventID, eventType, snap.MerchantOrderNo, deliveryID, digest, snap.Version, now()); err != nil {
			return nil, "", err
		}
		if err := tx.Commit(); err != nil {
			return nil, "", err
		}
		return nil, "ignored_unknown_order", nil
	}
	if err != nil {
		return nil, "", err
	}

	disposition, err := applyPerPaySnapshotTx(tx, local, snap, "webhook")
	if err != nil {
		return nil, "", err
	}
	if _, err := tx.Exec(`INSERT INTO perpay_webhook_events(event_id,event_type,merchant_order_no,delivery_id,body_digest,order_version,disposition,received_at)
		VALUES(?,?,?,?,?,?,?,?)`, eventID, eventType, snap.MerchantOrderNo, deliveryID, digest, snap.Version, disposition, now()); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	return GetPerPayOrder(local.ID), disposition, nil
}

// ListPerPayWebhookEvents 返回已接收通知记录。
func ListPerPayWebhookEvents(limit int) []model.PerPayWebhookEvent {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Must().Query(`SELECT id,event_id,event_type,merchant_order_no,delivery_id,body_digest,
		order_version,disposition,received_at FROM perpay_webhook_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return []model.PerPayWebhookEvent{}
	}
	defer rows.Close()
	out := []model.PerPayWebhookEvent{}
	for rows.Next() {
		var e model.PerPayWebhookEvent
		if rows.Scan(&e.ID, &e.EventID, &e.EventType, &e.MerchantOrderNo, &e.DeliveryID, &e.BodyDigest,
			&e.OrderVersion, &e.Disposition, &e.ReceivedAt) == nil {
			out = append(out, e)
		}
	}
	return out
}

func getPerPayOrderTx(tx *sql.Tx, id int) (*model.PerPayOrder, error) {
	row := tx.QueryRow("SELECT "+perpayOrderBaseCols+" FROM perpay_orders o WHERE id=?", id)
	o, err := scanPerPayOrder(row)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func getPerPayOrderByMerchantNoTx(tx *sql.Tx, no string) (*model.PerPayOrder, error) {
	row := tx.QueryRow("SELECT "+perpayOrderBaseCols+" FROM perpay_orders o WHERE merchant_order_no=?", no)
	o, err := scanPerPayOrder(row)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// applyPerPaySnapshotTx 是状态机核心：金额/订单 ID 校验、版本防回退、单次入账。
func applyPerPaySnapshotTx(tx *sql.Tx, local *model.PerPayOrder, snap PerPaySnapshot, source string) (string, error) {
	if snap.MerchantOrderNo != local.MerchantOrderNo {
		return "", errors.New("PerPay 返回了另一笔业务订单")
	}
	if snap.RequestedAmountCents != local.AmountCents {
		return "", errors.New("PerPay 订单金额与本地业务订单不一致")
	}
	if local.PerPayOrderID != nil && *local.PerPayOrderID != snap.OrderID {
		return "", errors.New("PerPay 订单 ID 与本地记录不一致")
	}
	disposition := "updated"
	if snap.Version < local.OrderVersion {
		return "ignored_older_version", nil
	}

	status := local.Status
	switch {
	case snap.PaymentStatus == "DISPUTED":
		status = "disputed"
	case snap.PaymentStatus == "CONFIRMED" || local.PaidAt != nil:
		// 已入账订单不因乱序/迟到的 UNPAID 快照回退。
		status = "paid"
	default:
		switch snap.CheckoutStatus {
		case "EXPIRED":
			status = "expired"
		case "CLOSED":
			status = "closed"
		default:
			status = "waiting"
		}
	}

	paidAt := local.PaidAt
	credited := local.Credited
	var receivedCents any
	if snap.ReceivedAmountCents != nil {
		receivedCents = *snap.ReceivedAmountCents
	} else {
		receivedCents = nil
	}
	var expires any
	if snap.CheckoutExpiresAt != nil {
		expires = *snap.CheckoutExpiresAt
	} else {
		expires = nil
	}

	creditNow := snap.PaymentStatus == "CONFIRMED" && local.PaidAt == nil
	creditAmount := round4(float64(local.AmountCents) / 100)
	if creditNow {
		paidAtValue := now()
		paidAt = &paidAtValue
		credited = &creditAmount
	}

	_, err := tx.Exec(`UPDATE perpay_orders SET perpay_order_id=?, payable_cents=?, received_cents=?,
		status=?, payment_basis=?, checkout_url=COALESCE(NULLIF(?,''), checkout_url),
		checkout_status=COALESCE(NULLIF(?,''), checkout_status),
		checkout_expires_at=COALESCE(?, checkout_expires_at),
		order_version=?, last_error='', credited=?, updated_at=?, paid_at=?, last_checked_at=?
		WHERE id=?`,
		snap.OrderID, snap.PayableAmountCents, receivedCents, status, snap.PaymentBasis,
		snap.CheckoutURL, snap.CheckoutStatus, expires, snap.Version, credited, now(),
		nullableInt64(paidAt), now(), local.ID)
	if err != nil {
		return "", err
	}

	if creditNow {
		amount := creditAmount
		var before float64
		if err := tx.QueryRow("SELECT balance FROM users WHERE uid=?", local.UID).Scan(&before); err != nil {
			return "", fmt.Errorf("读取用户余额失败：%w", err)
		}
		after := round4(before + amount)
		if _, err := tx.Exec("UPDATE users SET balance=?, updated_at=? WHERE uid=?", after, now(), local.UID); err != nil {
			return "", err
		}
		_, err := tx.Exec(`INSERT INTO ledger(uid,type,amount,balance_before,balance_after,order_number,code,operator_id,remark,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, local.UID, "recharge", amount, before, after,
			"perpay-"+strconv.Itoa(local.ID), "", nil, "支付宝充值（PerPay）", now())
		if err != nil {
			return "", err
		}
		disposition = "credited"
	} else if snap.Version == local.OrderVersion {
		disposition = "same_version"
	}
	return disposition, nil
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
