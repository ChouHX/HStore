package store

import (
	"database/sql"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// UpsertOrder 写入/更新订单（保留本地业务字段）。
type UpsertOrderInput struct {
	OrderNumber string
	OuterNumber string
	GoodsID     int
	GoodsName   string
	Count       int
	Money       float64
	Status      int
	Result      string
	Platform    string
	UserUID     *int
	SaleMoney   float64
	CostMoney   float64
	Source      string
	Contact     string
}

// UpsertOrder 插入或更新订单。
func UpsertOrder(o UpsertOrderInput) {
	ts := now()
	_, _ = db.Must().Exec(`INSERT INTO orders(order_number, outer_number, goods_id, goods_name, count, money,
		status, result, platform, user_uid, sale_money, cost_money, source, settled, contact, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?)
		ON CONFLICT(order_number) DO UPDATE SET
		  status=excluded.status, result=excluded.result, money=excluded.money,
		  count=excluded.count, updated_at=excluded.updated_at,
		  contact=CASE WHEN excluded.contact<>'' THEN excluded.contact ELSE orders.contact END`,
		o.OrderNumber, o.OuterNumber, o.GoodsID, o.GoodsName, o.Count, o.Money, o.Status, o.Result,
		o.Platform, o.UserUID, o.SaleMoney, o.CostMoney, o.Source, o.Contact, ts, ts)
}

// UpsertCards 写入/更新卡券。
func UpsertCards(orderNumber string, cards []model.Card) {
	d := db.Must()
	ts := now()
	for _, c := range cards {
		_, _ = d.Exec(`INSERT INTO cards(card_id, order_number, card_no, card_pwd, status, cancel_request, result, created_at)
			VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT(card_id) DO UPDATE SET
			  card_no=excluded.card_no, card_pwd=excluded.card_pwd, status=excluded.status,
			  cancel_request=excluded.cancel_request, result=excluded.result`,
			c.CardID, orderNumber, c.CardNo, c.CardPwd, c.Status, c.CancelRequest, c.Result, ts)
	}
}

// GetOrder 读取订单（含卡券）。
func GetOrder(orderNumber string) *model.LocalOrder {
	d := db.Must()
	var o model.LocalOrder
	var uid sql.NullInt64
	e := d.QueryRow(`SELECT o.order_number, o.outer_number, o.goods_id, o.goods_name, o.count, o.money,
		o.status, COALESCE(o.result,''), COALESCE(o.platform,'other'), o.created_at, o.updated_at, o.user_uid,
		COALESCE(CASE WHEN u.role='guest' THEN '游客' ELSE u.username END,''), COALESCE(o.sale_money,0), COALESCE(o.cost_money,0),
		COALESCE(o.source,''), COALESCE(o.settled,0), COALESCE(o.contact,'')
		FROM orders o LEFT JOIN users u ON u.uid = o.user_uid WHERE o.order_number = ?`, orderNumber).
		Scan(&o.OrderNumber, &o.OuterNumber, &o.GoodsID, &o.GoodsName, &o.Count, &o.Money,
			&o.Status, &o.Result, &o.Platform, &o.CreatedAt, &o.UpdatedAt, &uid,
			&o.Username, &o.SaleMoney, &o.CostMoney, &o.Source, &o.Settled, &o.Contact)
	if e != nil {
		return nil
	}
	if uid.Valid {
		v := int(uid.Int64)
		o.UserUID = &v
	}
	o.Cards = listCards(orderNumber)
	return &o
}

func listCards(orderNumber string) []model.Card {
	rows, e := db.Must().Query(`SELECT card_id, card_no, COALESCE(card_pwd,''), status,
		COALESCE(cancel_request,0), COALESCE(result,'')
		FROM cards WHERE order_number = ? ORDER BY created_at ASC`, orderNumber)
	if e != nil {
		return []model.Card{}
	}
	defer rows.Close()
	out := []model.Card{}
	for rows.Next() {
		var c model.Card
		if rows.Scan(&c.CardID, &c.CardNo, &c.CardPwd, &c.Status, &c.CancelRequest, &c.Result) == nil {
			out = append(out, c)
		}
	}
	return out
}

// OrderFilter 是订单筛选条件。
type OrderFilter struct {
	UID      int
	Statuses []int
	Keyword  string
	Limit    int
	Offset   int
}

// ListOrders 订单分页列表。
func ListOrders(f OrderFilter) []model.LocalOrder {
	d := db.Must()
	where := ""
	var args []any
	if f.UID > 0 {
		where += " AND o.user_uid = ?"
		args = append(args, f.UID)
	}
	if len(f.Statuses) > 0 {
		where += " AND o.status IN (" + joinInts(f.Statuses) + ")"
	}
	if f.Keyword != "" {
		where += " AND (o.order_number LIKE ? OR o.goods_name LIKE ?)"
		args = append(args, "%"+f.Keyword+"%", "%"+f.Keyword+"%")
	}
	if where != "" {
		where = "WHERE 1=1" + where
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	rows, e := d.Query(`SELECT o.order_number, o.outer_number, o.goods_id, o.goods_name, o.count, o.money,
		o.status, o.result, o.platform, o.created_at, o.updated_at, o.user_uid,
		COALESCE(CASE WHEN u.role='guest' THEN '游客' ELSE u.username END,''), o.sale_money, o.cost_money, o.source, o.settled
		FROM orders o LEFT JOIN users u ON u.uid = o.user_uid `+where+`
		ORDER BY o.created_at DESC LIMIT ? OFFSET ?`, append(args, limit, f.Offset)...)
	if e != nil {
		return []model.LocalOrder{}
	}
	defer rows.Close()
	out := []model.LocalOrder{}
	for rows.Next() {
		var o model.LocalOrder
		var uid sql.NullInt64
		if rows.Scan(&o.OrderNumber, &o.OuterNumber, &o.GoodsID, &o.GoodsName, &o.Count, &o.Money,
			&o.Status, &o.Result, &o.Platform, &o.CreatedAt, &o.UpdatedAt, &uid,
			&o.Username, &o.SaleMoney, &o.CostMoney, &o.Source, &o.Settled) == nil {
			if uid.Valid {
				v := int(uid.Int64)
				o.UserUID = &v
			}
			o.Cards = listCards(o.OrderNumber)
			out = append(out, o)
		}
	}
	return out
}

// CountOrders 订单总数。
func CountOrders(f OrderFilter) int {
	where := ""
	var args []any
	if f.UID > 0 {
		where += " AND o.user_uid = ?"
		args = append(args, f.UID)
	}
	if len(f.Statuses) > 0 {
		where += " AND o.status IN (" + joinInts(f.Statuses) + ")"
	}
	if f.Keyword != "" {
		where += " AND (o.order_number LIKE ? OR o.goods_name LIKE ?)"
		args = append(args, "%"+f.Keyword+"%", "%"+f.Keyword+"%")
	}
	if where != "" {
		where = "WHERE 1=1" + where
	}
	var c int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM orders o "+where, args...).Scan(&c)
	return c
}

// ApplyRemoteOrder 用上游订单详情更新本地记录。
func ApplyRemoteOrder(detail model.OrderDetail) *model.LocalOrder {
	existing := GetOrder(detail.OrderNumber)
	in := UpsertOrderInput{
		OrderNumber: detail.OrderNumber,
		OuterNumber: detail.OuterNumber,
		Count:       detail.Count,
		Money:       detail.Money,
		Status:      detail.Status,
		Result:      detail.Result,
		Platform:    "other",
		Source:      "admin",
	}
	if existing != nil {
		in.OuterNumber = firstNonEmpty(detail.OuterNumber, existing.OuterNumber)
		in.GoodsID = existing.GoodsID
		in.GoodsName = existing.GoodsName
		if detail.Count == 0 {
			in.Count = existing.Count
		}
		in.Money = detail.Money
		if detail.Status == 0 {
			in.Status = existing.Status
		}
		in.Platform = existing.Platform
		in.UserUID = existing.UserUID
		in.SaleMoney = existing.SaleMoney
		in.CostMoney = existing.CostMoney
		in.Source = existing.Source
	}
	UpsertOrder(in)
	if len(detail.Cards) > 0 {
		UpsertCards(detail.OrderNumber, detail.Cards)
	}
	return GetOrder(detail.OrderNumber)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// MarkSettled 标记订单已结算。
func MarkSettled(orderNumber string) {
	_, _ = db.Must().Exec("UPDATE orders SET settled = 1, updated_at = ? WHERE order_number = ?", now(), orderNumber)
}

// UpdateCardStatus 更新卡券状态。
func UpdateCardStatus(cardID string, status int, result string) {
	_, _ = db.Must().Exec("UPDATE cards SET status = ?, result = ? WHERE card_id = ?", status, result, cardID)
}

// CardOwner 返回卡券所属订单号。
func CardOwner(cardID string) string {
	var orderNumber string
	if e := db.Must().QueryRow("SELECT order_number FROM cards WHERE card_id = ?", cardID).Scan(&orderNumber); e != nil {
		return ""
	}
	return orderNumber
}

// CountUserOrders 用户订单数。
func CountUserOrders(uid int) int {
	var c int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM orders WHERE user_uid = ?", uid).Scan(&c)
	return c
}

// UserMailboxCards 列出用户所有「可取出邮箱」的卡券（card_no 形如 xxx@domain）。
// 取件页直接展示这份清单，用户不必记订单号。
func UserMailboxCards(uid, limit int) []model.MailboxItem {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, e := db.Must().Query(`
		SELECT c.order_number, c.card_id, c.card_no, c.status,
		       COALESCE(o.goods_name, ''), COALESCE(o.created_at, 0)
		FROM cards c
		JOIN orders o ON o.order_number = c.order_number
		WHERE o.user_uid = ? AND instr(c.card_no, '@') > 0
		ORDER BY o.created_at DESC, c.created_at ASC
		LIMIT ?`, uid, limit)
	if e != nil {
		return []model.MailboxItem{}
	}
	defer rows.Close()
	out := []model.MailboxItem{}
	for rows.Next() {
		var it model.MailboxItem
		if rows.Scan(&it.OrderNumber, &it.CardID, &it.Email, &it.Status,
			&it.GoodsName, &it.CreatedAt) == nil {
			out = append(out, it)
		}
	}
	return out
}
