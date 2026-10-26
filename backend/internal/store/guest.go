package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// CreateGuestUser 为游客购买创建一个隔离的临时用户，余额只用于本次购买。
// 游客用户不会出现在管理后台用户列表和用户统计中。
func CreateGuestUser() (*model.User, error) {
	var lastErr error
	for i := 0; i < 8; i++ {
		buf := make([]byte, 8)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		username := "guest_" + hex.EncodeToString(buf)
		pass := make([]byte, 16)
		if _, err := rand.Read(pass); err != nil {
			return nil, err
		}
		res, err := db.Must().Exec(`INSERT INTO users(username,nickname,password,phone,email,role,status,balance,total_spent,level_id,level_locked,created_at,updated_at)
			VALUES(?,?,?,'','','guest','active',0,0,NULL,0,?,?)`,
			username, "游客", db.HashPassword(hex.EncodeToString(pass)), now(), now())
		if err != nil {
			lastErr = err
			continue
		}
		id, _ := res.LastInsertId()
		return GetUser(int(id)), nil
	}
	if lastErr == nil {
		lastErr = errors.New("创建游客账户失败")
	}
	return nil, lastErr
}

const guestPurchaseCols = `id, token, uid, guest_user, channel, payment_order_id,
	goods_id, goods_name, goods_count, attach_json, amount_cny, status,
	upstream_order_no, outer_number, last_error, created_at, updated_at, paid_at, fulfilled_at`

func scanGuestPurchase(sc interface{ Scan(...any) error }) (model.GuestPurchase, error) {
	var g model.GuestPurchase
	var paidAt, fulfilledAt sql.NullInt64
	err := sc.Scan(&g.ID, &g.Token, &g.UID, &g.GuestUser, &g.Channel, &g.PaymentOrderID,
		&g.GoodsID, &g.GoodsName, &g.GoodsCount, &g.AttachJSON, &g.AmountCny, &g.Status,
		&g.UpstreamOrderNo, &g.OuterNumber, &g.LastError, &g.CreatedAt, &g.UpdatedAt,
		&paidAt, &fulfilledAt)
	if paidAt.Valid {
		v := paidAt.Int64
		g.PaidAt = &v
	}
	if fulfilledAt.Valid {
		v := fulfilledAt.Int64
		g.FulfilledAt = &v
	}
	return g, err
}

// InsertGuestPurchase 写入游客购买记录。
func InsertGuestPurchase(g model.GuestPurchase) (int, error) {
	res, err := db.Must().Exec(`INSERT INTO guest_purchases(token,uid,guest_user,channel,payment_order_id,
		goods_id,goods_name,goods_count,attach_json,amount_cny,status,upstream_order_no,outer_number,
		last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.Token, g.UID, g.GuestUser, g.Channel, g.PaymentOrderID, g.GoodsID, g.GoodsName,
		g.GoodsCount, g.AttachJSON, g.AmountCny, g.Status, g.UpstreamOrderNo, g.OuterNumber,
		g.LastError, now(), now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}

// GetGuestPurchase 按 ID 读取。
func GetGuestPurchase(id int) *model.GuestPurchase {
	row := db.Must().QueryRow("SELECT "+guestPurchaseCols+" FROM guest_purchases WHERE id=?", id)
	g, err := scanGuestPurchase(row)
	if err != nil {
		return nil
	}
	return &g
}

// GetGuestPurchaseByToken 按游客凭证读取。
func GetGuestPurchaseByToken(token string) *model.GuestPurchase {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	row := db.Must().QueryRow("SELECT "+guestPurchaseCols+" FROM guest_purchases WHERE token=?", token)
	g, err := scanGuestPurchase(row)
	if err != nil {
		return nil
	}
	return &g
}

// GetGuestPurchaseByPayment 按支付渠道和支付订单 ID 查找。
func GetGuestPurchaseByPayment(channel string, paymentOrderID int) *model.GuestPurchase {
	row := db.Must().QueryRow("SELECT "+guestPurchaseCols+" FROM guest_purchases WHERE channel=? AND payment_order_id=? ORDER BY id DESC LIMIT 1",
		channel, paymentOrderID)
	g, err := scanGuestPurchase(row)
	if err != nil {
		return nil
	}
	return &g
}

// ListPendingGuestPurchases 返回等待支付或上次履约失败的游客订单。
func ListPendingGuestPurchases(limit int) []model.GuestPurchase {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Must().Query("SELECT "+guestPurchaseCols+` FROM guest_purchases
		WHERE status IN ('waiting','failed') OR (status='fulfilling' AND updated_at < ?)
		ORDER BY id ASC LIMIT ?`, now()-2*60*1000, limit)
	if err != nil {
		return []model.GuestPurchase{}
	}
	defer rows.Close()
	out := []model.GuestPurchase{}
	for rows.Next() {
		if g, err := scanGuestPurchase(rows); err == nil {
			out = append(out, g)
		}
	}
	return out
}

// ClaimGuestPurchase 原子占用履约任务，避免重复向上游下单。
func ClaimGuestPurchase(id int) bool {
	nowMs := now()
	res, err := db.Must().Exec(`UPDATE guest_purchases SET status='fulfilling', updated_at=?
		WHERE id=? AND (status IN ('waiting','failed') OR (status='fulfilling' AND updated_at < ?))`,
		nowMs, id, nowMs-2*60*1000)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// MarkGuestPurchaseFulfilled 标记游客订单已向上游下单。
func MarkGuestPurchaseFulfilled(id int, upstreamOrderNo string, paidAt *int64) {
	nowMs := now()
	_, _ = db.Must().Exec(`UPDATE guest_purchases SET status='fulfilled', upstream_order_no=?,
		paid_at=COALESCE(?, paid_at), fulfilled_at=?, last_error='', updated_at=? WHERE id=?`,
		upstreamOrderNo, nullableInt64(paidAt), nowMs, nowMs, id)
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// MarkGuestPurchaseExpired 结束未支付的游客订单，避免长期占用待处理队列。
func MarkGuestPurchaseExpired(id int, status string) {
	if status != "expired" && status != "closed" && status != "cancelled" {
		status = "expired"
	}
	_, _ = db.Must().Exec(`UPDATE guest_purchases SET status=?, updated_at=?
		WHERE id=? AND status IN ('waiting','failed')`, status, now(), id)
}

// MarkGuestPurchaseFailed 记录履约失败，允许后续重试。
func MarkGuestPurchaseFailed(id int, message string) {
	if len(message) > 500 {
		message = message[:500]
	}
	_, _ = db.Must().Exec(`UPDATE guest_purchases SET status='failed', last_error=?, updated_at=?
		WHERE id=? AND status='fulfilling'`, message, now(), id)
}
