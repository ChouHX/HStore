package store

import (
	"database/sql"
	"strconv"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// CryptoChain 是支持的链。
type CryptoChain struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	Display string `json:"display"`
}

// CryptoChains 是支持的链列表。
var CryptoChains = []CryptoChain{
	{Code: "TRX", Label: "TRON (TRC20)", Display: "TRC20"},
	{Code: "BSC", Label: "BSC (BEP20)", Display: "BEP20"},
}

// CryptoConfig 是加密充值配置。
type CryptoConfig struct {
	Enabled      bool    `json:"enabled"`
	Chain        string  `json:"chain"`
	ChainLabel   string  `json:"chainLabel"`
	ChainDisplay string  `json:"chainDisplay"`
	Rate         float64 `json:"rate"`
	MinUsdt      float64 `json:"minUsdt"`
}

// GetCryptoConfig 读取加密充值配置。
func GetCryptoConfig() CryptoConfig {
	chain := db.GetMeta("bybit_chain")
	if chain == "" {
		chain = "TRX"
	}
	info := CryptoChains[0]
	for _, c := range CryptoChains {
		if c.Code == chain {
			info = c
		}
	}
	enabled := db.GetMeta("bybit_enabled")
	if enabled == "" {
		enabled = "1"
	}
	rate := 7.2
	if v := db.GetMeta("bybit_rate"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil {
			rate = f
		}
	}
	minUsdt := 2.0
	if v := db.GetMeta("bybit_min_usdt"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil {
			minUsdt = f
		}
	}
	return CryptoConfig{
		Enabled: enabled == "1", Chain: info.Code,
		ChainLabel: info.Label, ChainDisplay: info.Display,
		Rate: rate, MinUsdt: minUsdt,
	}
}

// SaveCryptoConfig 保存加密充值配置。
func SaveCryptoConfig(patch map[string]any) {
	if v, ok := patch["enabled"].(bool); ok {
		val := "0"
		if v {
			val = "1"
		}
		_ = db.SetMeta("bybit_enabled", val)
	}
	if v, ok := patch["chain"].(string); ok && v != "" {
		for _, c := range CryptoChains {
			if c.Code == v {
				_ = db.SetMeta("bybit_chain", v)
			}
		}
	}
	if v, ok := patch["rate"].(float64); ok {
		_ = db.SetMeta("bybit_rate", strconv.FormatFloat(v, 'f', -1, 64))
	}
	if v, ok := patch["minUsdt"].(float64); ok {
		_ = db.SetMeta("bybit_min_usdt", strconv.FormatFloat(v, 'f', -1, 64))
	}
	if v, ok := patch["apiKey"].(string); ok && v != "" {
		_ = db.SetMeta("bybit_api_key", v)
	}
	if v, ok := patch["apiSecret"].(string); ok && v != "" {
		_ = db.SetMeta("bybit_api_secret", v)
	}
}

// ListPool 地址池列表。
func ListPool() []model.PoolSlot {
	rows, e := db.Must().Query(`SELECT id, sub_uid, username, address, address_trx, address_bsc, chain, status,
		current_order_id, reserved_until, updated_at FROM bybit_pool ORDER BY id ASC`)
	if e != nil {
		return []model.PoolSlot{}
	}
	defer rows.Close()
	out := []model.PoolSlot{}
	for rows.Next() {
		var p model.PoolSlot
		var orderID, reserved sql.NullInt64
		if rows.Scan(&p.ID, &p.SubUID, &p.Username, &p.Address, &p.AddressTrx, &p.AddressBsc,
			&p.Chain, &p.Status, &orderID, &reserved, &p.UpdatedAt) == nil {
			if orderID.Valid {
				v := int(orderID.Int64)
				p.CurrentOrderID = &v
			}
			if reserved.Valid {
				v := reserved.Int64
				p.ReservedUntil = &v
			}
			out = append(out, p)
		}
	}
	return out
}

// AddressColumn 返回链对应的地址列名。
func AddressColumn(chain string) string {
	if chain == "BSC" {
		return "address_bsc"
	}
	return "address_trx"
}

// SyncPoolUIDsSetting 把地址池 UID 同步到设置项。
func SyncPoolUIDsSetting() {
	rows, e := db.Must().Query("SELECT sub_uid FROM bybit_pool ORDER BY id ASC")
	if e != nil {
		return
	}
	defer rows.Close()
	var uids []string
	for rows.Next() {
		var uid string
		if rows.Scan(&uid) == nil {
			uids = append(uids, uid)
		}
	}
	joined := ""
	for i, u := range uids {
		if i > 0 {
			joined += ","
		}
		joined += u
	}
	_ = db.SetMeta("bybit_pool_uids", joined)
}

// AddPoolUID 添加子账户。
func AddPoolUID(subUID string) bool {
	if subUID == "" {
		return false
	}
	res, e := db.Must().Exec("INSERT OR IGNORE INTO bybit_pool(sub_uid, chain, status, updated_at) VALUES(?, ?, 'available', ?)",
		subUID, GetCryptoConfig().Chain, now())
	SyncPoolUIDsSetting()
	if e != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// RemovePoolSlot 移除子账户。
func RemovePoolSlot(id int) bool {
	res, e := db.Must().Exec("DELETE FROM bybit_pool WHERE id=? AND status<>'reserved'", id)
	SyncPoolUIDsSetting()
	if e != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// SetPoolSlotEnabled 启用/停用子账户。
func SetPoolSlotEnabled(id int, enabled bool) bool {
	status := "disabled"
	if enabled {
		status = "available"
	}
	res, e := db.Must().Exec("UPDATE bybit_pool SET status=?, updated_at=? WHERE id=? AND status<>'reserved'", status, now(), id)
	if e != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

const cryptoOrderCols = `id, uid, pool_id, sub_uid, address, chain, requested_cny, requested_usdt,
	usdt_amount, rate, credit_cny, status, txid, received_usdt, credited, expires_at, created_at, paid_at`

func scanCryptoOrder(sc interface{ Scan(...any) error }) (model.CryptoOrder, error) {
	var o model.CryptoOrder
	var txid, received sql.NullString
	var credited sql.NullFloat64
	var paidAt sql.NullInt64
	e := sc.Scan(&o.ID, &o.UID, &o.PoolID, &o.SubUID, &o.Address, &o.Chain, &o.RequestedCny,
		&o.RequestedUsdt, &o.UsdtAmount, &o.Rate, &o.CreditCny, &o.Status, &txid, &received,
		&credited, &o.ExpiresAt, &o.CreatedAt, &paidAt)
	for _, c := range CryptoChains {
		if c.Code == o.Chain {
			o.ChainLabel = c.Label
		}
	}
	if txid.Valid {
		o.Txid = &txid.String
	}
	if received.Valid {
		o.ReceivedUsdt = &received.String
	}
	if credited.Valid {
		v := credited.Float64
		o.Credited = &v
	}
	if paidAt.Valid {
		v := paidAt.Int64
		o.PaidAt = &v
	}
	return o, e
}

// GetCryptoOrder 取加密充值订单。
func GetCryptoOrder(id int) *model.CryptoOrder {
	row := db.Must().QueryRow("SELECT "+cryptoOrderCols+" FROM crypto_orders WHERE id = ?", id)
	o, e := scanCryptoOrder(row)
	if e != nil {
		return nil
	}
	return &o
}

// ListCryptoOrders 加密充值订单列表。
func ListCryptoOrders(uid, limit int) []model.CryptoOrder {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var e error
	if uid > 0 {
		rows, e = db.Must().Query("SELECT "+cryptoOrderCols+" FROM crypto_orders WHERE uid=? ORDER BY id DESC LIMIT ?", uid, limit)
	} else {
		rows, e = db.Must().Query("SELECT "+cryptoOrderCols+" FROM crypto_orders ORDER BY id DESC LIMIT ?", limit)
	}
	if e != nil {
		return []model.CryptoOrder{}
	}
	defer rows.Close()
	out := []model.CryptoOrder{}
	for rows.Next() {
		if o, e := scanCryptoOrder(rows); e == nil {
			out = append(out, o)
		}
	}
	return out
}

// ExpireCryptoOrders 过期未支付订单并释放地址。
func ExpireCryptoOrders() int {
	d := db.Must()
	rows, e := d.Query("SELECT id, pool_id FROM crypto_orders WHERE status='waiting' AND expires_at < ?", now())
	if e != nil {
		return 0
	}
	type item struct{ id, poolID int }
	var list []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.poolID) == nil {
			list = append(list, it)
		}
	}
	rows.Close()
	for _, it := range list {
		res, e := d.Exec("UPDATE crypto_orders SET status='expired' WHERE id=? AND status='waiting'", it.id)
		if e != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 1 {
			_, _ = d.Exec("UPDATE bybit_pool SET status='available', current_order_id=NULL, reserved_until=NULL WHERE id=? AND current_order_id=?",
				it.poolID, it.id)
		}
	}
	return len(list)
}

// ReservePoolSlot 分配一个可用地址并锁定。
func ReservePoolSlot(chain string, expires int64) (*model.PoolSlot, bool) {
	col := AddressColumn(chain)
	d := db.Must()
	row := d.QueryRow("SELECT id, sub_uid, address_trx, address_bsc FROM bybit_pool WHERE status='available' AND " + col + " <> '' ORDER BY id ASC LIMIT 1")
	var slot model.PoolSlot
	if e := row.Scan(&slot.ID, &slot.SubUID, &slot.AddressTrx, &slot.AddressBsc); e != nil {
		return nil, false
	}
	res, e := d.Exec("UPDATE bybit_pool SET status='reserved', reserved_until=?, updated_at=? WHERE id=? AND status='available'",
		expires, now(), slot.ID)
	if e != nil {
		return nil, false
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, false
	}
	if chain == "BSC" {
		slot.Address = slot.AddressBsc
	} else {
		slot.Address = slot.AddressTrx
	}
	return &slot, slot.Address != ""
}

// InsertCryptoOrder 写入充值订单。
func InsertCryptoOrder(uid, poolID int, subUID, address, chain string, cny float64, requestedUsdt, usdtAmount string, rate, creditCny float64, expires int64) int {
	res, e := db.Must().Exec(`INSERT INTO crypto_orders(uid,pool_id,sub_uid,address,chain,requested_cny,requested_usdt,usdt_amount,rate,credit_cny,status,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,'waiting',?,?)`,
		uid, poolID, subUID, address, chain, cny, requestedUsdt, usdtAmount, rate, creditCny, expires, now())
	if e != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	_, _ = db.Must().Exec("UPDATE bybit_pool SET current_order_id=? WHERE id=?", id, poolID)
	return int(id)
}

// UniqueExpectedAmount 生成不重复的待支付金额（尾数随机）。
func UniqueExpectedAmount(requested float64) (string, bool) {
	d := db.Must()
	for i := 0; i < 50; i++ {
		tail := float64(randInt(1, 99)) / 10000
		val := strconv.FormatFloat(requested+tail, 'f', 4, 64)
		var one int
		if e := d.QueryRow("SELECT 1 FROM crypto_orders WHERE status='waiting' AND usdt_amount=?", val).Scan(&one); e != nil {
			return val, true
		}
	}
	return "", false
}

// SettleCryptoOrder 订单到账结算。
func SettleCryptoOrder(order *model.CryptoOrder, txid, received, remark string) *model.CryptoOrder {
	d := db.Must()
	credited := round4(parseFloat(received) * order.Rate)
	tx, e := d.Begin()
	if e != nil {
		return order
	}
	defer tx.Rollback()
	res, e := tx.Exec("UPDATE crypto_orders SET status='paid', txid=?, received_usdt=?, credited=?, paid_at=? WHERE id=? AND status='waiting'",
		txid, received, credited, now(), order.ID)
	if e != nil {
		return order
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return GetCryptoOrder(order.ID)
	}
	var before float64
	if e := tx.QueryRow("SELECT balance FROM users WHERE uid=?", order.UID).Scan(&before); e == nil {
		after := round4(before + credited)
		_, _ = tx.Exec("UPDATE users SET balance=?, updated_at=? WHERE uid=?", after, now(), order.UID)
		_, _ = tx.Exec(`INSERT INTO ledger(uid,type,amount,balance_before,balance_after,order_number,code,operator_id,remark,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, order.UID, "recharge", credited, before, after,
			"crypto-"+strconvItoa(order.ID), "", nil, remark, now())
	}
	_, _ = tx.Exec("UPDATE bybit_pool SET status='available', current_order_id=NULL, reserved_until=NULL WHERE id=? AND current_order_id=?",
		order.PoolID, order.ID)
	_ = tx.Commit()
	return GetCryptoOrder(order.ID)
}

// TxidUsed 检查 txid 是否已被其它订单使用。
func TxidUsed(txid string, exceptID int) bool {
	var id int
	if e := db.Must().QueryRow("SELECT id FROM crypto_orders WHERE txid=?", txid).Scan(&id); e != nil {
		return false
	}
	return id != exceptID
}

// CancelCryptoOrder 取消订单并释放地址。
func CancelCryptoOrder(id int) {
	d := db.Must()
	res, e := d.Exec("UPDATE crypto_orders SET status='cancelled' WHERE id=? AND status='waiting'", id)
	if e != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 1 {
		_, _ = d.Exec("UPDATE bybit_pool SET status='available', current_order_id=NULL, reserved_until=NULL WHERE current_order_id=?", id)
	}
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func randInt(min, max int) int {
	if max <= min {
		return min
	}
	return min + int(now()%int64(max-min+1))
}
