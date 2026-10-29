package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// CryptoChain 是支持的链。
type CryptoChain struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	Display string `json:"display"`
	// Contract 是该链上 USDT 的合约地址（TRX 为 base58，EVM 为 0x）。
	// 用于前端提示用户「别转错币」，以及结算时做链 + 合约双重校验。
	Contract string `json:"contract"`
	// EVM 为 true 时复用地址池的 address_evm（所有 EVM 链同一地址）。
	EVM bool `json:"evm"`
	// SlowConfirm 为 true 表示该链到账较慢，前端要提示用户耐心等待。
	SlowConfirm bool `json:"slowConfirm"`
	// ETA 是预计到账时间文案，仅 SlowConfirm 时使用。
	ETA string `json:"eta,omitempty"`
}

// CryptoChains 是支持的链列表。
//
// 注意：Arbitrum 与 Polygon 在 Bybit 上的 chainType 都是 USDT0，
// 链上 USDT 合约地址已固定写在这里，结算时用它防止同名不同币。
//
// Arbitrum / Polygon 的充值需要更多区块确认，实测到账明显慢于 TRC20，
// 所以标记为 SlowConfirm，前端会提示用户等待 5~10 分钟。
var CryptoChains = []CryptoChain{
	{Code: "TRX", Label: "TRON (TRC20)", Display: "TRC20",
		Contract: "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"},
	{Code: "BSC", Label: "BSC (BEP20)", Display: "BEP20", EVM: true,
		Contract: "0x55d398326f99059fF775485246999027B3197955"},
	{Code: "ARBI", Label: "Arbitrum One (USDT0)", Display: "Arbitrum", EVM: true,
		Contract: "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9",
		SlowConfirm: true, ETA: "5~10 分钟"},
	{Code: "MATIC", Label: "Polygon PoS (USDT0)", Display: "Polygon", EVM: true,
		Contract: "0xc2132D05D31c914a87C6611C10748AEb04B58e8F",
		SlowConfirm: true, ETA: "5~10 分钟"},
}

// ChainInfo 按 code 取链配置，找不到返回 nil。
func ChainInfo(code string) *CryptoChain {
	for i := range CryptoChains {
		if CryptoChains[i].Code == code {
			return &CryptoChains[i]
		}
	}
	return nil
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
	rows, e := db.Must().Query(`SELECT id, sub_uid, username, address, address_trx, address_bsc, address_evm, chain, status,
		current_order_id, reserved_until, updated_at FROM bybit_pool ORDER BY id ASC`)
	if e != nil {
		return []model.PoolSlot{}
	}
	defer rows.Close()
	out := []model.PoolSlot{}
	for rows.Next() {
		var p model.PoolSlot
		var orderID, reserved sql.NullInt64
		if rows.Scan(&p.ID, &p.SubUID, &p.Username, &p.Address, &p.AddressTrx, &p.AddressBsc, &p.AddressEvm,
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
// 所有 EVM 链（BSC / Arbitrum / Polygon）共用同一个充值地址。
func AddressColumn(chain string) string {
	if c := ChainInfo(chain); c != nil && c.EVM {
		return "address_evm"
	}
	return "address_trx"
}

// PickPoolSlot 按链选一个可用地址。
//
// 不再把子账户「冻结」给单个订单：地址池很小，一旦某个用户下单后
// 直接关网页，冻结会持续到过期（30 分钟）且阻塞其他人。
// 并发安全靠「每笔订单金额唯一」（见 UniqueExpectedAmount），
// 同一地址上同时存在多笔不同金额的待支付订单是允许的。
// PickPoolSlot 按链选一个地址。
//
// 排序规则（依次）：
//  1. 空闲地址（当前无待支付单）优先 —— 这样地址够用时天然就是「一人一址」，
//     同一个金额被不同用户同时充也不会串单
//  2. 都忙时选挂单最少的，把流量摊开
//  3. id 升序保证稳定
//
// 注意：不冻结地址。用户下单后关网页不会阻塞其他人，
// 过期只是状态流转，不影响地址可用性。
func PickPoolSlot(chain string) (*model.PoolSlot, bool) {
	col := AddressColumn(chain)
	row := db.Must().QueryRow(`SELECT p.id, p.sub_uid, p.address_trx, p.address_bsc, p.address_evm
		FROM bybit_pool p
		WHERE p.status='available' AND ` + col + ` <> ''
		ORDER BY (SELECT COUNT(*) FROM crypto_orders o
		          WHERE o.pool_id = p.id AND o.status = 'waiting') ASC, p.id ASC
		LIMIT 1`)
	var slot model.PoolSlot
	if e := row.Scan(&slot.ID, &slot.SubUID, &slot.AddressTrx, &slot.AddressBsc, &slot.AddressEvm); e != nil {
		return nil, false
	}
	switch {
	case col == "address_evm":
		slot.Address = slot.AddressEvm
	default:
		slot.Address = slot.AddressTrx
	}
	return &slot, slot.Address != ""
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

const cryptoOrderCols = `id, order_no, uid, pool_id, sub_uid, address, chain, requested_cny, requested_usdt,
	usdt_amount, rate, credit_cny, status, txid, received_usdt, credited, expires_at, created_at, paid_at`

func scanCryptoOrder(sc interface{ Scan(...any) error }) (model.CryptoOrder, error) {
	var o model.CryptoOrder
	var txid, received sql.NullString
	var credited sql.NullFloat64
	var paidAt sql.NullInt64
	e := sc.Scan(&o.ID, &o.OrderNo, &o.UID, &o.PoolID, &o.SubUID, &o.Address, &o.Chain, &o.RequestedCny,
		&o.RequestedUsdt, &o.UsdtAmount, &o.Rate, &o.CreditCny, &o.Status, &txid, &received,
		&credited, &o.ExpiresAt, &o.CreatedAt, &paidAt)
	for _, c := range CryptoChains {
		if c.Code == o.Chain {
			o.ChainLabel = c.Label
			o.ChainSlowConfirm = c.SlowConfirm
			o.ChainEta = c.ETA
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

// GetCryptoOrderByNo 按系统生成的支付单号取订单（用户端只认这个号）。
func GetCryptoOrderByNo(orderNo string) *model.CryptoOrder {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return nil
	}
	row := db.Must().QueryRow("SELECT "+cryptoOrderCols+" FROM crypto_orders WHERE order_no = ?", orderNo)
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

// ExpireCryptoOrders 把超时未支付的订单标为 expired。
//
// 地址不再被冻结，所以不需要「释放地址」；过期只是状态流转。
// 过期单仍保留在轮询范围内（见 pendingCryptoOrders），
// 以便用户实际在有效期内付了款但轮询错过了的能补结算。
func ExpireCryptoOrders() int {
	res, e := db.Must().Exec(
		"UPDATE crypto_orders SET status='expired' WHERE status='waiting' AND expires_at < ?", now())
	if e != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

// InsertCryptoOrder 写入充值订单。
// 单号由系统生成（与支付宝同一套规则），不沿用任何上游编号。
// insertOrderMu 串行化「选地址 + 定金额 + 插单」这一整段。
//
// PickPoolSlot 只读不占位（地址不再冻结），并发时多个请求可能同时
// 选到同一个空闲地址，导致「一人一址」退化成共用。下单频率很低，
// 用互斥锁换取分配确定性，比引入事务锁简单且可靠。
var insertOrderMu sync.Mutex

// NewCryptoOrderInput 是原子建单的入参。
type NewCryptoOrderInput struct {
	UID       int
	Chain     string
	CNY       float64
	Base      float64 // 基础 USDT 金额（未加尾数）
	Rate      float64
	Requested string // 展示用的申请金额（如 "1.00"）
	Expires   int64
}

// CreateCryptoOrderAtomic 原子地完成：选地址 → 定金额 → 插入订单。
//
// 金额策略：
//   - 选到空闲地址 → 直接用原始金额，不加尾数
//   - 所有地址都忙（或选到的地址已有挂单）→ 在该地址内做金额去重
//
// 返回订单 ID；失败返回 0 与错误。
func CreateCryptoOrderAtomic(in NewCryptoOrderInput) (int, error) {
	insertOrderMu.Lock()
	defer insertOrderMu.Unlock()

	slot, ok := PickPoolSlot(in.Chain)
	if !ok {
		return 0, errors.New("充值通道繁忙，请稍后重试")
	}

	var amount string
	if PoolSlotIsIdle(slot.ID) {
		amount = strconv.FormatFloat(in.Base, 'f', 4, 64)
	} else {
		var okAmt bool
		amount, okAmt = UniqueAmountOnSlot(slot.ID, in.Base)
		if !okAmt {
			return 0, errors.New("生成唯一支付金额失败，请重试")
		}
	}

	id := InsertCryptoOrder(in.UID, slot.ID, slot.SubUID, slot.Address, in.Chain,
		in.CNY, in.Requested, amount, in.Rate, round4(parseFloat(amount)*in.Rate), in.Expires)
	if id == 0 {
		return 0, errors.New("创建订单失败，请重试")
	}
	return id, nil
}

func InsertCryptoOrder(uid, poolID int, subUID, address, chain string, cny float64, requestedUsdt, usdtAmount string, rate, creditCny float64, expires int64) int {
	res, e := db.Must().Exec(`INSERT INTO crypto_orders(uid,order_no,pool_id,sub_uid,address,chain,requested_cny,requested_usdt,usdt_amount,rate,credit_cny,status,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'waiting',?,?)`,
		uid, NewOrderNo(PrefixCrypto), poolID, subUID, address, chain, cny, requestedUsdt, usdtAmount, rate, creditCny, expires, now())
	if e != nil {
		return 0
	}
	id, _ := res.LastInsertId()
	_, _ = db.Must().Exec("UPDATE bybit_pool SET current_order_id=? WHERE id=?", id, poolID)
	return int(id)
}

// UniqueExpectedAmount 生成不重复的待支付金额（尾数随机）。
//
// 既然不再冻结地址，同一地址上会同时存在多笔待支付单，金额唯一就是
// 区分它们的唯一依据，所以这里要严：
//   - 对「未结束」的订单（waiting / expired 未结算）都查重，因为过期但
//     用户实际已付款的单还可能补结算，金额不能被新单复用
//   - 尾数用 4 位小数（1~9999），空间足够大，避免同一金额下建单上限太低
//   - 随机源用 crypto/rand，不用时间戳：同一毫秒内并发请求会拿到相同值
//
// PoolSlotIsIdle 判断该地址当前是否没有任何待支付订单。
//
// 空闲地址可以独占给一个用户，此时金额直接用原值，不需要随机尾数。
func PoolSlotIsIdle(poolID int) bool {
	var n int
	_ = db.Must().QueryRow(`SELECT COUNT(*) FROM crypto_orders
		WHERE pool_id = ? AND status = 'waiting'`, poolID).Scan(&n)
	return n == 0
}

// UniqueAmountOnSlot 在**指定地址**内生成不重复的待支付金额。
//
// 关键：唯一性只需在同一地址内成立。不同地址各自收相同金额互不干扰，
// 因为到账匹配靠「地址 + 金额 + 链」三者，地址不同就不会串单。
// 这样同一地址上可以并发多笔不同金额的订单。
func UniqueAmountOnSlot(poolID int, requested float64) (string, bool) {
	d := db.Must()
	for i := 0; i < 200; i++ {
		tail := float64(cryptoRandInt(1, 9999)) / 10000
		val := strconv.FormatFloat(requested+tail, 'f', 4, 64)
		var one int
		// 只看这个地址上「未结束」的单（过期未结算的仍可能补结算，也要避开）
		e := d.QueryRow(`SELECT 1 FROM crypto_orders
			WHERE pool_id = ? AND usdt_amount = ? AND status IN ('waiting','expired')
			  AND COALESCE(txid,'') = '' LIMIT 1`, poolID, val).Scan(&one)
		if e != nil {
			return val, true
		}
	}
	return "", false
}

// UniqueExpectedAmount 在**全库**范围内生成不重复金额。
//
// 保留给需要全局唯一的场景；正常下单走 UniqueAmountOnSlot（按地址去重）。
func UniqueExpectedAmount(requested float64) (string, bool) {
	d := db.Must()
	for i := 0; i < 200; i++ {
		tail := float64(cryptoRandInt(1, 9999)) / 10000
		val := strconv.FormatFloat(requested+tail, 'f', 4, 64)
		var one int
		e := d.QueryRow(`SELECT 1 FROM crypto_orders
			WHERE usdt_amount = ? AND status IN ('waiting','expired')
			  AND COALESCE(txid,'') = '' LIMIT 1`, val).Scan(&one)
		if e != nil {
			return val, true
		}
	}
	return "", false
}

// cryptoRandInt 返回 [min, max] 内的密码学随机整数。
// 用于生成支付金额尾数，避免并发请求在同一毫秒拿到相同尾数而撞号。
func cryptoRandInt(min, max int) int {
	if max <= min {
		return min
	}
	n, e := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if e != nil {
		return min
	}
	return min + int(n.Int64())
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
	res, e := tx.Exec("UPDATE crypto_orders SET status='paid', txid=?, received_usdt=?, credited=?, paid_at=? WHERE id=? AND status IN ('waiting','expired')",
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
	// 地址不再被冻结，这里只清理历史遗留的占用字段（若有）
	_, _ = tx.Exec("UPDATE bybit_pool SET status='available', current_order_id=NULL, reserved_until=NULL WHERE current_order_id=?", order.ID)
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
		// 地址不再被冻结，只需清掉历史遗留的占用字段
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
