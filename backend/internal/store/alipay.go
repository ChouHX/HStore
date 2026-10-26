package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

var (
	ErrAlipayAmountSlotsExhausted = errors.New("当前金额暂时没有可用的应付金额，请稍后重试")
	ErrAlipayOrderNotOpen         = errors.New("订单已结束或已支付")
	ErrAlipayLedgerAllocated      = errors.New("该支付宝流水已关联订单")
)

// AlipayConfig 是经营码收款配置（脱敏）。
type AlipayConfig struct {
	Enabled              bool    `json:"enabled"`
	Env                  string  `json:"env"`
	AppID                string  `json:"appId"`
	HasPrivateKey        bool    `json:"hasPrivateKey"`
	HasPublicKey         bool    `json:"hasPublicKey"`
	CollectionCode       string  `json:"collectionCode"`
	MinCny               float64 `json:"minCny"`
	MaxCny               float64 `json:"maxCny"`
	ProductName          string  `json:"productName"`
	OrderTTLSeconds      int     `json:"orderTtlSeconds"`
	AmountOffsetMaxCents int     `json:"amountOffsetMaxCents"`
	CooldownSeconds      int     `json:"cooldownSeconds"`
	ActiveScanSeconds    int     `json:"activeScanSeconds"`
	NormalScanSeconds    int     `json:"normalScanSeconds"`
	SafetyLagSeconds     int     `json:"safetyLagSeconds"`
	LookbackSeconds      int     `json:"lookbackSeconds"`
	Configured           bool    `json:"configured"`
	Endpoint             string  `json:"endpoint"`
}

// AlipaySecretConfig 是内部使用的完整配置。
type AlipaySecretConfig struct {
	AlipayConfig
	PrivateKey string `json:"-"`
	PublicKey  string `json:"-"`
}

// GetAlipayConfig 读取脱敏配置。
func GetAlipayConfig() AlipayConfig {
	return GetAlipaySecretConfig().AlipayConfig
}

// GetAlipaySecretConfig 读取完整配置。
func GetAlipaySecretConfig() AlipaySecretConfig {
	cfg := AlipaySecretConfig{}
	cfg.Enabled = db.GetMeta("alipay_enabled") == "1"
	cfg.Env = db.GetMeta("alipay_env")
	if cfg.Env == "" {
		cfg.Env = "production"
	}
	cfg.AppID = db.GetMeta("alipay_app_id")
	cfg.PrivateKey = db.GetMeta("alipay_private_key")
	cfg.PublicKey = db.GetMeta("alipay_public_key")
	cfg.CollectionCode = db.GetMeta("alipay_collection_code")
	cfg.MinCny = metaFloat("alipay_min_cny", 1)
	cfg.MaxCny = metaFloat("alipay_max_cny", 5000)
	cfg.ProductName = db.GetMeta("alipay_product_name")
	if cfg.ProductName == "" {
		cfg.ProductName = "账户余额充值"
	}
	cfg.OrderTTLSeconds = metaInt("alipay_order_ttl_seconds", 300)
	cfg.AmountOffsetMaxCents = metaInt("alipay_amount_offset_max_cents", 99)
	cfg.CooldownSeconds = metaInt("alipay_cooldown_seconds", 600)
	cfg.ActiveScanSeconds = metaInt("alipay_active_scan_seconds", 8)
	cfg.NormalScanSeconds = metaInt("alipay_normal_scan_seconds", 60)
	cfg.SafetyLagSeconds = metaInt("alipay_safety_lag_seconds", 10)
	cfg.LookbackSeconds = metaInt("alipay_lookback_seconds", 3600)
	cfg.HasPrivateKey = cfg.PrivateKey != ""
	cfg.HasPublicKey = cfg.PublicKey != ""
	cfg.Configured = cfg.AppID != "" && cfg.PrivateKey != "" && cfg.PublicKey != "" && cfg.CollectionCode != ""
	cfg.Endpoint = "https://openapi.alipay.com"
	if strings.EqualFold(cfg.Env, "sandbox") {
		cfg.Endpoint = "https://openapi-sandbox.dl.alipaydev.com"
	}
	return cfg
}

func metaFloat(key string, def float64) float64 {
	v := db.GetMeta(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func metaInt(key string, def int) int {
	v := db.GetMeta(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// SaveAlipayConfig 保存配置；空密钥表示不修改。
func SaveAlipayConfig(patch map[string]any) {
	if v, ok := patch["enabled"].(bool); ok {
		_ = db.SetMeta("alipay_enabled", boolMeta(v))
	}
	for key, metaKey := range map[string]string{
		"env":            "alipay_env",
		"appId":          "alipay_app_id",
		"collectionCode": "alipay_collection_code",
		"productName":    "alipay_product_name",
	} {
		if v, ok := patch[key].(string); ok {
			_ = db.SetMeta(metaKey, strings.TrimSpace(v))
		}
	}
	for key, metaKey := range map[string]string{
		"minCny":               "alipay_min_cny",
		"maxCny":               "alipay_max_cny",
		"orderTtlSeconds":      "alipay_order_ttl_seconds",
		"amountOffsetMaxCents": "alipay_amount_offset_max_cents",
		"cooldownSeconds":      "alipay_cooldown_seconds",
		"activeScanSeconds":    "alipay_active_scan_seconds",
		"normalScanSeconds":    "alipay_normal_scan_seconds",
		"safetyLagSeconds":     "alipay_safety_lag_seconds",
		"lookbackSeconds":      "alipay_lookback_seconds",
	} {
		if v, ok := patch[key].(float64); ok {
			_ = db.SetMeta(metaKey, strconv.FormatFloat(v, 'f', -1, 64))
		}
	}
	if v, ok := patch["privateKey"].(string); ok && strings.TrimSpace(v) != "" {
		_ = db.SetMeta("alipay_private_key", strings.TrimSpace(v))
	}
	if v, ok := patch["publicKey"].(string); ok && strings.TrimSpace(v) != "" {
		_ = db.SetMeta("alipay_public_key", strings.TrimSpace(v))
	}
}

func boolMeta(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// AlipayCheckoutToken 基于服务端会话密钥和订单号生成稳定的收银台 token。
func AlipayCheckoutToken(orderNo string) string {
	mac := hmac.New(sha256.New, []byte(db.SessionSecret()))
	mac.Write([]byte("alipay-checkout:" + orderNo))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// HashAlipayToken 返回 token 的 SHA-256，用于数据库索引。
func HashAlipayToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewAlipayOrderNo 生成商户/本地订单号。
func NewAlipayOrderNo() string {
	buf := make([]byte, 5)
	_, _ = rand.Read(buf)
	return "AL" + time.Now().Format("20060102150405") + strings.ToUpper(hex.EncodeToString(buf))
}

// AlipayScanState 读取查账游标。
func GetAlipayScanState() model.AlipayScanState {
	var s model.AlipayScanState
	_ = db.Must().QueryRow(`SELECT cursor_at,last_attempt_at,last_success_at,last_error,last_trace_id
		FROM alipay_scan_state WHERE id=1`).Scan(&s.CursorAt, &s.LastAttemptAt, &s.LastSuccessAt, &s.LastError, &s.LastTraceID)
	return s
}

// MarkAlipayScanAttempt 记录一次查账尝试的结果。
func MarkAlipayScanAttempt(errMessage, traceID string, success bool, cursor int64) {
	if len(errMessage) > 500 {
		errMessage = errMessage[:500]
	}
	if success {
		_, _ = db.Must().Exec(`UPDATE alipay_scan_state SET cursor_at=?, last_attempt_at=?, last_success_at=?,
			last_error='', last_trace_id=? WHERE id=1`, cursor, now(), now(), traceID)
		return
	}
	_, _ = db.Must().Exec(`UPDATE alipay_scan_state SET last_attempt_at=?, last_error=?, last_trace_id=? WHERE id=1`,
		now(), errMessage, traceID)
}

// ResetAlipayScanState 在切换支付宝应用/环境后重置查账游标。
func ResetAlipayScanState() {
	_, _ = db.Must().Exec(`UPDATE alipay_scan_state SET cursor_at=0,last_attempt_at=0,
		last_success_at=0,last_error='',last_trace_id='' WHERE id=1`)
}

// CountOpenAlipayOrders 返回当前待支付订单数，用于选择查账频率。
func CountOpenAlipayOrders() int {
	var n int
	_ = db.Must().QueryRow(`SELECT COUNT(*) FROM alipay_orders
		WHERE payment_status='UNPAID' AND checkout_status='OPEN' AND expires_at > ?`, now()).Scan(&n)
	return n
}

const alipayOrderCols = `o.id, o.uid, o.order_no, o.checkout_hash, o.collection_code, o.requested_cents, o.payable_cents,
	o.payment_status, o.checkout_status, o.payment_basis, o.received_cents, o.alipay_order_no,
	o.ledger_entry_id, o.credited, o.expires_at, o.ended_at, o.cooldown_until, o.created_at, o.updated_at,
	o.paid_at, o.last_error`

func scanAlipayOrder(sc interface{ Scan(...any) error }) (model.AlipayOrder, error) {
	var o model.AlipayOrder
	var received, ledgerID, paidAt sql.NullInt64
	var credited sql.NullFloat64
	err := sc.Scan(&o.ID, &o.UID, &o.OrderNo, &o.CheckoutHash, &o.CollectionCode, &o.RequestedCents, &o.PayableCents,
		&o.PaymentStatus, &o.CheckoutStatus, &o.PaymentBasis, &received, &o.AlipayOrderNo,
		&ledgerID, &credited, &o.ExpiresAt, &o.EndedAt, &o.CooldownUntil, &o.CreatedAt, &o.UpdatedAt,
		&paidAt, &o.LastError)
	o.RequestedCny = round4(float64(o.RequestedCents) / 100)
	o.CheckoutToken = AlipayCheckoutToken(o.OrderNo)
	if o.CheckoutStatus == "OPEN" && o.PaymentStatus == "UNPAID" {
		o.CheckoutURL = "/pay/" + o.CheckoutToken
	}
	o.Status = alipayDisplayStatus(o)
	if received.Valid {
		v := received.Int64
		o.ReceivedCents = &v
	}
	if ledgerID.Valid {
		v := ledgerID.Int64
		o.LedgerEntryID = &v
	}
	if credited.Valid {
		v := credited.Float64
		o.Credited = &v
	}
	if paidAt.Valid {
		v := paidAt.Int64
		o.PaidAt = &v
	}
	return o, err
}

func alipayDisplayStatus(o model.AlipayOrder) string {
	switch o.PaymentStatus {
	case "CONFIRMED":
		return "paid"
	case "DISPUTED":
		return "disputed"
	}
	switch o.CheckoutStatus {
	case "EXPIRED":
		return "expired"
	case "CLOSED":
		return "closed"
	default:
		return "waiting"
	}
}

// GetAlipayOrder 按本地 ID 读取。
func GetAlipayOrder(id int) *model.AlipayOrder {
	row := db.Must().QueryRow("SELECT "+alipayOrderCols+" FROM alipay_orders o WHERE o.id=?", id)
	o, err := scanAlipayOrder(row)
	if err != nil {
		return nil
	}
	return &o
}

// GetAlipayOrderByNo 按订单号读取。
func GetAlipayOrderByNo(no string) *model.AlipayOrder {
	row := db.Must().QueryRow("SELECT "+alipayOrderCols+" FROM alipay_orders o WHERE o.order_no=?", no)
	o, err := scanAlipayOrder(row)
	if err != nil {
		return nil
	}
	return &o
}

// GetAlipayOrderByToken 按收银台 token 读取。
func GetAlipayOrderByToken(token string) *model.AlipayOrder {
	hash := HashAlipayToken(token)
	row := db.Must().QueryRow("SELECT "+alipayOrderCols+" FROM alipay_orders o WHERE o.checkout_hash=?", hash)
	o, err := scanAlipayOrder(row)
	if err != nil {
		return nil
	}
	return &o
}

// ListAlipayOrders 返回订单列表；uid 为 0 时返回全部。
func ListAlipayOrders(uid, limit int) []model.AlipayOrder {
	if limit <= 0 {
		limit = 100
	}
	query := "SELECT " + alipayOrderCols + ", COALESCE(u.username,'') FROM alipay_orders o " +
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
		return []model.AlipayOrder{}
	}
	defer rows.Close()
	out := []model.AlipayOrder{}
	for rows.Next() {
		var o model.AlipayOrder
		var received, ledgerID, paidAt sql.NullInt64
		var credited sql.NullFloat64
		var username string
		if err := rows.Scan(&o.ID, &o.UID, &o.OrderNo, &o.CheckoutHash, &o.CollectionCode, &o.RequestedCents, &o.PayableCents,
			&o.PaymentStatus, &o.CheckoutStatus, &o.PaymentBasis, &received, &o.AlipayOrderNo,
			&ledgerID, &credited, &o.ExpiresAt, &o.EndedAt, &o.CooldownUntil, &o.CreatedAt, &o.UpdatedAt,
			&paidAt, &o.LastError, &username); err != nil {
			continue
		}
		o.Username = username
		o.RequestedCny = round4(float64(o.RequestedCents) / 100)
		o.CheckoutToken = AlipayCheckoutToken(o.OrderNo)
		if o.CheckoutStatus == "OPEN" && o.PaymentStatus == "UNPAID" {
			o.CheckoutURL = "/pay/" + o.CheckoutToken
		}
		o.Status = alipayDisplayStatus(o)
		if received.Valid {
			v := received.Int64
			o.ReceivedCents = &v
		}
		if ledgerID.Valid {
			v := ledgerID.Int64
			o.LedgerEntryID = &v
		}
		if credited.Valid {
			v := credited.Float64
			o.Credited = &v
		}
		if paidAt.Valid {
			v := paidAt.Int64
			o.PaidAt = &v
		}
		out = append(out, o)
	}
	return out
}

// CreateAlipayOrder 在事务内选择唯一应付金额、写入订单并占用金额槽。
func CreateAlipayOrder(uid int, orderNo, checkoutHash, collectionCode string, requestedCents int64, offsetMax, ttlMs, cooldownMs int64) (*model.AlipayOrder, error) {
	if requestedCents < 1 {
		return nil, errors.New("充值金额过小")
	}
	if offsetMax < 1 {
		offsetMax = 1
	}
	d := db.Must()
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	nowMs := now()
	var payable int64
	found := false
	for offset := int64(1); offset <= offsetMax; offset++ {
		candidate := requestedCents + offset
		var one int
		err := tx.QueryRow(`SELECT 1 FROM alipay_amount_slots WHERE payable_cents=?
			AND (released_at IS NULL OR cooldown_until > ?)`, candidate, nowMs).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			payable = candidate
			found = true
			break
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if !found {
		return nil, ErrAlipayAmountSlotsExhausted
	}
	expires := nowMs + ttlMs
	cooldownUntil := expires + cooldownMs
	res, err := tx.Exec(`INSERT INTO alipay_orders(uid,order_no,checkout_hash,collection_code,requested_cents,payable_cents,
		payment_status,checkout_status,payment_basis,expires_at,ended_at,cooldown_until,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'UNPAID','OPEN','NONE',?,?,?,?,?)`,
		uid, orderNo, checkoutHash, collectionCode, requestedCents, payable, expires, expires, cooldownUntil, nowMs, nowMs)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO alipay_amount_slots(payable_cents,order_id,occupied_from,released_at,cooldown_until)
		VALUES(?,?,?,NULL,?) ON CONFLICT(payable_cents) DO UPDATE SET order_id=excluded.order_id,
		occupied_from=excluded.occupied_from, released_at=NULL, cooldown_until=excluded.cooldown_until`,
		payable, id, nowMs, cooldownUntil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetAlipayOrder(int(id)), nil
}

// ExpireAlipayOrders 把过期未支付订单标记为 EXPIRED，并释放金额槽。
func ExpireAlipayOrders() int {
	d := db.Must()
	nowMs := now()
	rows, err := d.Query(`SELECT id, expires_at, cooldown_until FROM alipay_orders
		WHERE payment_status='UNPAID' AND checkout_status='OPEN' AND expires_at < ?`, nowMs)
	if err != nil {
		return 0
	}
	type item struct {
		id, expires, cooldown int64
	}
	list := []item{}
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.expires, &it.cooldown) == nil {
			list = append(list, it)
		}
	}
	rows.Close()
	count := 0
	for _, it := range list {
		tx, err := d.Begin()
		if err != nil {
			continue
		}
		res, err := tx.Exec(`UPDATE alipay_orders SET checkout_status='EXPIRED', updated_at=?
			WHERE id=? AND payment_status='UNPAID' AND checkout_status='OPEN'`, nowMs, it.id)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 1 {
				_, _ = tx.Exec(`UPDATE alipay_amount_slots SET released_at=?, cooldown_until=?
					WHERE order_id=?`, it.expires, it.cooldown, it.id)
				count++
			}
		}
		_ = tx.Commit()
	}
	return count
}

// CloseAlipayOrder 关闭未支付订单并释放金额槽。
func CloseAlipayOrder(id int) (*model.AlipayOrder, error) {
	d := db.Must()
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	order, err := getAlipayOrderTx(tx, id)
	if err != nil {
		return nil, err
	}
	if order.PaymentStatus != "UNPAID" || order.CheckoutStatus != "OPEN" {
		return nil, ErrAlipayOrderNotOpen
	}
	nowMs := now()
	duration := order.CooldownUntil - order.ExpiresAt
	if duration < 0 {
		duration = 0
	}
	endedAt := nowMs
	cooldown := nowMs + duration
	if nowMs >= order.ExpiresAt {
		// 已超过有效期时关闭不能延长可匹配窗口，也不能重新开始冷却。
		endedAt = order.ExpiresAt
		cooldown = order.CooldownUntil
	}
	status := "CLOSED"
	if nowMs >= order.ExpiresAt {
		status = "EXPIRED"
	}
	if _, err := tx.Exec(`UPDATE alipay_orders SET checkout_status=?, ended_at=?, cooldown_until=?, updated_at=?
		WHERE id=?`, status, endedAt, cooldown, nowMs, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE alipay_amount_slots SET released_at=?, cooldown_until=?
		WHERE order_id=?`, endedAt, cooldown, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetAlipayOrder(id), nil
}

func getAlipayOrderTx(tx *sql.Tx, id int) (*model.AlipayOrder, error) {
	row := tx.QueryRow("SELECT "+alipayOrderCols+" FROM alipay_orders o WHERE o.id=?", id)
	o, err := scanAlipayOrder(row)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// InsertAlipayLedgerEntry 幂等写入一条账务流水，返回本地 ID 和是否新插入。
func InsertAlipayLedgerEntry(e model.AlipayLedgerEntry) (int64, bool, error) {
	d := db.Must()
	res, err := d.Exec(`INSERT OR IGNORE INTO alipay_ledger_entries(external_event_id,account_log_id,
		occurred_at,occurred_precision_ms,amount_cents,direction,alipay_order_no,merchant_order_no,
		trans_memo,other_account,state,order_id,raw_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL,?,?,?)`,
		e.ExternalEventID, e.AccountLogID, e.OccurredAt, e.OccurredPrecision, e.AmountCents,
		e.Direction, e.AlipayOrderNo, e.MerchantOrderNo, e.TransMemo, e.OtherAccount,
		e.State, e.RawJSON, now(), now())
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		id, _ := res.LastInsertId()
		return id, true, nil
	}
	var id int64
	if err := d.QueryRow(`SELECT id FROM alipay_ledger_entries WHERE external_event_id=?`,
		e.ExternalEventID).Scan(&id); err != nil {
		return 0, false, err
	}
	return id, false, nil
}

// GetAlipayLedgerEntry 按 ID 读取流水。
func GetAlipayLedgerEntry(id int64) *model.AlipayLedgerEntry {
	var e model.AlipayLedgerEntry
	var orderID sql.NullInt64
	err := db.Must().QueryRow(`SELECT id,external_event_id,account_log_id,occurred_at,
		occurred_precision_ms,amount_cents,direction,alipay_order_no,merchant_order_no,
		trans_memo,other_account,state,order_id,raw_json,created_at,updated_at
		FROM alipay_ledger_entries WHERE id=?`, id).Scan(&e.ID, &e.ExternalEventID, &e.AccountLogID,
		&e.OccurredAt, &e.OccurredPrecision, &e.AmountCents, &e.Direction, &e.AlipayOrderNo,
		&e.MerchantOrderNo, &e.TransMemo, &e.OtherAccount, &e.State, &orderID, &e.RawJSON,
		&e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil
	}
	if orderID.Valid {
		v := int(orderID.Int64)
		e.OrderID = &v
	}
	return &e
}

// ListAlipayLedger 返回最近流水，state 为空时返回全部。
func ListAlipayLedger(limit int, state string) []model.AlipayLedgerEntry {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id,external_event_id,account_log_id,occurred_at,occurred_precision_ms,
		amount_cents,direction,alipay_order_no,merchant_order_no,trans_memo,other_account,
		state,order_id,raw_json,created_at,updated_at FROM alipay_ledger_entries`
	args := []any{}
	if state != "" {
		query += " WHERE state=?"
		args = append(args, state)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.Must().Query(query, args...)
	if err != nil {
		return []model.AlipayLedgerEntry{}
	}
	defer rows.Close()
	out := []model.AlipayLedgerEntry{}
	for rows.Next() {
		var e model.AlipayLedgerEntry
		var orderID sql.NullInt64
		if rows.Scan(&e.ID, &e.ExternalEventID, &e.AccountLogID, &e.OccurredAt,
			&e.OccurredPrecision, &e.AmountCents, &e.Direction, &e.AlipayOrderNo,
			&e.MerchantOrderNo, &e.TransMemo, &e.OtherAccount, &e.State, &orderID,
			&e.RawJSON, &e.CreatedAt, &e.UpdatedAt) != nil {
			continue
		}
		if orderID.Valid {
			v := int(orderID.Int64)
			e.OrderID = &v
		}
		out = append(out, e)
	}
	return out
}

// ListAlipayPendingLedger 返回待人工/自动匹配的收入流水。
func ListAlipayPendingLedger(limit int) []model.AlipayLedgerEntry {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Must().Query(`SELECT id,external_event_id,account_log_id,occurred_at,
		occurred_precision_ms,amount_cents,direction,alipay_order_no,merchant_order_no,
		trans_memo,other_account,state,order_id,raw_json,created_at,updated_at
		FROM alipay_ledger_entries
		WHERE direction='CREDIT' AND state IN ('UNALLOCATED','CANDIDATE')
		ORDER BY occurred_at ASC, id ASC LIMIT ?`, limit)
	if err != nil {
		return []model.AlipayLedgerEntry{}
	}
	defer rows.Close()
	out := []model.AlipayLedgerEntry{}
	for rows.Next() {
		var e model.AlipayLedgerEntry
		var orderID sql.NullInt64
		if rows.Scan(&e.ID, &e.ExternalEventID, &e.AccountLogID, &e.OccurredAt,
			&e.OccurredPrecision, &e.AmountCents, &e.Direction, &e.AlipayOrderNo,
			&e.MerchantOrderNo, &e.TransMemo, &e.OtherAccount, &e.State, &orderID,
			&e.RawJSON, &e.CreatedAt, &e.UpdatedAt) != nil {
			continue
		}
		if orderID.Valid {
			v := int(orderID.Int64)
			e.OrderID = &v
		}
		out = append(out, e)
	}
	return out
}

// AlipayReconcileResult 是单条流水的自动匹配结果。
type AlipayReconcileResult struct {
	Kind      string `json:"kind"` // ignored / unmatched / ambiguous / settled
	OrderID   int    `json:"orderId"`
	OrderNo   string `json:"orderNo"`
	Candidate int    `json:"candidate"`
}

// ReconcileAlipayLedgerEntry 尝试把一条收入流水与唯一待支付订单匹配。
func ReconcileAlipayLedgerEntry(entryID int64) (AlipayReconcileResult, error) {
	d := db.Must()
	tx, err := d.Begin()
	if err != nil {
		return AlipayReconcileResult{}, err
	}
	defer tx.Rollback()
	entry, err := getAlipayLedgerTx(tx, entryID)
	if err != nil {
		return AlipayReconcileResult{}, err
	}
	if entry.Direction != "CREDIT" || entry.State == "ALLOCATED" {
		return AlipayReconcileResult{Kind: "ignored"}, nil
	}
	rows, err := tx.Query(`SELECT `+alipayOrderCols+` FROM alipay_orders o
		WHERE o.payment_status='UNPAID' AND o.payable_cents=?
		  AND ? + ? > o.created_at
		  AND ? < o.ended_at
		ORDER BY o.created_at, o.id LIMIT 2`,
		entry.AmountCents, entry.OccurredAt, entry.OccurredPrecision, entry.OccurredAt)
	if err != nil {
		return AlipayReconcileResult{}, err
	}
	candidates := []model.AlipayOrder{}
	for rows.Next() {
		if o, err := scanAlipayOrder(rows); err == nil {
			candidates = append(candidates, o)
		}
	}
	rows.Close()
	if len(candidates) == 0 {
		_, _ = tx.Exec(`UPDATE alipay_ledger_entries SET state='UNALLOCATED', updated_at=? WHERE id=?`, now(), entryID)
		if err := tx.Commit(); err != nil {
			return AlipayReconcileResult{}, err
		}
		return AlipayReconcileResult{Kind: "unmatched"}, nil
	}
	if len(candidates) > 1 {
		_, _ = tx.Exec(`UPDATE alipay_ledger_entries SET state='CANDIDATE', updated_at=? WHERE id=?`, now(), entryID)
		if err := tx.Commit(); err != nil {
			return AlipayReconcileResult{}, err
		}
		return AlipayReconcileResult{Kind: "ambiguous", Candidate: len(candidates)}, nil
	}
	order := candidates[0]
	if err := settleAlipayOrderTx(tx, &order, entry, "INFERRED"); err != nil {
		return AlipayReconcileResult{}, err
	}
	if _, err := tx.Exec(`UPDATE alipay_ledger_entries SET state='ALLOCATED', order_id=?, updated_at=? WHERE id=?`,
		order.ID, now(), entryID); err != nil {
		return AlipayReconcileResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AlipayReconcileResult{}, err
	}
	return AlipayReconcileResult{Kind: "settled", OrderID: order.ID, OrderNo: order.OrderNo}, nil
}

// ManualMatchAlipay 管理员把一条流水关联到指定待支付订单（允许金额不一致）。
func ManualMatchAlipay(orderID int, ledgerID int64) (*model.AlipayOrder, error) {
	d := db.Must()
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	entry, err := getAlipayLedgerTx(tx, ledgerID)
	if err != nil {
		return nil, err
	}
	if entry.Direction != "CREDIT" || entry.State == "ALLOCATED" {
		return nil, ErrAlipayLedgerAllocated
	}
	order, err := getAlipayOrderTx(tx, orderID)
	if err != nil {
		return nil, err
	}
	if order.PaymentStatus != "UNPAID" {
		return nil, ErrAlipayOrderNotOpen
	}
	if err := settleAlipayOrderTx(tx, order, entry, "MANUAL"); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE alipay_ledger_entries SET state='ALLOCATED', order_id=?, updated_at=? WHERE id=?`,
		order.ID, now(), ledgerID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetAlipayOrder(orderID), nil
}

func getAlipayLedgerTx(tx *sql.Tx, id int64) (*model.AlipayLedgerEntry, error) {
	var e model.AlipayLedgerEntry
	var orderID sql.NullInt64
	err := tx.QueryRow(`SELECT id,external_event_id,account_log_id,occurred_at,occurred_precision_ms,
		amount_cents,direction,alipay_order_no,merchant_order_no,trans_memo,other_account,
		state,order_id,raw_json,created_at,updated_at FROM alipay_ledger_entries WHERE id=?`, id).
		Scan(&e.ID, &e.ExternalEventID, &e.AccountLogID, &e.OccurredAt, &e.OccurredPrecision,
			&e.AmountCents, &e.Direction, &e.AlipayOrderNo, &e.MerchantOrderNo, &e.TransMemo,
			&e.OtherAccount, &e.State, &orderID, &e.RawJSON, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if orderID.Valid {
		v := int(orderID.Int64)
		e.OrderID = &v
	}
	return &e, nil
}

// settleAlipayOrderTx 在事务内确认订单、入账并释放金额槽。
func settleAlipayOrderTx(tx *sql.Tx, order *model.AlipayOrder, entry *model.AlipayLedgerEntry, basis string) error {
	nowMs := now()
	duration := order.CooldownUntil - order.ExpiresAt
	if duration < 0 {
		duration = 0
	}
	cooldown := maxInt64(nowMs, order.ExpiresAt) + duration
	credited := round4(float64(order.RequestedCents) / 100)
	res, err := tx.Exec(`UPDATE alipay_orders SET payment_status='CONFIRMED', checkout_status='CLOSED',
		payment_basis=?, received_cents=?, alipay_order_no=?, ledger_entry_id=?, credited=?,
		paid_at=?, cooldown_until=?, updated_at=?, last_error=''
		WHERE id=? AND payment_status='UNPAID'`,
		basis, entry.AmountCents, entry.AlipayOrderNo, entry.ID, credited, nowMs, cooldown, nowMs, order.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrAlipayOrderNotOpen
	}
	if _, err := tx.Exec(`UPDATE alipay_amount_slots SET released_at=?, cooldown_until=? WHERE order_id=?`,
		nowMs, cooldown, order.ID); err != nil {
		return err
	}
	var before float64
	if err := tx.QueryRow("SELECT balance FROM users WHERE uid=?", order.UID).Scan(&before); err != nil {
		return fmt.Errorf("读取用户余额失败：%w", err)
	}
	after := round4(before + credited)
	if _, err := tx.Exec("UPDATE users SET balance=?, updated_at=? WHERE uid=?", after, nowMs, order.UID); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO ledger(uid,type,amount,balance_before,balance_after,order_number,code,operator_id,remark,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, order.UID, "recharge", credited, before, after,
		"alipay-"+strconv.Itoa(order.ID), "", nil, "支付宝经营码充值", nowMs)
	return err
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
