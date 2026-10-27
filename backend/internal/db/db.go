package db

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
	_ "modernc.org/sqlite"
)

var (
	once sync.Once
	conn *sql.DB
	err  error
)

// DBPath 返回数据库文件路径（FAKA_DB_PATH 优先，其次 ./data/faka.db）。
func DBPath() string {
	if p := os.Getenv("FAKA_DB_PATH"); p != "" {
		return p
	}
	return filepath.Join("data", "faka.db")
}

// Open 打开（并初始化）数据库，进程内单例。
func Open() (*sql.DB, error) {
	once.Do(func() {
		path := DBPath()
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		dsn := "file:" + filepath.ToSlash(path) +
			"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(ON)"
		conn, err = sql.Open("sqlite", dsn)
		if err != nil {
			return
		}
		// WAL 允许多读一写：给 4 个连接，避免「一条连接被卡住 → 整站所有接口全挂」。
		// 之前是 1，一旦某次请求把连接占住不放（网络/驱动异常），全站 DB 调用会永久排队。
		// 写并发靠 busy_timeout(10000) 串行化，不会出现写冲突报错。
		conn.SetMaxOpenConns(4)
		conn.SetMaxIdleConns(4)
		conn.SetConnMaxLifetime(30 * time.Minute)
		if err = conn.Ping(); err != nil {
			return
		}
		if err = createTables(conn); err != nil {
			return
		}
		if err = migrate(conn); err != nil {
			return
		}
		if err = seed(conn); err != nil {
			return
		}
		backfillRootCategory(conn)
	})
	return conn, err
}

// Close 关闭连接池。主要给测试用：Windows 上不关闭连接，临时目录删不掉。
// 注意 Open 是 sync.Once 单例，Close 之后不能再 Open。
func Close() error {
	if conn == nil {
		return nil
	}
	return conn.Close()
}

// Must 打开数据库，失败直接 panic。
func Must() *sql.DB {
	c, e := Open()
	if e != nil {
		panic(e)
	}
	return c
}

const schema = `
CREATE TABLE IF NOT EXISTS categories (
  id         INTEGER PRIMARY KEY,
  name       TEXT    NOT NULL,
  img        TEXT    NOT NULL DEFAULT '',
  parent_id  INTEGER,
  platform   TEXT    NOT NULL DEFAULT 'other',
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS products (
  goods_id     INTEGER PRIMARY KEY,
  name         TEXT    NOT NULL,
  face_value   REAL    NOT NULL DEFAULT 0,
  sales_price  REAL    NOT NULL DEFAULT 0,
  goods_type   INTEGER NOT NULL DEFAULT 1,
  warranty_days INTEGER NOT NULL DEFAULT 0,
  status       INTEGER NOT NULL DEFAULT 1,
  multiple     INTEGER NOT NULL DEFAULT 1,
  is_repeat    INTEGER NOT NULL DEFAULT 0,
  is_ref_order INTEGER NOT NULL DEFAULT 0,
  is_ref_money INTEGER NOT NULL DEFAULT 0,
  sku_type     INTEGER NOT NULL DEFAULT 0,
  img_url      TEXT    NOT NULL DEFAULT '',
  dir_ids      TEXT    NOT NULL DEFAULT '[]',
  platform     TEXT    NOT NULL DEFAULT 'other',
  updated_at   INTEGER NOT NULL,
  stock_count  INTEGER NOT NULL DEFAULT -1,
  custom_name  TEXT,
  custom_desc  TEXT,
  custom_img   TEXT,
  root_category_id INTEGER,
  stock_synced_at INTEGER NOT NULL DEFAULT 0,
  detail_json  TEXT    NOT NULL DEFAULT '',
  detail_synced_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS orders (
  order_number TEXT PRIMARY KEY,
  outer_number TEXT NOT NULL DEFAULT '',
  goods_id     INTEGER NOT NULL,
  goods_name   TEXT NOT NULL DEFAULT '',
  count        INTEGER NOT NULL DEFAULT 0,
  money        REAL NOT NULL DEFAULT 0,
  status       INTEGER NOT NULL DEFAULT 1,
  result       TEXT NOT NULL DEFAULT '',
  ret_money    INTEGER NOT NULL DEFAULT 0,
  platform     TEXT NOT NULL DEFAULT 'other',
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS cards (
  card_id        TEXT PRIMARY KEY,
  order_number   TEXT NOT NULL,
  card_no        TEXT NOT NULL DEFAULT '',
  card_pwd       TEXT NOT NULL DEFAULT '',
  status         INTEGER NOT NULL DEFAULT 0,
  cancel_request INTEGER NOT NULL DEFAULT 0,
  result         TEXT NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  uid          INTEGER PRIMARY KEY AUTOINCREMENT,
  username     TEXT NOT NULL UNIQUE,
  nickname     TEXT NOT NULL DEFAULT '',
  password     TEXT NOT NULL DEFAULT '',
  phone        TEXT NOT NULL DEFAULT '',
  email        TEXT NOT NULL DEFAULT '',
  role         TEXT NOT NULL DEFAULT 'user',
  status       TEXT NOT NULL DEFAULT 'active',
  balance      REAL NOT NULL DEFAULT 0,
  total_spent  REAL NOT NULL DEFAULT 0,
  level_id     INTEGER,
  level_locked INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS ledger (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  uid            INTEGER NOT NULL,
  type           TEXT NOT NULL,
  amount         REAL NOT NULL,
  balance_before REAL NOT NULL,
  balance_after  REAL NOT NULL,
  order_number   TEXT NOT NULL DEFAULT '',
  code           TEXT NOT NULL DEFAULT '',
  operator_id    INTEGER,
  remark         TEXT NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  operator_id INTEGER,
  action      TEXT NOT NULL,
  target_type TEXT NOT NULL DEFAULT '',
  target_id   TEXT NOT NULL DEFAULT '',
  before_json TEXT NOT NULL DEFAULT '',
  after_json  TEXT NOT NULL DEFAULT '',
  remark      TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS pricing_rules (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  name         TEXT NOT NULL,
  scope        TEXT NOT NULL DEFAULT 'global',
  scope_id     TEXT NOT NULL DEFAULT '',
  formula_type TEXT NOT NULL DEFAULT 'fixed_add',
  amount       REAL NOT NULL DEFAULT 0,
  percent      REAL NOT NULL DEFAULT 0,
  multiplier   REAL NOT NULL DEFAULT 1,
  fixed        REAL NOT NULL DEFAULT 0,
  rounding     TEXT NOT NULL DEFAULT 'round2',
  priority     INTEGER NOT NULL DEFAULT 0,
  enabled      INTEGER NOT NULL DEFAULT 1,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS levels (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL,
  threshold  REAL NOT NULL DEFAULT 0,
  discount   REAL NOT NULL DEFAULT 1,
  sort       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS redeem_batches (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  name           TEXT NOT NULL,
  face_value     REAL NOT NULL DEFAULT 0,
  count          INTEGER NOT NULL DEFAULT 0,
  used_count     INTEGER NOT NULL DEFAULT 0,
  start_at       INTEGER,
  end_at         INTEGER,
  per_user_limit INTEGER NOT NULL DEFAULT 0,
  status         TEXT NOT NULL DEFAULT 'active',
  created_at     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS redeem_codes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id     INTEGER NOT NULL DEFAULT 0,
  code         TEXT NOT NULL UNIQUE,
  face_value   REAL NOT NULL DEFAULT 0,
  goods_id     INTEGER,
  goods_count  INTEGER NOT NULL DEFAULT 1,
  order_number TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'unused',
  used_by      INTEGER,
  used_at      INTEGER,
  redeeming_at INTEGER,
  start_at     INTEGER,
  end_at       INTEGER,
  created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS redeem_failures (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  uid        INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS bybit_pool (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  sub_uid          TEXT NOT NULL UNIQUE,
  username         TEXT NOT NULL DEFAULT '',
  address          TEXT NOT NULL DEFAULT '',
  address_trx      TEXT NOT NULL DEFAULT '',
  address_bsc      TEXT NOT NULL DEFAULT '',
  chain            TEXT NOT NULL DEFAULT 'TRX',
  status           TEXT NOT NULL DEFAULT 'available',
  current_order_id INTEGER,
  reserved_until   INTEGER,
  updated_at       INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS crypto_orders (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  uid            INTEGER NOT NULL,
  pool_id        INTEGER NOT NULL,
  sub_uid        TEXT NOT NULL,
  address        TEXT NOT NULL,
  chain          TEXT NOT NULL,
  requested_cny  REAL NOT NULL DEFAULT 0,
  requested_usdt TEXT NOT NULL,
  usdt_amount    TEXT NOT NULL,
  rate           REAL NOT NULL,
  credit_cny     REAL NOT NULL DEFAULT 0,
  status         TEXT NOT NULL DEFAULT 'waiting',
  txid           TEXT UNIQUE,
  received_usdt  TEXT,
  credited       REAL,
  expires_at     INTEGER NOT NULL,
  created_at     INTEGER NOT NULL,
  paid_at        INTEGER
);

CREATE INDEX IF NOT EXISTS idx_crypto_orders_status ON crypto_orders(status);
CREATE INDEX IF NOT EXISTS idx_crypto_orders_uid ON crypto_orders(uid);

CREATE TABLE IF NOT EXISTS alipay_orders (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  uid                INTEGER NOT NULL,
  order_no           TEXT NOT NULL UNIQUE,
  checkout_hash      TEXT NOT NULL UNIQUE,
  collection_code    TEXT NOT NULL DEFAULT '',
  requested_cents    INTEGER NOT NULL,
  payable_cents      INTEGER NOT NULL,
  payment_status     TEXT NOT NULL DEFAULT 'UNPAID',
  checkout_status    TEXT NOT NULL DEFAULT 'OPEN',
  payment_basis      TEXT NOT NULL DEFAULT 'NONE',
  received_cents     INTEGER,
  alipay_order_no    TEXT NOT NULL DEFAULT '',
  ledger_entry_id    INTEGER,
  credited           REAL,
  expires_at         INTEGER NOT NULL,
  ended_at           INTEGER NOT NULL DEFAULT 0,
  cooldown_until     INTEGER NOT NULL DEFAULT 0,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  paid_at            INTEGER,
  last_error         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_alipay_orders_uid ON alipay_orders(uid, created_at);
CREATE INDEX IF NOT EXISTS idx_alipay_orders_payable ON alipay_orders(payable_cents);
CREATE INDEX IF NOT EXISTS idx_alipay_orders_status ON alipay_orders(payment_status, checkout_status);

CREATE TABLE IF NOT EXISTS alipay_amount_slots (
  payable_cents  INTEGER PRIMARY KEY,
  order_id       INTEGER NOT NULL,
  occupied_from  INTEGER NOT NULL,
  released_at    INTEGER,
  cooldown_until INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_alipay_slots_order ON alipay_amount_slots(order_id);

CREATE TABLE IF NOT EXISTS alipay_ledger_entries (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  external_event_id     TEXT NOT NULL UNIQUE,
  account_log_id        TEXT NOT NULL DEFAULT '',
  occurred_at           INTEGER NOT NULL,
  occurred_precision_ms INTEGER NOT NULL DEFAULT 1000,
  amount_cents          INTEGER NOT NULL,
  direction             TEXT NOT NULL,
  alipay_order_no       TEXT NOT NULL DEFAULT '',
  merchant_order_no     TEXT NOT NULL DEFAULT '',
  trans_memo            TEXT NOT NULL DEFAULT '',
  other_account         TEXT NOT NULL DEFAULT '',
  state                 TEXT NOT NULL DEFAULT 'UNALLOCATED',
  order_id              INTEGER,
  raw_json              TEXT NOT NULL DEFAULT '',
  created_at            INTEGER NOT NULL,
  updated_at            INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_alipay_ledger_state ON alipay_ledger_entries(state, direction);
CREATE INDEX IF NOT EXISTS idx_alipay_ledger_amount ON alipay_ledger_entries(amount_cents, occurred_at);
CREATE INDEX IF NOT EXISTS idx_alipay_ledger_order ON alipay_ledger_entries(order_id);

CREATE TABLE IF NOT EXISTS alipay_scan_state (
  id              INTEGER PRIMARY KEY CHECK (id = 1),
  cursor_at       INTEGER NOT NULL DEFAULT 0,
  last_attempt_at INTEGER NOT NULL DEFAULT 0,
  last_success_at INTEGER NOT NULL DEFAULT 0,
  last_error      TEXT NOT NULL DEFAULT '',
  last_trace_id   TEXT NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO alipay_scan_state(id) VALUES(1);


CREATE TABLE IF NOT EXISTS guest_purchases (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  token            TEXT NOT NULL UNIQUE,
  uid              INTEGER NOT NULL,
  guest_user       INTEGER NOT NULL DEFAULT 1,
  channel          TEXT NOT NULL,
  payment_order_id INTEGER NOT NULL,
  goods_id         INTEGER NOT NULL,
  goods_name       TEXT NOT NULL DEFAULT '',
  goods_count      INTEGER NOT NULL DEFAULT 1,
  attach_json      TEXT NOT NULL DEFAULT '[]',
  amount_cny       REAL NOT NULL DEFAULT 0,
  status           TEXT NOT NULL DEFAULT 'waiting',
  upstream_order_no TEXT NOT NULL DEFAULT '',
  outer_number     TEXT NOT NULL UNIQUE,
  last_error       TEXT NOT NULL DEFAULT '',
  created_at       INTEGER NOT NULL,
  updated_at       INTEGER NOT NULL,
  paid_at          INTEGER,
  fulfilled_at     INTEGER
);

CREATE INDEX IF NOT EXISTS idx_guest_purchases_status ON guest_purchases(status, created_at);
CREATE INDEX IF NOT EXISTS idx_guest_purchases_payment ON guest_purchases(channel, payment_order_id);

CREATE TABLE IF NOT EXISTS announcements (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  type       TEXT NOT NULL DEFAULT 'normal',
  title      TEXT NOT NULL DEFAULT '',
  content    TEXT NOT NULL DEFAULT '',
  enabled    INTEGER NOT NULL DEFAULT 1,
  sort       INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_products_platform ON products(platform);
CREATE INDEX IF NOT EXISTS idx_cards_order ON cards(order_number);
CREATE INDEX IF NOT EXISTS idx_orders_created ON orders(created_at);
CREATE INDEX IF NOT EXISTS idx_ledger_uid ON ledger(uid, created_at);
CREATE INDEX IF NOT EXISTS idx_redeem_codes_batch ON redeem_codes(batch_id);
CREATE INDEX IF NOT EXISTS idx_redeem_failures ON redeem_failures(uid, created_at);
`

func createTables(db *sql.DB) error {
	_, e := db.Exec(schema)
	return e
}

// columns 返回表的列名集合。
func columns(db *sql.DB, table string) (map[string]bool, error) {
	rows, e := db.Query("PRAGMA table_info(" + table + ")")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if e := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); e != nil {
			return nil, e
		}
		out[name] = true
	}
	return out, rows.Err()
}

func addColumns(db *sql.DB, table string, defs [][2]string) error {
	cols, e := columns(db, table)
	if e != nil {
		return e
	}
	for _, d := range defs {
		if cols[d[0]] {
			continue
		}
		if _, e := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, d[0], d[1])); e != nil {
			return e
		}
	}
	return nil
}

func migrate(db *sql.DB) error {
	if e := addColumns(db, "products", [][2]string{
		{"cost_price", "REAL NOT NULL DEFAULT 0"},
		{"final_price", "REAL NOT NULL DEFAULT 0"},
		{"enabled", "INTEGER NOT NULL DEFAULT 1"},
		{"manual_price", "REAL"},
		{"price_locked", "INTEGER NOT NULL DEFAULT 0"},
		{"stock_count", "INTEGER NOT NULL DEFAULT -1"},
		{"custom_name", "TEXT"},
		{"custom_desc", "TEXT"},
		{"custom_img", "TEXT"},
		{"root_category_id", "INTEGER"},
		{"stock_synced_at", "INTEGER NOT NULL DEFAULT 0"},
		// 上游商品详情原始 JSON 缓存：同步商品时写入，商详接口直接读本地，
		// 不再每次点击都回源（上游有 WAF 挑战，单次 0.7s+）。
		{"detail_json", "TEXT NOT NULL DEFAULT ''"},
		{"detail_synced_at", "INTEGER NOT NULL DEFAULT 0"},
	}); e != nil {
		return e
	}
	if _, e := db.Exec("UPDATE products SET cost_price = sales_price WHERE cost_price = 0 AND sales_price > 0"); e != nil {
		return e
	}
	if _, e := db.Exec("UPDATE products SET final_price = sales_price WHERE final_price = 0 AND sales_price > 0"); e != nil {
		return e
	}

	if e := addColumns(db, "orders", [][2]string{
		{"user_uid", "INTEGER"},
		{"sale_money", "REAL NOT NULL DEFAULT 0"},
		{"cost_money", "REAL NOT NULL DEFAULT 0"},
		{"source", "TEXT NOT NULL DEFAULT 'admin'"},
		{"settled", "INTEGER NOT NULL DEFAULT 0"},
		// 联系方式：游客下单必填，用于「订单查询」按联系方式检索
		{"contact", "TEXT NOT NULL DEFAULT ''"},
	}); e != nil {
		return e
	}
	if _, e := db.Exec("CREATE INDEX IF NOT EXISTS idx_orders_user ON orders(user_uid)"); e != nil {
		return e
	}
	if _, e := db.Exec("CREATE INDEX IF NOT EXISTS idx_orders_contact ON orders(contact)"); e != nil {
		return e
	}

	if _, e := db.Exec("UPDATE pricing_rules SET rounding = 'round4' WHERE rounding = 'round2'"); e != nil {
		return e
	}

	if e := addColumns(db, "bybit_pool", [][2]string{
		{"address_trx", "TEXT NOT NULL DEFAULT ''"},
		{"address_bsc", "TEXT NOT NULL DEFAULT ''"},
	}); e != nil {
		return e
	}
	if e := addColumns(db, "crypto_orders", [][2]string{
		{"requested_cny", "REAL NOT NULL DEFAULT 0"},
	}); e != nil {
		return e
	}
	if e := addColumns(db, "alipay_orders", [][2]string{
		{"ended_at", "INTEGER NOT NULL DEFAULT 0"},
		{"collection_code", "TEXT NOT NULL DEFAULT ''"},
	}); e != nil {
		return e
	}
	if _, e := db.Exec("UPDATE alipay_orders SET ended_at = expires_at WHERE ended_at = 0"); e != nil {
		return e
	}

	// 游客直购的联系方式：下单必填，用于「订单查询」按联系方式检索
	if e := addColumns(db, "guest_purchases", [][2]string{
		{"contact", "TEXT NOT NULL DEFAULT ''"},
	}); e != nil {
		return e
	}
	if _, e := db.Exec("CREATE INDEX IF NOT EXISTS idx_guest_purchases_contact ON guest_purchases(contact)"); e != nil {
		return e
	}

	if e := addColumns(db, "redeem_codes", [][2]string{
		{"start_at", "INTEGER"},
		{"end_at", "INTEGER"},
		{"goods_id", "INTEGER"},
		{"goods_count", "INTEGER NOT NULL DEFAULT 1"},
		{"order_number", "TEXT NOT NULL DEFAULT ''"},
		{"redeeming_at", "INTEGER"},
	}); e != nil {
		return e
	}
	if _, e := db.Exec("CREATE INDEX IF NOT EXISTS idx_redeem_codes_order ON redeem_codes(order_number)"); e != nil {
		return e
	}

	_, e := db.Exec(`
CREATE TRIGGER IF NOT EXISTS ledger_no_update
  BEFORE UPDATE ON ledger BEGIN SELECT RAISE(ABORT, 'ledger is immutable'); END;
CREATE TRIGGER IF NOT EXISTS ledger_no_delete
  BEFORE DELETE ON ledger BEGIN SELECT RAISE(ABORT, 'ledger is immutable'); END;
`)
	return e
}

// HashPassword 生成 "hexSalt:hexHash"。
// 与 Node 版 crypto.scryptSync(password, saltString, 32) 完全一致：
// Node 把 salt 当作字符串传入，用的是它的 UTF-8 字节，而非 hex 解码后的字节。
func HashPassword(password string) string {
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	salt := hex.EncodeToString(raw)
	key, e := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, 32)
	if e != nil {
		return ""
	}
	return salt + ":" + hex.EncodeToString(key)
}

// VerifyPassword 校验 scrypt 密码（兼容 Node 版写入的历史哈希）。
func VerifyPassword(password, stored string) bool {
	parts := strings.SplitN(stored, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	salt := parts[0]
	want, e := hex.DecodeString(parts[1])
	if e != nil || len(want) == 0 {
		return false
	}
	got, e := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, len(want))
	if e != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func seed(db *sql.DB) error {
	now := time.Now().UnixMilli()

	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM pricing_rules").Scan(&n)
	if n == 0 {
		if _, e := db.Exec(`INSERT INTO pricing_rules(name, scope, scope_id, formula_type, amount, percent, multiplier, fixed, rounding, priority, enabled, created_at, updated_at)
			VALUES('全站默认加价 10%', 'global', '', 'ratio', 0, 10, 1, 0, 'round4', 0, 1, ?, ?)`, now, now); e != nil {
			return e
		}
	}

	_ = db.QueryRow("SELECT COUNT(*) FROM levels").Scan(&n)
	if n == 0 {
		rows := [][3]any{
			{"普通会员", 0, 1.0},
			{"白银会员", 500, 0.98},
			{"黄金会员", 2000, 0.95},
			{"钻石会员", 5000, 0.92},
		}
		for i, r := range rows {
			if _, e := db.Exec("INSERT INTO levels(name, threshold, discount, sort, created_at, updated_at) VALUES(?,?,?,?,?,?)",
				r[0], r[1], r[2], i, now, now); e != nil {
				return e
			}
		}
	}

	_ = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	if n == 0 {
		var basic int
		_ = db.QueryRow("SELECT id FROM levels ORDER BY threshold ASC LIMIT 1").Scan(&basic)
		// 首次启动只建管理员；普通用户由站点自行注册 / 后台创建。
		if _, e := db.Exec(`INSERT INTO users(username, nickname, password, phone, email, role, status, balance, total_spent, level_id, level_locked, created_at, updated_at)
			VALUES('admin','系统管理员',?,'','','admin','active',0,0,?,0,?,?)`, HashPassword("admin123"), basic, now, now); e != nil {
			return e
		}
	}

	// 业务配置一律在管理后台维护（写入 sync_meta），不再从环境变量读取。
	metaDefaults := [][2]string{
		{"bybit_chain", "TRX"},
		{"bybit_rate", "7.2"},
		{"bybit_min_usdt", "2"},
		{"bybit_pool_uids", "598363709,598363661"},
		{"bybit_enabled", "1"},
		{"alipay_enabled", "0"},
		{"alipay_env", "production"},
		{"alipay_min_cny", "1"},
		{"alipay_max_cny", "5000"},
		{"alipay_product_name", "账户余额充值"},
		{"alipay_order_ttl_seconds", "300"},
		{"alipay_amount_offset_max_cents", "99"},
		{"alipay_cooldown_seconds", "600"},
		{"alipay_active_scan_seconds", "8"},
		{"alipay_normal_scan_seconds", "60"},
		{"alipay_safety_lag_seconds", "10"},
		{"alipay_lookback_seconds", "3600"},
	}
	for _, kv := range metaDefaults {
		if kv[1] == "" {
			continue
		}
		if _, e := db.Exec("INSERT OR IGNORE INTO sync_meta(key, value) VALUES(?, ?)", kv[0], kv[1]); e != nil {
			return e
		}
	}

	_ = db.QueryRow("SELECT COUNT(*) FROM announcements").Scan(&n)
	if n == 0 {
		rows := [][3]any{
			{"系统公告", "系统已完成上游商品同步，价格与库存实时更新", 40},
			{"新增专区", "新增「谷歌邮箱」企业号专区，支持批量采购", 30},
			{"兑换码", "兑换码支持自定义有效期，过期自动失效", 20},
			{"订单提示", "下单后请在「个人中心 - 我的订单」查看卡券", 10},
		}
		for _, r := range rows {
			if _, e := db.Exec("INSERT INTO announcements(type, title, content, enabled, sort, created_at, updated_at) VALUES('normal', ?, ?, 1, ?, ?, ?)",
				r[0], r[1], r[2], now, now); e != nil {
				return e
			}
		}
	}

	// 地址池：把配置里的 UID 补齐
	var uids string
	_ = db.QueryRow("SELECT value FROM sync_meta WHERE key = 'bybit_pool_uids'").Scan(&uids)
	for _, uid := range strings.Split(uids, ",") {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if _, e := db.Exec("INSERT OR IGNORE INTO bybit_pool(sub_uid, chain, status, updated_at) VALUES(?, 'TRX', 'available', ?)", uid, now); e != nil {
			return e
		}
	}
	return nil
}

// backfillRootCategory 给缺 root_category_id 的商品按 dir_ids 回填顶级板块。
func backfillRootCategory(db *sql.DB) {
	type cat struct {
		id     int
		parent *int
	}
	cats := map[int]cat{}
	rows, e := db.Query("SELECT id, parent_id FROM categories")
	if e != nil {
		return
	}
	for rows.Next() {
		var c cat
		if rows.Scan(&c.id, &c.parent) == nil {
			cats[c.id] = c
		}
	}
	rows.Close()
	if len(cats) == 0 {
		return
	}
	rootOf := func(id int) (int, bool) {
		cur, ok := cats[id]
		guard := 0
		for ok && cur.parent != nil && guard < 20 {
			cur, ok = cats[*cur.parent]
			guard++
		}
		if !ok {
			return 0, false
		}
		return cur.id, true
	}

	prows, e := db.Query("SELECT goods_id, dir_ids FROM products WHERE root_category_id IS NULL")
	if e != nil {
		return
	}
	type upd struct {
		goodsID int
		root    int
	}
	var list []upd
	for prows.Next() {
		var gid int
		var dirs string
		if prows.Scan(&gid, &dirs) != nil {
			continue
		}
		for _, d := range parseIntList(dirs) {
			if r, ok := rootOf(d); ok {
				list = append(list, upd{gid, r})
				break
			}
		}
	}
	prows.Close()
	for _, u := range list {
		_, _ = db.Exec("UPDATE products SET root_category_id = ? WHERE goods_id = ?", u.root, u.goodsID)
	}
}

// parseIntList 解析 JSON 数字数组（避免为小工具引入 encoding/json 依赖链）。
func parseIntList(s string) []int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if v, e := strconv.Atoi(p); e == nil {
			out = append(out, v)
		}
	}
	return out
}

// ===== sync_meta =====

// GetMeta 读配置项，不存在返回空串。
func GetMeta(key string) string {
	db := Must()
	var v string
	if e := db.QueryRow("SELECT value FROM sync_meta WHERE key = ?", key).Scan(&v); e != nil {
		return ""
	}
	return v
}

// SetMeta 写配置项。
func SetMeta(key, value string) error {
	_, e := Must().Exec("INSERT INTO sync_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return e
}

var (
	secretOnce sync.Once
	secretVal  string
)

// SessionSecret 返回会话签名密钥。
// 首次启动随机生成 32 字节并写入 sync_meta，之后固定不变：
// 既不需要任何配置，也避免硬编码默认值被用来伪造会话。
func SessionSecret() string {
	secretOnce.Do(func() {
		if v := GetMeta("session_secret"); v != "" {
			secretVal = v
			return
		}
		buf := make([]byte, 32)
		if _, e := rand.Read(buf); e != nil {
			// 极端情况下退化为进程内随机值，仅影响重启后的登录态
			secretVal = strconv.FormatInt(time.Now().UnixNano(), 36)
			return
		}
		secretVal = hex.EncodeToString(buf)
		_ = SetMeta("session_secret", secretVal)
	})
	return secretVal
}

// SignSession 生成 uid.hmac 形式的会话 token。
func SignSession(uid int) string {
	mac := hmac.New(sha256.New, []byte(SessionSecret()))
	mac.Write([]byte(strconv.Itoa(uid)))
	sig := hex.EncodeToString(mac.Sum(nil))[:24]
	return strconv.Itoa(uid) + "." + sig
}

// VerifySession 校验会话 token，返回 uid。
func VerifySession(token string) (int, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return 0, false
	}
	uid, e := strconv.Atoi(parts[0])
	if e != nil || uid <= 0 {
		return 0, false
	}
	want := SignSession(uid)
	wantSig := strings.SplitN(want, ".", 2)[1]
	if len(parts[1]) != len(wantSig) {
		return 0, false
	}
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(wantSig)) != 1 {
		return 0, false
	}
	return uid, true
}
