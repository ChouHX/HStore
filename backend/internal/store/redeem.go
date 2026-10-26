package store

import (
	"crypto/rand"
	"database/sql"
	"strconv"
	"strings"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// NormalizeCode 把用户输入的兑换码规整成 XXXX-XXXX-XXXX-XXXX。
// 允许大小写混写、带或不带分隔符、前后有空格；其他字符一律丢弃。
// 长度不是 16 位时原样返回，交给调用方按其他含义处理（比如订单号）。
func NormalizeCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if len(digits) != 16 {
		return strings.ToUpper(strings.TrimSpace(raw))
	}
	out := make([]byte, 0, 19)
	for i := 0; i < 16; i++ {
		if i > 0 && i%4 == 0 {
			out = append(out, '-')
		}
		out = append(out, digits[i])
	}
	return string(out)
}

const codeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// RandomRedeemCode 生成 XXXX-XXXX-XXXX-XXXX 形式的兑换码。
func RandomRedeemCode() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	out := make([]byte, 0, 19)
	for i := 0; i < 16; i++ {
		if i > 0 && i%4 == 0 {
			out = append(out, '-')
		}
		out = append(out, codeCharset[int(buf[i])%len(codeCharset)])
	}
	return string(out)
}

const codeSelect = `SELECT rc.id, rc.code, rc.face_value, rc.goods_id, rc.goods_count, rc.order_number,
	rc.status, rc.used_by, rc.used_at, rc.start_at, rc.end_at, rc.created_at, COALESCE(p.name,'')
	FROM redeem_codes rc LEFT JOIN products p ON p.goods_id = rc.goods_id`

func scanCode(sc interface{ Scan(...any) error }) (model.RedeemCode, error) {
	var c model.RedeemCode
	var goodsID, usedBy sql.NullInt64
	var usedAt, startAt, endAt sql.NullInt64
	e := sc.Scan(&c.ID, &c.Code, &c.FaceValue, &goodsID, &c.GoodsCount, &c.OrderNumber,
		&c.Status, &usedBy, &usedAt, &startAt, &endAt, &c.CreatedAt, &c.GoodsName)
	if goodsID.Valid {
		v := int(goodsID.Int64)
		c.GoodsID = &v
	}
	if usedBy.Valid {
		v := int(usedBy.Int64)
		c.UsedBy = &v
	}
	if usedAt.Valid {
		v := usedAt.Int64
		c.UsedAt = &v
	}
	if startAt.Valid {
		v := startAt.Int64
		c.StartAt = &v
	}
	if endAt.Valid {
		v := endAt.Int64
		c.EndAt = &v
	}
	return c, e
}

// CodeFilter 是兑换码筛选条件。
type CodeFilter struct {
	Status  string
	Keyword string
	UsedBy  int
	Kind    string // balance | product
}

func buildCodeWhere(f CodeFilter) (string, []any) {
	var where []string
	var args []any
	if f.Status != "" && f.Status != "all" {
		where = append(where, "rc.status = ?")
		args = append(args, f.Status)
	}
	if f.Keyword != "" {
		where = append(where, "(rc.code LIKE ? OR rc.order_number LIKE ? OR p.name LIKE ?)")
		kw := strings.ToUpper(f.Keyword)
		args = append(args, "%"+kw+"%", "%"+kw+"%", "%"+f.Keyword+"%")
	}
	if f.UsedBy > 0 {
		where = append(where, "rc.used_by = ?")
		args = append(args, f.UsedBy)
	}
	if f.Kind == "product" {
		where = append(where, "rc.goods_id IS NOT NULL")
	}
	if f.Kind == "balance" {
		where = append(where, "rc.goods_id IS NULL")
	}
	if len(where) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

// CreateCodesInput 是生成兑换码入参。
type CreateCodesInput struct {
	FaceValue  float64
	GoodsID    *int
	GoodsCount int
	Count      int
	StartAt    *int64
	EndAt      *int64
}

// CreateCodes 批量生成兑换码。
func CreateCodes(in CreateCodesInput) int {
	d := db.Must()
	count := in.Count
	if count < 1 {
		count = 1
	}
	if count > 100000 {
		count = 100000
	}
	faceValue := in.FaceValue
	if in.GoodsID != nil {
		faceValue = 0
	}
	goodsCount := in.GoodsCount
	if goodsCount < 1 {
		goodsCount = 1
	}
	ts := now()
	inserted := 0
	for guard := 0; inserted < count && guard < count*5; guard++ {
		var goods any
		if in.GoodsID != nil {
			goods = *in.GoodsID
		}
		res, e := d.Exec(`INSERT OR IGNORE INTO redeem_codes(batch_id, code, face_value, goods_id, goods_count, status, start_at, end_at, created_at)
			VALUES(0, ?, ?, ?, ?, 'unused', ?, ?, ?)`,
			RandomRedeemCode(), faceValue, goods, goodsCount, in.StartAt, in.EndAt, ts)
		if e != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 1 {
			inserted++
		}
	}
	return inserted
}

// ListCodes 兑换码分页列表。
func ListCodes(f CodeFilter, limit, offset int) []model.RedeemCode {
	if limit <= 0 {
		limit = 20
	}
	where, args := buildCodeWhere(f)
	rows, e := db.Must().Query(codeSelect+" "+where+" ORDER BY rc.id DESC LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if e != nil {
		return []model.RedeemCode{}
	}
	defer rows.Close()
	out := []model.RedeemCode{}
	for rows.Next() {
		if c, e := scanCode(rows); e == nil {
			out = append(out, c)
		}
	}
	return out
}

// CountCodes 兑换码总数。
func CountCodes(f CodeFilter) int {
	where, args := buildCodeWhere(f)
	var c int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM redeem_codes rc LEFT JOIN products p ON p.goods_id = rc.goods_id "+where, args...).Scan(&c)
	return c
}

// ExportCodesCSV 导出兑换码 CSV。
func ExportCodesCSV(f CodeFilter) string {
	where, args := buildCodeWhere(f)
	rows, e := db.Must().Query(codeSelect+" "+where+" ORDER BY rc.id DESC", args...)
	if e != nil {
		return "code\n"
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("code,kind,goods_id,goods_name,goods_count,face_value,order_number,status,used_by,used_at,start_at,end_at\n")
	for rows.Next() {
		c, e := scanCode(rows)
		if e != nil {
			continue
		}
		kind := "balance"
		goodsID := ""
		goodsCount := ""
		face := TrimFloat(c.FaceValue)
		if c.GoodsID != nil {
			kind = "product"
			goodsID = strconvItoa(*c.GoodsID)
			goodsCount = strconvItoa(c.GoodsCount)
			face = ""
		}
		usedBy := ""
		if c.UsedBy != nil {
			usedBy = strconvItoa(*c.UsedBy)
		}
		b.WriteString(csvRow([]string{
			c.Code, kind, goodsID, c.GoodsName, goodsCount, face, c.OrderNumber, c.Status, usedBy,
			tsString(c.UsedAt), tsString(c.StartAt), tsString(c.EndAt),
		}))
		b.WriteString("\n")
	}
	return b.String()
}

func csvRow(cells []string) string {
	out := make([]string, len(cells))
	for i, c := range cells {
		if strings.ContainsAny(c, ",\"\n") {
			c = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
		}
		out[i] = c
	}
	return strings.Join(out, ",")
}

func tsString(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

// VoidCode 作废兑换码。
func VoidCode(id int) {
	_, _ = db.Must().Exec("UPDATE redeem_codes SET status = 'voided' WHERE id = ? AND status IN ('unused','redeeming')", id)
}

// GetCodeByID 按 id 取兑换码。
func GetCodeByID(id int) *model.RedeemCode {
	row := db.Must().QueryRow(codeSelect+" WHERE rc.id = ?", id)
	c, e := scanCode(row)
	if e != nil {
		return nil
	}
	return &c
}

// GetCodeByCode 按码值取兑换码。
func GetCodeByCode(code string) *model.RedeemCode {
	row := db.Must().QueryRow(codeSelect+" WHERE rc.code = ?", NormalizeCode(code))
	c, e := scanCode(row)
	if e != nil {
		return nil
	}
	return &c
}

// CodeByOrder 按订单号反查兑换码。
func CodeByOrder(orderNumber string) *model.RedeemCode {
	if orderNumber == "" {
		return nil
	}
	row := db.Must().QueryRow(codeSelect+" WHERE rc.order_number = ? LIMIT 1", orderNumber)
	c, e := scanCode(row)
	if e != nil {
		return nil
	}
	return &c
}

// ClaimCode 原子占用兑换码（unused / 超时的 redeeming → redeeming）。
func ClaimCode(id, uid int, lockMS int64) bool {
	ts := now()
	res, e := db.Must().Exec(`UPDATE redeem_codes SET status = 'redeeming', used_by = ?, redeeming_at = ?
		WHERE id = ? AND (status = 'unused' OR (status = 'redeeming' AND (redeeming_at IS NULL OR redeeming_at < ?)))`,
		uid, ts, id, ts-lockMS)
	if e != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// ReleaseCode 回滚占用（兑换失败时）。
func ReleaseCode(id int) {
	_, _ = db.Must().Exec("UPDATE redeem_codes SET status = 'unused', used_by = NULL, redeeming_at = NULL WHERE id = ? AND status = 'redeeming'", id)
}

// FinishCode 核销完成，记录订单号。
func FinishCode(id int, orderNumber string) {
	_, _ = db.Must().Exec("UPDATE redeem_codes SET status = 'used', used_at = ?, order_number = ?, redeeming_at = NULL WHERE id = ?",
		now(), orderNumber, id)
}

// UseBalanceCode 核销面值码（占用 + 置为已用）。
func UseBalanceCode(id, uid int) bool {
	res, e := db.Must().Exec("UPDATE redeem_codes SET status = 'used', used_by = ?, used_at = ? WHERE id = ? AND status = 'unused'",
		uid, now(), id)
	if e != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// RedeemBalanceCode 在同一事务内完成「占用面值码 + 余额入账 + 写账本」，
// 避免出现「码已用但未到账」的中间态。
func RedeemBalanceCode(id, uid int, faceValue float64) (bool, float64) {
	d := db.Must()
	tx, e := d.Begin()
	if e != nil {
		return false, 0
	}
	defer func() { _ = tx.Rollback() }()

	res, e := tx.Exec("UPDATE redeem_codes SET status = 'used', used_by = ?, used_at = ? WHERE id = ? AND status = 'unused'",
		uid, now(), id)
	if e != nil {
		return false, 0
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return false, 0
	}

	var before float64
	if e := tx.QueryRow("SELECT balance FROM users WHERE uid = ?", uid).Scan(&before); e != nil {
		return false, 0
	}
	after := round4(before + faceValue)
	if _, e := tx.Exec("UPDATE users SET balance = ?, updated_at = ? WHERE uid = ?", after, now(), uid); e != nil {
		return false, 0
	}
	if _, e := tx.Exec(`INSERT INTO ledger(uid, type, amount, balance_before, balance_after, order_number, code, operator_id, remark, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		uid, "redeem", round4(faceValue), before, after, "", "", nil, "兑换码核销", now()); e != nil {
		return false, 0
	}
	if e := tx.Commit(); e != nil {
		return false, 0
	}
	return true, after
}

// ===== 防刷 =====

const failWindowMS = 60_000
const failLimit = 5

// RecentFailureCount 一分钟内失败次数。
func RecentFailureCount(uid int) int {
	var c int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM redeem_failures WHERE uid = ? AND created_at > ?", uid, now()-failWindowMS).Scan(&c)
	return c
}

// RecordFailure 记录一次失败。
func RecordFailure(uid int) {
	_, _ = db.Must().Exec("INSERT INTO redeem_failures(uid, created_at) VALUES(?, ?)", uid, now())
}

// ClearFailures 清空失败记录。
func ClearFailures(uid int) {
	_, _ = db.Must().Exec("DELETE FROM redeem_failures WHERE uid = ?", uid)
}

// FailLimit 返回失败次数上限。
func FailLimit() int { return failLimit }
