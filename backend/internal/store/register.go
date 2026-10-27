package store

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"hstore/backend/internal/db"
	"hstore/backend/internal/model"
)

// SMTPConfig 是注册验证码邮件的发信配置，保存在 sync_meta 中。
type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"` // ssl（465）或 starttls（587）
	Username string `json:"username"`
	Password string `json:"password"`
	FromName string `json:"fromName"`
	Enabled  bool   `json:"enabled"`
}

const SKSMTPConfig = "smtp_config"

// GetSMTPConfig 读取发信配置，默认 Gmail + SSL 465。
func GetSMTPConfig() SMTPConfig {
	cfg := SMTPConfig{Host: "smtp.gmail.com", Port: 465, Security: "ssl", FromName: "HShop", Enabled: true}
	if raw := db.GetMeta(SKSMTPConfig); raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	if cfg.FromName == "" {
		cfg.FromName = "HShop"
	}
	return cfg
}

// Ready 表示发信配置是否可用。
func (c SMTPConfig) Ready() bool {
	return c.Enabled && c.Host != "" && c.Port > 0 && c.Username != "" && c.Password != ""
}

// SaveSMTPConfig 保存发信配置。
func SaveSMTPConfig(cfg SMTPConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return db.SetMeta(SKSMTPConfig, string(raw))
}

// Gmail 免费账号每天 SMTP 发信约 500 封，达到后当天不再尝试发送。
const RegisterDailyMailLimit = 500

// RegisterMailCountToday 统计今天已发送的注册验证码邮件数。
func RegisterMailCountToday() int {
	t := time.Now()
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).UnixMilli()
	var n int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM email_codes WHERE created_at >= ?", dayStart).Scan(&n)
	return n
}

// LatestRegisterCodeAt 返回该邮箱最近一次验证码的发送时间（毫秒），没有则为 0。
func LatestRegisterCodeAt(email string) int64 {
	var ts int64
	_ = db.Must().QueryRow("SELECT COALESCE(MAX(created_at), 0) FROM email_codes WHERE email = ?", email).Scan(&ts)
	return ts
}

// NewRegisterCode 生成并保存一个 6 位数字验证码，10 分钟有效。
func NewRegisterCode(email, ip string) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	_, err = db.Must().Exec("INSERT INTO email_codes(email, code, ip, attempts, consumed, expires_at, created_at) VALUES(?,?,?,0,0,?,?)",
		email, code, ip, now()+10*60*1000, now())
	if err != nil {
		return "", err
	}
	return code, nil
}

// DeleteRegisterCode 发送失败时删除验证码，避免留下不可用记录。
func DeleteRegisterCode(email, code string) {
	_, _ = db.Must().Exec("DELETE FROM email_codes WHERE email = ? AND code = ?", email, code)
}

// VerifyRegisterCode 校验验证码，最多允许 5 次错误尝试。
func VerifyRegisterCode(email, code string) (bool, string) {
	var id, attempts int
	var expect string
	var expiresAt int64
	err := db.Must().QueryRow(`SELECT id, code, attempts, expires_at FROM email_codes
		WHERE email = ? AND consumed = 0 ORDER BY id DESC LIMIT 1`, email).
		Scan(&id, &expect, &attempts, &expiresAt)
	if err != nil {
		return false, "请先获取验证码"
	}
	if attempts >= 5 {
		return false, "验证码错误次数过多，请重新获取"
	}
	if now() > expiresAt {
		return false, "验证码已过期，请重新获取"
	}
	if expect != code {
		_, _ = db.Must().Exec("UPDATE email_codes SET attempts = attempts + 1 WHERE id = ?", id)
		return false, "验证码不正确"
	}
	_, _ = db.Must().Exec("UPDATE email_codes SET consumed = 1 WHERE id = ?", id)
	return true, ""
}

// EmailRegistered 判断邮箱是否已注册（登录名即邮箱）。
func EmailRegistered(email string) bool {
	var n int
	_ = db.Must().QueryRow("SELECT COUNT(*) FROM users WHERE username = ? OR email = ?", email, email).Scan(&n)
	return n > 0
}

// CreateRegisteredUser 创建通过邮箱验证的正式用户，登录名为邮箱。
func CreateRegisteredUser(email, password, nickname string) (*model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if EmailRegistered(email) {
		return nil, errors.New("该邮箱已注册，请直接登录")
	}
	if nickname == "" {
		nickname = strings.SplitN(email, "@", 2)[0]
	}
	res, err := db.Must().Exec(`INSERT INTO users(username,nickname,password,phone,email,role,status,balance,total_spent,level_id,level_locked,created_at,updated_at)
		VALUES(?,?,?,'',?,'user','active',0,0,NULL,0,?,?)`,
		email, nickname, db.HashPassword(password), email, now(), now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errors.New("该邮箱已注册，请直接登录")
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetUser(int(id)), nil
}
