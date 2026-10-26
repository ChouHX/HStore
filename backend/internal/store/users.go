package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

const userSelect = `SELECT u.uid, u.username, u.nickname, u.phone, u.email, u.role, u.status,
	u.balance, u.total_spent, u.level_id, u.level_locked, u.created_at, u.updated_at,
	COALESCE(l.name, '') AS level_name
	FROM users u LEFT JOIN levels l ON l.id = u.level_id`

func scanUser(sc interface{ Scan(...any) error }) (model.User, error) {
	var u model.User
	var levelID sql.NullInt64
	e := sc.Scan(&u.UID, &u.Username, &u.Nickname, &u.Phone, &u.Email, &u.Role, &u.Status,
		&u.Balance, &u.TotalSpent, &levelID, &u.LevelLocked, &u.CreatedAt, &u.UpdatedAt, &u.LevelName)
	if levelID.Valid {
		v := int(levelID.Int64)
		u.LevelID = &v
	}
	return u, e
}

// GetUser 按 uid 取用户。
func GetUser(uid int) *model.User {
	row := db.Must().QueryRow(userSelect+" WHERE u.uid = ?", uid)
	u, e := scanUser(row)
	if e != nil {
		return nil
	}
	return &u
}

// GetUserByUsername 按用户名取用户。
func GetUserByUsername(username string) *model.User {
	row := db.Must().QueryRow(userSelect+" WHERE u.username = ?", username)
	u, e := scanUser(row)
	if e != nil {
		return nil
	}
	return &u
}

// Authenticate 校验用户名密码。
func Authenticate(username, password string) *model.User {
	var uid int
	var hash string
	e := db.Must().QueryRow("SELECT uid, password FROM users WHERE username = ?", username).Scan(&uid, &hash)
	if e != nil {
		return nil
	}
	if !db.VerifyPassword(password, hash) {
		return nil
	}
	return GetUser(uid)
}

// UserFilter 是用户列表筛选条件。
type UserFilter struct {
	Keyword string
	Status  string
	LevelID int
	Role    string
}

func buildUserWhere(f UserFilter) (string, []any) {
	var where []string
	var args []any
	if f.Role != "guest" {
		where = append(where, "u.role <> 'guest'")
	}
	if f.Keyword != "" {
		where = append(where, "(u.username LIKE ? OR u.nickname LIKE ? OR u.phone LIKE ? OR u.email LIKE ? OR CAST(u.uid AS TEXT) = ?)")
		args = append(args, "%"+f.Keyword+"%", "%"+f.Keyword+"%", "%"+f.Keyword+"%", "%"+f.Keyword+"%", f.Keyword)
	}
	if f.Status != "" {
		where = append(where, "u.status = ?")
		args = append(args, f.Status)
	}
	if f.LevelID > 0 {
		where = append(where, "u.level_id = ?")
		args = append(args, f.LevelID)
	}
	if f.Role != "" {
		where = append(where, "u.role = ?")
		args = append(args, f.Role)
	}
	if len(where) == 0 {
		return "", args
	}
	out := "WHERE "
	for i, w := range where {
		if i > 0 {
			out += " AND "
		}
		out += w
	}
	return out, args
}

// ListUsers 用户分页列表。
func ListUsers(f UserFilter, limit, offset int) []model.User {
	if limit <= 0 {
		limit = 20
	}
	where, args := buildUserWhere(f)
	rows, e := db.Must().Query(userSelect+" "+where+" ORDER BY u.uid ASC LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if e != nil {
		return []model.User{}
	}
	defer rows.Close()
	out := []model.User{}
	for rows.Next() {
		if u, e := scanUser(rows); e == nil {
			out = append(out, u)
		}
	}
	return out
}

// CountUsers 用户总数。
func CountUsers(f UserFilter) int {
	where, args := buildUserWhere(f)
	var c int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM users u "+where, args...).Scan(&c)
	return c
}

// UpdateUser 编辑用户资料。
func UpdateUser(uid int, patch map[string]any) *model.User {
	cur := GetUser(uid)
	if cur == nil {
		return nil
	}
	nickname, phone, email, status := cur.Nickname, cur.Phone, cur.Email, cur.Status
	if v, ok := patch["nickname"].(string); ok {
		nickname = v
	}
	if v, ok := patch["phone"].(string); ok {
		phone = v
	}
	if v, ok := patch["email"].(string); ok {
		email = v
	}
	if v, ok := patch["status"].(string); ok && v != "" {
		status = v
	}
	_, _ = db.Must().Exec("UPDATE users SET nickname=?, phone=?, email=?, status=?, updated_at=? WHERE uid=?",
		nickname, phone, email, status, now(), uid)
	return GetUser(uid)
}

// WriteAudit 写操作日志。
func WriteAudit(operatorID *int, action, targetType, targetID string, before, after any, remark string) {
	b, a := "", ""
	if before != nil {
		if raw, e := json.Marshal(before); e == nil {
			b = string(raw)
		}
	}
	if after != nil {
		if raw, e := json.Marshal(after); e == nil {
			a = string(raw)
		}
	}
	_, _ = db.Must().Exec(`INSERT INTO audit_logs(operator_id, action, target_type, target_id, before_json, after_json, remark, created_at)
		VALUES(?,?,?,?,?,?,?,?)`, operatorID, action, targetType, targetID, b, a, remark, now())
}

// ===== 账本 =====

// ledgerExec 让 insertLedger 既可在事务内也可在连接池上执行。
type ledgerExec interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func insertLedger(d ledgerExec, uid int, typ string, amount, before, after float64, orderNumber, code string, operatorID *int, remark string) {
	_, _ = d.Exec(`INSERT INTO ledger(uid, type, amount, balance_before, balance_after, order_number, code, operator_id, remark, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, uid, typ, round4(amount), before, after, orderNumber, code, operatorID, remark, now())
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// Credit 入账（不影响累计消费）。
func Credit(uid int, amount float64, typ string, code, orderNumber string, operatorID *int, remark string) (float64, float64) {
	d := db.Must()
	tx, e := d.Begin()
	if e != nil {
		return 0, 0
	}
	defer tx.Rollback()
	var before float64
	if e := tx.QueryRow("SELECT balance FROM users WHERE uid = ?", uid).Scan(&before); e != nil {
		return 0, 0
	}
	after := round4(before + amount)
	if _, e := tx.Exec("UPDATE users SET balance = ?, updated_at = ? WHERE uid = ?", after, now(), uid); e != nil {
		return 0, 0
	}
	insertLedger(tx, uid, typ, amount, before, after, orderNumber, code, operatorID, remark)
	_ = tx.Commit()
	return before, after
}

// Consume 扣余额。
func Consume(uid int, amount float64, orderNumber string) (bool, string, float64, float64) {
	d := db.Must()
	tx, e := d.Begin()
	if e != nil {
		return false, "扣款失败", 0, 0
	}
	defer tx.Rollback()
	var balance float64
	var status string
	if e := tx.QueryRow("SELECT balance, status FROM users WHERE uid = ?", uid).Scan(&balance, &status); e != nil {
		return false, "用户不存在", 0, 0
	}
	if status != "active" {
		return false, "账号已被冻结", 0, 0
	}
	if balance < amount {
		return false, "余额不足", 0, 0
	}
	after := round4(balance - amount)
	if _, e := tx.Exec("UPDATE users SET balance = ?, updated_at = ? WHERE uid = ?", after, now(), uid); e != nil {
		return false, "扣款失败", 0, 0
	}
	insertLedger(tx, uid, "consume", -amount, balance, after, orderNumber, "", nil, "购买商品")
	if e := tx.Commit(); e != nil {
		return false, "扣款失败", 0, 0
	}
	return true, "", balance, after
}

// RefundConsume 下单失败退回。
func RefundConsume(uid int, amount float64, orderNumber, remark string) {
	Credit(uid, amount, "recharge", "", orderNumber, nil, remark)
}

// AdjustBalance 管理员调整余额。
func AdjustBalance(uid int, delta float64, operatorID int, remark string) (bool, string, float64, float64) {
	d := db.Must()
	if remark == "" {
		var nick, name string
		_ = d.QueryRow("SELECT nickname, username FROM users WHERE uid = ?", operatorID).Scan(&nick, &name)
		who := nick
		if who == "" {
			who = name
		}
		if who == "" {
			who = "#" + strconvItoa(operatorID)
		}
		remark = "管理员调整（" + who + "）"
	}
	tx, e := d.Begin()
	if e != nil {
		return false, "调整失败", 0, 0
	}
	defer tx.Rollback()
	var before float64
	if e := tx.QueryRow("SELECT balance FROM users WHERE uid = ?", uid).Scan(&before); e != nil {
		return false, "用户不存在", 0, 0
	}
	after := round4(before + delta)
	if after < 0 {
		return false, "调整后余额不能为负", 0, 0
	}
	if _, e := tx.Exec("UPDATE users SET balance = ?, updated_at = ? WHERE uid = ?", after, now(), uid); e != nil {
		return false, "调整失败", 0, 0
	}
	insertLedger(tx, uid, "admin_adjust", delta, before, after, "", "", &operatorID, remark)
	if e := tx.Commit(); e != nil {
		return false, "调整失败", 0, 0
	}
	WriteAudit(&operatorID, "adjust_balance", "user", strconvItoa(uid),
		map[string]any{"balance": before}, map[string]any{"balance": after, "delta": delta}, remark)
	return true, "", before, after
}

// ListLedger 余额明细。
func ListLedger(uid int, limit, offset int) []model.LedgerEntry {
	if limit <= 0 {
		limit = 20
	}
	q := `SELECT l.id, l.uid, COALESCE(u.username,''), l.type, l.amount, l.balance_before, l.balance_after,
		l.order_number, l.code, l.operator_id, l.remark, l.created_at
		FROM ledger l LEFT JOIN users u ON u.uid = l.uid`
	var args []any
	if uid > 0 {
		q += " WHERE l.uid = ?"
		args = append(args, uid)
	}
	q += " ORDER BY l.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, e := db.Must().Query(q, args...)
	if e != nil {
		return []model.LedgerEntry{}
	}
	defer rows.Close()
	out := []model.LedgerEntry{}
	for rows.Next() {
		var it model.LedgerEntry
		var op sql.NullInt64
		if rows.Scan(&it.ID, &it.UID, &it.Username, &it.Type, &it.Amount, &it.BalanceBefore,
			&it.BalanceAfter, &it.OrderNumber, &it.Code, &op, &it.Remark, &it.CreatedAt) == nil {
			if op.Valid {
				v := int(op.Int64)
				it.OperatorID = &v
			}
			out = append(out, it)
		}
	}
	return out
}

// CountLedger 明细条数。
func CountLedger(uid int) int {
	var c int
	if uid > 0 {
		_ = db.Must().QueryRow("SELECT COUNT(*) FROM ledger WHERE uid = ?", uid).Scan(&c)
	} else {
		_ = db.Must().QueryRow("SELECT COUNT(*) FROM ledger").Scan(&c)
	}
	return c
}

// ListAudit 操作日志。
func ListAudit(limit, offset int) []model.AuditLog {
	if limit <= 0 {
		limit = 20
	}
	rows, e := db.Must().Query(`SELECT id, operator_id, action, target_type, target_id, before_json, after_json, remark, created_at
		FROM audit_logs ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if e != nil {
		return []model.AuditLog{}
	}
	defer rows.Close()
	out := []model.AuditLog{}
	for rows.Next() {
		var it model.AuditLog
		var op sql.NullInt64
		if rows.Scan(&it.ID, &op, &it.Action, &it.TargetType, &it.TargetID, &it.Before, &it.After, &it.Remark, &it.CreatedAt) == nil {
			if op.Valid {
				v := int(op.Int64)
				it.OperatorID = &v
			}
			out = append(out, it)
		}
	}
	return out
}

// CountAudit 日志条数。
func CountAudit() int {
	var c int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM audit_logs").Scan(&c)
	return c
}

// ===== 等级 =====

// ListLevels 列出会员等级。
func ListLevels() []model.Level {
	rows, e := db.Must().Query("SELECT id, name, threshold, discount, sort FROM levels ORDER BY threshold ASC, sort ASC")
	if e != nil {
		return []model.Level{}
	}
	defer rows.Close()
	out := []model.Level{}
	for rows.Next() {
		var l model.Level
		if rows.Scan(&l.ID, &l.Name, &l.Threshold, &l.Discount, &l.Sort) == nil {
			out = append(out, l)
		}
	}
	return out
}

// ResolveLevelID 按累计消费判断应属等级。
func ResolveLevelID(totalSpent float64) *int {
	var id int
	if e := db.Must().QueryRow("SELECT id FROM levels WHERE threshold <= ? ORDER BY threshold DESC LIMIT 1", totalSpent).Scan(&id); e != nil {
		return nil
	}
	return &id
}

// GetLevelDiscount 取等级折扣。
func GetLevelDiscount(levelID *int) float64 {
	if levelID == nil {
		return 1
	}
	var d float64
	if e := db.Must().QueryRow("SELECT discount FROM levels WHERE id = ?", *levelID).Scan(&d); e != nil {
		return 1
	}
	return d
}

func autoAssignLevel(tx *sql.Tx, uid int) *int {
	var levelID sql.NullInt64
	var locked int
	var spent float64
	if e := tx.QueryRow("SELECT level_id, level_locked, total_spent FROM users WHERE uid = ?", uid).Scan(&levelID, &locked, &spent); e != nil {
		return nil
	}
	if locked == 1 {
		if levelID.Valid {
			v := int(levelID.Int64)
			return &v
		}
		return nil
	}
	target := ResolveLevelID(spent)
	if target != nil && (!levelID.Valid || int(levelID.Int64) != *target) {
		_, _ = tx.Exec("UPDATE users SET level_id = ?, updated_at = ? WHERE uid = ?", *target, now(), uid)
		return target
	}
	if levelID.Valid {
		v := int(levelID.Int64)
		return &v
	}
	return nil
}

// SettleConsumption 订单结算：累计消费 + 自动升级。
func SettleConsumption(uid int, amount float64, orderNumber string) (float64, *int) {
	d := db.Must()
	tx, e := d.Begin()
	if e != nil {
		return 0, nil
	}
	defer tx.Rollback()
	if _, e := tx.Exec("UPDATE users SET total_spent = ROUND(total_spent + ?, 4), updated_at = ? WHERE uid = ?", amount, now(), uid); e != nil {
		return 0, nil
	}
	levelID := autoAssignLevel(tx, uid)
	var spent float64
	_ = tx.QueryRow("SELECT total_spent FROM users WHERE uid = ?", uid).Scan(&spent)
	_ = tx.Commit()
	WriteAudit(nil, "settle_consumption", "order", orderNumber,
		nil, map[string]any{"uid": uid, "amount": amount, "totalSpent": spent, "levelId": levelID}, "订单结算，累加消费额")
	return spent, levelID
}

// CreateLevel 新增等级。
func CreateLevel(name string, threshold, discount float64, sort int) *model.Level {
	ts := now()
	res, e := db.Must().Exec("INSERT INTO levels(name, threshold, discount, sort, created_at, updated_at) VALUES(?,?,?,?,?,?)",
		name, threshold, discount, sort, ts, ts)
	if e != nil {
		return nil
	}
	id, _ := res.LastInsertId()
	for _, l := range ListLevels() {
		if l.ID == int(id) {
			return &l
		}
	}
	return nil
}

// UpdateLevel 修改等级。
func UpdateLevel(id int, patch map[string]any) {
	var cur *model.Level
	for _, l := range ListLevels() {
		if l.ID == id {
			c := l
			cur = &c
			break
		}
	}
	if cur == nil {
		return
	}
	name, threshold, discount, sort := cur.Name, cur.Threshold, cur.Discount, cur.Sort
	if v, ok := patch["name"].(string); ok && v != "" {
		name = v
	}
	if v, ok := patch["threshold"].(float64); ok {
		threshold = v
	}
	if v, ok := patch["discount"].(float64); ok {
		discount = v
	}
	if v, ok := patch["sort"].(float64); ok {
		sort = int(v)
	}
	_, _ = db.Must().Exec("UPDATE levels SET name=?, threshold=?, discount=?, sort=?, updated_at=? WHERE id=?",
		name, threshold, discount, sort, now(), id)
}

// DeleteLevel 删除等级并重算用户等级。
func DeleteLevel(id int) {
	d := db.Must()
	_, _ = d.Exec("UPDATE users SET level_id = NULL WHERE level_id = ?", id)
	_, _ = d.Exec("DELETE FROM levels WHERE id = ?", id)
	RecomputeAllLevels()
}

// SetUserLevel 手动指定/锁定等级。
func SetUserLevel(uid int, levelID *int, locked bool, operatorID int) *model.User {
	before := GetUser(uid)
	if before == nil {
		return nil
	}
	lock := 0
	if locked {
		lock = 1
	}
	_, _ = db.Must().Exec("UPDATE users SET level_id = ?, level_locked = ?, updated_at = ? WHERE uid = ?", levelID, lock, now(), uid)
	if !locked && levelID == nil {
		if l := ResolveLevelID(before.TotalSpent); l != nil {
			_, _ = db.Must().Exec("UPDATE users SET level_id = ? WHERE uid = ?", *l, uid)
		}
	}
	after := GetUser(uid)
	WriteAudit(&operatorID, "set_level", "user", strconvItoa(uid),
		map[string]any{"levelId": before.LevelID, "locked": before.LevelLocked},
		map[string]any{"levelId": after.LevelID, "locked": after.LevelLocked}, "管理员调整会员等级")
	return after
}

// RecomputeAllLevels 重算未锁定用户的等级。
func RecomputeAllLevels() int {
	d := db.Must()
	rows, e := d.Query("SELECT uid FROM users WHERE level_locked = 0")
	if e != nil {
		return 0
	}
	var uids []int
	for rows.Next() {
		var uid int
		if rows.Scan(&uid) == nil {
			uids = append(uids, uid)
		}
	}
	rows.Close()
	for _, uid := range uids {
		tx, e := d.Begin()
		if e != nil {
			continue
		}
		autoAssignLevel(tx, uid)
		_ = tx.Commit()
	}
	return len(uids)
}

// ===== 公告 =====

// ListAnnouncements 列出公告。
func ListAnnouncements() []model.Announcement {
	rows, e := db.Must().Query("SELECT id, type, title, content, enabled, sort, created_at, updated_at FROM announcements ORDER BY type ASC, sort DESC, id DESC")
	if e != nil {
		return []model.Announcement{}
	}
	defer rows.Close()
	out := []model.Announcement{}
	for rows.Next() {
		var a model.Announcement
		if rows.Scan(&a.ID, &a.Type, &a.Title, &a.Content, &a.Enabled, &a.Sort, &a.CreatedAt, &a.UpdatedAt) == nil {
			out = append(out, a)
		}
	}
	return out
}

// PublicAnnouncements 返回前台公告（普通列表 + 唯一弹窗）。
func PublicAnnouncements() ([]model.Announcement, *model.Announcement) {
	normal := []model.Announcement{}
	var popup *model.Announcement
	for _, a := range ListAnnouncements() {
		if a.Enabled != 1 {
			continue
		}
		if a.Type == "popup" {
			if popup == nil {
				c := a
				popup = &c
			}
			continue
		}
		normal = append(normal, a)
	}
	return normal, popup
}

// SaveAnnouncement 新增/修改公告。
func SaveAnnouncement(id int, typ, title, content string, enabled bool, sort int) *model.Announcement {
	d := db.Must()
	ts := now()
	en := 0
	if enabled {
		en = 1
	}
	if typ == "popup" && enabled {
		_, _ = d.Exec("UPDATE announcements SET enabled=0, updated_at=? WHERE type='popup' AND id<>?", ts, id)
	}
	if id > 0 {
		_, _ = d.Exec("UPDATE announcements SET type=?, title=?, content=?, enabled=?, sort=?, updated_at=? WHERE id=?",
			typ, title, content, en, sort, ts, id)
	} else {
		res, e := d.Exec("INSERT INTO announcements(type, title, content, enabled, sort, created_at, updated_at) VALUES(?,?,?,?,?,?,?)",
			typ, title, content, en, sort, ts, ts)
		if e == nil {
			nid, _ := res.LastInsertId()
			id = int(nid)
		}
	}
	for _, a := range ListAnnouncements() {
		if a.ID == id {
			return &a
		}
	}
	return nil
}

// DeleteAnnouncement 删除公告。
func DeleteAnnouncement(id int) { _, _ = db.Must().Exec("DELETE FROM announcements WHERE id=?", id) }

// ToggleAnnouncement 启用/停用公告。
func ToggleAnnouncement(id int, enabled bool) {
	d := db.Must()
	ts := now()
	var typ string
	if e := d.QueryRow("SELECT type FROM announcements WHERE id=?", id).Scan(&typ); e != nil {
		return
	}
	if typ == "popup" && enabled {
		_, _ = d.Exec("UPDATE announcements SET enabled=0, updated_at=? WHERE type='popup' AND id<>?", ts, id)
	}
	en := 0
	if enabled {
		en = 1
	}
	_, _ = d.Exec("UPDATE announcements SET enabled=?, updated_at=? WHERE id=?", en, ts, id)
}

// ===== 设置 =====

const (
	SKBaseURL        = "tianqi_base_url"
	SKAppID          = "tianqi_app_id"
	SKAppSecret      = "tianqi_app_secret"
	SKCookie         = "tianqi_cookie"
	SKCookieAt       = "tianqi_cookie_at"
	SKRedeemPurchase = "redeem_purchase_url"
)

// TianqiSettings 是上游开放平台配置。
type TianqiSettings struct {
	BaseURL   string
	AppID     string
	AppSecret string
}

// GetTianqiSettings 读取上游配置（数据库优先，环境变量兜底）。
func GetTianqiSettings() TianqiSettings {
	s := TianqiSettings{
		BaseURL:   db.GetMeta(SKBaseURL),
		AppID:     db.GetMeta(SKAppID),
		AppSecret: db.GetMeta(SKAppSecret),
	}
	if s.BaseURL == "" {
		// 上游地址有内置默认值，AppID / AppSecret 必须在管理后台填写
		s.BaseURL = "https://tianqi996.com"
	}
	return s
}

// SaveTianqiSettings 保存上游配置（空值跳过）。
func SaveTianqiSettings(baseURL, appID, appSecret string) {
	if baseURL != "" {
		_ = db.SetMeta(SKBaseURL, stringsTrimRight(baseURL, "/"))
	}
	if appID != "" {
		_ = db.SetMeta(SKAppID, appID)
	}
	if appSecret != "" {
		_ = db.SetMeta(SKAppSecret, appSecret)
	}
}

// GetCookie 返回 WAF Cookie。
func GetCookie() string { return db.GetMeta(SKCookie) }

// GetCookieAt 返回 Cookie 获取时间。
func GetCookieAt() int64 {
	v := db.GetMeta(SKCookieAt)
	if v == "" {
		return 0
	}
	var n int64
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// SaveCookie 保存 WAF Cookie。
func SaveCookie(cookie string) {
	_ = db.SetMeta(SKCookie, cookie)
	_ = db.SetMeta(SKCookieAt, strconvItoa(int(now())))
}

// ClearCookie 清除 WAF Cookie。
func ClearCookie() {
	_ = db.SetMeta(SKCookie, "")
	_ = db.SetMeta(SKCookieAt, "0")
}

// GetRedeemPurchaseURL 返回兑换码购买链接。
func GetRedeemPurchaseURL() string { return db.GetMeta(SKRedeemPurchase) }

// SaveRedeemPurchaseURL 保存兑换码购买链接。
func SaveRedeemPurchaseURL(url string) { _ = db.SetMeta(SKRedeemPurchase, url) }

// MaskSecret 脱敏展示密钥。
func MaskSecret(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 8 {
		return "****"
	}
	return string(r[:4]) + "****" + string(r[len(r)-4:])
}

// ValidatePassword 校验新密码强度：6–64 位，不含空白以外的控制字符。
func ValidatePassword(pw string) error {
	if len(pw) < 6 {
		return errors.New("密码至少 6 位")
	}
	if len(pw) > 64 {
		return errors.New("密码最多 64 位")
	}
	for _, r := range pw {
		if r < 0x20 || r == 0x7f {
			return errors.New("密码不能包含控制字符")
		}
	}
	return nil
}

// ChangePassword 校验原密码后改成新密码（用户自助）。
func ChangePassword(uid int, oldPassword, newPassword string) (bool, string) {
	var hash string
	if e := db.Must().QueryRow("SELECT password FROM users WHERE uid = ?", uid).Scan(&hash); e != nil {
		return false, "用户不存在"
	}
	if !db.VerifyPassword(oldPassword, hash) {
		return false, "原密码不正确"
	}
	if db.VerifyPassword(newPassword, hash) {
		return false, "新密码不能与原密码相同"
	}
	return SetPassword(uid, newPassword)
}

// SetPassword 直接重置密码（管理员用，不校验原密码）。
func SetPassword(uid int, newPassword string) (bool, string) {
	res, e := db.Must().Exec("UPDATE users SET password = ?, updated_at = ? WHERE uid = ?",
		db.HashPassword(newPassword), now(), uid)
	if e != nil {
		return false, "修改密码失败"
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, "用户不存在"
	}
	return true, ""
}
