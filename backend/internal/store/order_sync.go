package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

const orderSyncInterval = 15 * time.Second

func OrderPending(status int) bool {
	return status == 1 || status == 2 || status == 7 || status == 8
}

// PendingOrderSyncNumbers reads persistent work, including orders left by a restart.
func PendingOrderSyncNumbers(limit int) ([]string, error) {
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	rows, err := db.Must().Query(`SELECT order_number FROM orders
		WHERE upstream_order_no <> '' AND status IN (1,2,7,8) AND updated_at <= ?
		ORDER BY updated_at, order_number LIMIT ?`, now()-orderSyncInterval.Milliseconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var numbers []string
	for rows.Next() {
		var number string
		if err := rows.Scan(&number); err != nil {
			return nil, err
		}
		numbers = append(numbers, number)
	}
	return numbers, rows.Err()
}

// ClaimOrderSync throttles retries, including network errors, without changing status.
func ClaimOrderSync(number string) (bool, error) {
	ts := now()
	res, err := db.Must().Exec(`UPDATE orders SET updated_at=? WHERE order_number=?
		AND upstream_order_no <> '' AND status IN (1,2,7,8) AND updated_at <= ?`,
		ts, number, ts-orderSyncInterval.Milliseconds())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func GetOrderByOuterNumber(outerNumber string) *model.LocalOrder {
	if outerNumber == "" {
		return nil
	}
	var number string
	if err := db.Must().QueryRow(`SELECT order_number FROM orders WHERE outer_number=? LIMIT 1`, outerNumber).Scan(&number); err != nil {
		return nil
	}
	return GetOrder(number)
}

func guestFulfillmentStatus(status int) string {
	switch status {
	case 3:
		return "fulfilled"
	case 4:
		return "delivery_failed"
	case 5:
		return "delivery_cancelled"
	default:
		return "fulfilling"
	}
}

// LinkGuestPurchaseOrder records acceptance separately from actual delivery.
// Read the order inside the UPDATE so a concurrent sync cannot be overwritten.
func LinkGuestPurchaseOrder(id int, number string, paidAt *int64) error {
	res, err := db.Must().Exec(`UPDATE guest_purchases SET upstream_order_no=?,
		status=(SELECT CASE status WHEN 3 THEN 'fulfilled' WHEN 4 THEN 'delivery_failed'
			WHEN 5 THEN 'delivery_cancelled' ELSE 'fulfilling' END FROM orders WHERE order_number=?),
		last_error=(SELECT CASE WHEN status IN (4,5,8) THEN result ELSE '' END FROM orders WHERE order_number=?),
		paid_at=COALESCE(?,paid_at),
		fulfilled_at=(SELECT CASE WHEN status=3 THEN COALESCE(guest_purchases.fulfilled_at,?) ELSE NULL END
			FROM orders WHERE order_number=?), updated_at=?
		WHERE id=? AND (upstream_order_no='' OR upstream_order_no=?)
		AND EXISTS(SELECT 1 FROM orders o WHERE o.order_number=? AND o.user_uid=guest_purchases.uid
			AND o.outer_number=guest_purchases.outer_number)`,
		number, number, number, nullableInt64(paidAt), now(), number, now(), id, number, number)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("订单关联失败")
	}
	return err
}

// ApplyOrderSync commits status, cards and guest delivery state together.
// A remote refund is not a customer refund; no balance or payment is changed here.
func ApplyOrderSync(number string, detail model.OrderDetail) error {
	if detail.OrderNumber == "" || detail.Status < 0 || detail.Status > 8 || detail.Status == 6 {
		return errors.New("invalid remote order response")
	}
	tx, err := db.Must().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Acquire the SQLite write lock before reading; concurrent workers cannot regress a terminal order.
	res, err := tx.Exec(`UPDATE orders SET updated_at=? WHERE order_number=? AND upstream_order_no=?
		AND (?='' OR outer_number=?) AND status IN (1,2,7,8)`,
		now(), number, detail.OrderNumber, detail.OuterNumber, detail.OuterNumber)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("order no longer pending or remote identity mismatch")
	}
	var currentStatus int
	if err := tx.QueryRow(`SELECT status FROM orders WHERE order_number=?`, number).Scan(&currentStatus); err != nil {
		return err
	}
	status := detail.Status
	if status == 0 {
		status = currentStatus
	}
	for _, card := range detail.Cards {
		if card.CardID == "" {
			return errors.New("remote card has no identity")
		}
		res, err := tx.Exec(`INSERT INTO cards(card_id,order_number,card_no,card_pwd,status,cancel_request,result,created_at)
			VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(card_id) DO UPDATE SET
			card_no=excluded.card_no,card_pwd=excluded.card_pwd,status=excluded.status,
			cancel_request=excluded.cancel_request,result=excluded.result
			WHERE cards.order_number=excluded.order_number`,
			card.CardID, number, card.CardNo, card.CardPwd, card.Status, card.CancelRequest, card.Result, now())
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			return errors.New("remote card belongs to another order")
		}
	}
	// Remote money is procurement cost. Keep the customer's sale amount and quantity intact.
	if _, err := tx.Exec(`UPDATE orders SET status=?,result=?,ret_money=?,updated_at=? WHERE order_number=?`,
		status, detail.Result, detail.RetMoney, now(), number); err != nil {
		return err
	}
	if status == 4 && IsShortageResult(detail.Result) {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO order_notifications(order_number,kind,status,created_at,updated_at)
			VALUES(?, 'shortage', 'pending', ?, ?)`, number, now(), now()); err != nil {
			return err
		}
	}
	guestStatus := guestFulfillmentStatus(status)
	lastError := ""
	if status == 4 || status == 5 || status == 8 {
		lastError = detail.Result
	}
	if _, err := tx.Exec(`UPDATE guest_purchases SET upstream_order_no=?,status=?,last_error=?,updated_at=?,
		fulfilled_at=CASE WHEN ?='fulfilled' THEN COALESCE(fulfilled_at,?) ELSE NULL END
		WHERE (upstream_order_no=? OR (upstream_order_no='' AND outer_number<>'' AND
			outer_number=(SELECT outer_number FROM orders WHERE order_number=?)))
		AND uid=(SELECT user_uid FROM orders WHERE order_number=?)`,
		number, guestStatus, lastError, now(), guestStatus, now(), number, number, number); err != nil {
		return err
	}
	if status == 4 {
		var uid sql.NullInt64
		var amount float64
		var cardCount int
		err := tx.QueryRow(`SELECT o.user_uid,o.sale_money,
			(SELECT COUNT(*) FROM cards c WHERE c.order_number=o.order_number)
			FROM orders o WHERE o.order_number=? AND o.settled=0 AND o.source='user'
			AND NOT EXISTS(SELECT 1 FROM redeem_codes rc WHERE rc.order_number=o.order_number)
			AND NOT EXISTS(SELECT 1 FROM guest_purchases gp WHERE gp.upstream_order_no=o.order_number OR gp.outer_number=o.outer_number)
			AND EXISTS(SELECT 1 FROM users u WHERE u.uid=o.user_uid AND u.role<>'guest')`, number).Scan(&uid, &amount, &cardCount)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && uid.Valid {
			// Never refund the full purchase when any cards were already delivered.
			if cardCount == 0 {
				var before float64
				if err := tx.QueryRow(`SELECT balance FROM users WHERE uid=?`, uid.Int64).Scan(&before); err != nil {
					return err
				}
				after := round4(before + amount)
				if _, err := tx.Exec(`UPDATE users SET balance=?,updated_at=? WHERE uid=?`, after, now(), uid.Int64); err != nil {
					return err
				}
				insertLedger(tx, int(uid.Int64), "recharge", amount, before, after, number, "", nil, "商品缺货自动退回余额")
			}
			if _, err := tx.Exec(`UPDATE orders SET settled=1 WHERE order_number=?`, number); err != nil {
				return err
			}
		}
	}
	// Only balance purchases accrue consumption. Code redemption and direct payment do not.
	if status == 3 {
		var uid sql.NullInt64
		var amount float64
		err := tx.QueryRow(`SELECT user_uid,sale_money FROM orders o WHERE order_number=? AND settled=0 AND source='user'
			AND NOT EXISTS(SELECT 1 FROM redeem_codes rc WHERE rc.order_number=o.order_number)
			AND NOT EXISTS(SELECT 1 FROM guest_purchases gp WHERE gp.upstream_order_no=o.order_number OR gp.outer_number=o.outer_number)`,
			number).Scan(&uid, &amount)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && uid.Valid {
			if _, err := tx.Exec(`UPDATE users SET total_spent=ROUND(total_spent+?,4),updated_at=? WHERE uid=?`, amount, now(), uid.Int64); err != nil {
				return err
			}
			autoAssignLevel(tx, int(uid.Int64))
			if _, err := tx.Exec(`UPDATE orders SET settled=1 WHERE order_number=?`, number); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// CustomerOrderResult exposes delivery information without supplier or procurement details.
func CustomerOrderResult(status int, result string) string {
	switch status {
	case 1, 7:
		return "正在安排发货，请稍候"
	case 2:
		return "部分商品已发出，剩余商品正在发货"
	case 3:
		return "发货成功"
	case 4:
		if IsShortageResult(result) {
			return "商品缺货，发货未完成，请联系客服处理退款，请勿重复支付"
		}
		return "发货未完成，请联系客服处理退款或换货，请勿重复支付"
	case 5:
		return "发货已取消，请联系客服处理，请勿重复支付"
	case 8:
		return "发货处理中，正在确认订单状态，请稍候"
	default:
		return ""
	}
}

func IsShortageResult(result string) bool {
	return strings.Contains(result, "缺货") || strings.Contains(result, "库存不足")
}

func OrderBalanceRefunded(orderNumber string) bool {
	var exists int
	err := db.Must().QueryRow(`SELECT 1 FROM ledger WHERE order_number=? AND type='recharge'
		AND remark='商品缺货自动退回余额' LIMIT 1`, orderNumber).Scan(&exists)
	return err == nil
}

// ShortageNotification is the durable work item sent to administrators.
type ShortageNotification struct {
	OrderNumber    string
	GoodsName      string
	Count          int
	SaleMoney      float64
	RetMoney       float64
	Result         string
	Contact        string
	CreatedAt      int64
	Attempts       int
	SentRecipients []string
}

// EnsureShortageNotifications backfills orders confirmed before this feature was deployed.
func EnsureShortageNotifications() error {
	_, err := db.Must().Exec(`INSERT OR IGNORE INTO order_notifications(order_number,kind,status,created_at,updated_at)
		SELECT order_number,'shortage','pending',created_at,updated_at FROM orders
		WHERE upstream_order_no<>'' AND status=4 AND (result LIKE '%缺货%' OR result LIKE '%库存不足%')`)
	return err
}

func ClaimShortageNotification() (*ShortageNotification, error) {
	d := db.Must()
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cutoff := now() - 5*60*1000
	if _, err := tx.Exec(`UPDATE order_notifications SET status='pending',updated_at=?
		WHERE kind='shortage' AND status='sending' AND updated_at<?`, now(), cutoff); err != nil {
		return nil, err
	}
	var n ShortageNotification
	var sentRecipients string
	err = tx.QueryRow(`SELECT o.order_number,o.goods_name,o.count,COALESCE(o.sale_money,0),
		COALESCE(o.ret_money,0),o.result,COALESCE(o.contact,''),o.created_at,notif.attempts,notif.sent_recipients
		FROM order_notifications notif JOIN orders o ON o.order_number=notif.order_number
		WHERE notif.kind='shortage' AND notif.status='pending' AND o.status=4
		AND (notif.attempts=0 OR notif.updated_at<=?)
		ORDER BY notif.updated_at,notif.order_number LIMIT 1`, now()-60*1000).Scan(&n.OrderNumber, &n.GoodsName, &n.Count,
		&n.SaleMoney, &n.RetMoney, &n.Result, &n.Contact, &n.CreatedAt, &n.Attempts, &sentRecipients)
	if err == sql.ErrNoRows {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(sentRecipients), &n.SentRecipients); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE order_notifications SET status='sending',attempts=attempts+1,updated_at=?
		WHERE order_number=? AND kind='shortage' AND status='pending'`, now(), n.OrderNumber); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	n.Attempts++
	return &n, nil
}

func CompleteShortageNotification(orderNumber string, sendErr error) error {
	if sendErr == nil {
		_, err := db.Must().Exec(`UPDATE order_notifications SET status='sent',last_error='',sent_at=?,updated_at=?
			WHERE order_number=? AND kind='shortage' AND status='sending'`, now(), now(), orderNumber)
		return err
	}
	message := sendErr.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := db.Must().Exec(`UPDATE order_notifications SET status='pending',last_error=?,updated_at=?
		WHERE order_number=? AND kind='shortage' AND status='sending'`, message, now(), orderNumber)
	return err
}

func RecordShortageRecipient(orderNumber, recipient string) error {
	_, err := db.Must().Exec(`UPDATE order_notifications
		SET sent_recipients=json_insert(sent_recipients,'$[#]',?),updated_at=?
		WHERE order_number=? AND kind='shortage' AND status='sending'
		AND NOT EXISTS (SELECT 1 FROM json_each(sent_recipients) WHERE value=?)`,
		recipient, now(), orderNumber, recipient)
	return err
}
