import Database from "better-sqlite3";
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";

const DATA_DIR = path.join(process.cwd(), "data");
const DB_PATH = process.env.FAKA_DB_PATH || path.join(DATA_DIR, "faka.db");

declare global {
  // eslint-disable-next-line no-var
  var __fakaDb: Database.Database | undefined;
}

function createTables(db: Database.Database): void {
  db.exec(`
    -- ===== 商品/分类（上游同步） =====
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
      sales_price  REAL    NOT NULL DEFAULT 0,   -- 上游成本/原价
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
      stock_synced_at INTEGER NOT NULL DEFAULT 0
    );

    -- ===== 订单/卡券 =====
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

    -- ===== 用户 / 资产 =====
    CREATE TABLE IF NOT EXISTS users (
      uid          INTEGER PRIMARY KEY AUTOINCREMENT,
      username     TEXT NOT NULL UNIQUE,
      nickname     TEXT NOT NULL DEFAULT '',
      password     TEXT NOT NULL DEFAULT '',
      phone        TEXT NOT NULL DEFAULT '',
      email        TEXT NOT NULL DEFAULT '',
      role         TEXT NOT NULL DEFAULT 'user',   -- user | admin
      status       TEXT NOT NULL DEFAULT 'active', -- active | frozen
      balance      REAL NOT NULL DEFAULT 0,
      total_spent  REAL NOT NULL DEFAULT 0,        -- 累计实际消费（成长依据）
      level_id     INTEGER,
      level_locked INTEGER NOT NULL DEFAULT 0,     -- 锁定等级，禁止自动升降
      created_at   INTEGER NOT NULL,
      updated_at   INTEGER NOT NULL
    );

    -- 不可篡改账本（触发器禁止 UPDATE/DELETE）
    CREATE TABLE IF NOT EXISTS ledger (
      id             INTEGER PRIMARY KEY AUTOINCREMENT,
      uid            INTEGER NOT NULL,
      type           TEXT NOT NULL,   -- recharge | consume | redeem | admin_adjust
      amount         REAL NOT NULL,   -- 有符号
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

    -- ===== 定价规则 =====
    CREATE TABLE IF NOT EXISTS pricing_rules (
      id           INTEGER PRIMARY KEY AUTOINCREMENT,
      name         TEXT NOT NULL,
      scope        TEXT NOT NULL DEFAULT 'global',  -- global | category | product
      scope_id     TEXT NOT NULL DEFAULT '',        -- dirId 或 goodsId
      formula_type TEXT NOT NULL DEFAULT 'fixed_add', -- fixed_add|fixed_sub|ratio|mixed
      amount       REAL NOT NULL DEFAULT 0,          -- X 固定金额
      percent      REAL NOT NULL DEFAULT 0,          -- Y 百分比
      multiplier   REAL NOT NULL DEFAULT 1,          -- 倍率
      fixed        REAL NOT NULL DEFAULT 0,          -- 混合公式固定金额
      rounding     TEXT NOT NULL DEFAULT 'round2',   -- round2|ceil2|floor2|ceil1|round0
      priority     INTEGER NOT NULL DEFAULT 0,
      enabled      INTEGER NOT NULL DEFAULT 1,
      created_at   INTEGER NOT NULL,
      updated_at   INTEGER NOT NULL
    );

    -- ===== 会员等级 =====
    CREATE TABLE IF NOT EXISTS levels (
      id         INTEGER PRIMARY KEY AUTOINCREMENT,
      name       TEXT NOT NULL,
      threshold  REAL NOT NULL DEFAULT 0,   -- 累计消费门槛
      discount   REAL NOT NULL DEFAULT 1,   -- 折扣率 1 = 不打折
      sort       INTEGER NOT NULL DEFAULT 0,
      created_at INTEGER NOT NULL,
      updated_at INTEGER NOT NULL
    );

    -- ===== 兑换码 =====
    CREATE TABLE IF NOT EXISTS redeem_batches (
      id             INTEGER PRIMARY KEY AUTOINCREMENT,
      name           TEXT NOT NULL,
      face_value     REAL NOT NULL DEFAULT 0,
      count          INTEGER NOT NULL DEFAULT 0,
      used_count     INTEGER NOT NULL DEFAULT 0,
      start_at       INTEGER,              -- null = 立即
      end_at         INTEGER,              -- null = 永久
      per_user_limit INTEGER NOT NULL DEFAULT 0,  -- 0 = 每人仅 1 张；N = 每人最多 N 张
      status         TEXT NOT NULL DEFAULT 'active', -- active | voided
      created_at     INTEGER NOT NULL
    );

    CREATE TABLE IF NOT EXISTS redeem_codes (
      id         INTEGER PRIMARY KEY AUTOINCREMENT,
      batch_id   INTEGER NOT NULL DEFAULT 0,
      code       TEXT NOT NULL UNIQUE,
      face_value REAL NOT NULL DEFAULT 0,
      status     TEXT NOT NULL DEFAULT 'unused', -- unused | used | voided
      used_by    INTEGER,
      used_at    INTEGER,
      start_at   INTEGER,
      end_at     INTEGER,
      created_at INTEGER NOT NULL
    );

    CREATE TABLE IF NOT EXISTS redeem_failures (
      id         INTEGER PRIMARY KEY AUTOINCREMENT,
      uid        INTEGER NOT NULL,
      created_at INTEGER NOT NULL
    );

    -- ===== 加密充值（Bybit 子账户地址池） =====
    CREATE TABLE IF NOT EXISTS bybit_pool (
      id               INTEGER PRIMARY KEY AUTOINCREMENT,
      sub_uid          TEXT NOT NULL UNIQUE,
      username         TEXT NOT NULL DEFAULT '',
      address          TEXT NOT NULL DEFAULT '',
      address_trx      TEXT NOT NULL DEFAULT '',
      address_bsc      TEXT NOT NULL DEFAULT '',
      chain            TEXT NOT NULL DEFAULT 'TRX',
      status           TEXT NOT NULL DEFAULT 'available', -- available | reserved | disabled
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
      status         TEXT NOT NULL DEFAULT 'waiting', -- waiting | paid | expired | cancelled
      txid           TEXT UNIQUE,
      received_usdt  TEXT,
      credited       REAL,
      expires_at     INTEGER NOT NULL,
      created_at     INTEGER NOT NULL,
      paid_at        INTEGER
    );

    CREATE INDEX IF NOT EXISTS idx_crypto_orders_status ON crypto_orders(status);
    CREATE INDEX IF NOT EXISTS idx_crypto_orders_uid ON crypto_orders(uid);

    CREATE TABLE IF NOT EXISTS announcements (
      id         INTEGER PRIMARY KEY AUTOINCREMENT,
      type       TEXT NOT NULL DEFAULT 'normal', -- normal | popup
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
  `);
}

/** 给已存在的库补列（幂等） */
function migrate(db: Database.Database): void {
  const cols = (table: string) =>
    new Set(
      (db.prepare(`PRAGMA table_info(${table})`).all() as { name: string }[]).map((c) => c.name),
    );

  const productCols = cols("products");
  const addProduct: [string, string][] = [
    ["cost_price", "REAL NOT NULL DEFAULT 0"],
    ["final_price", "REAL NOT NULL DEFAULT 0"],
    ["enabled", "INTEGER NOT NULL DEFAULT 1"],
    ["manual_price", "REAL"],
    ["price_locked", "INTEGER NOT NULL DEFAULT 0"],
    ["stock_count", "INTEGER NOT NULL DEFAULT -1"],
    ["custom_name", "TEXT"],
    ["custom_desc", "TEXT"],
    ["custom_img", "TEXT"],
    ["root_category_id", "INTEGER"],
    ["stock_synced_at", "INTEGER NOT NULL DEFAULT 0"],
  ];
  for (const [name, def] of addProduct) {
    if (!productCols.has(name)) db.exec(`ALTER TABLE products ADD COLUMN ${name} ${def}`);
  }
  db.exec("UPDATE products SET cost_price = sales_price WHERE cost_price = 0 AND sales_price > 0");
  db.exec("UPDATE products SET final_price = sales_price WHERE final_price = 0 AND sales_price > 0");

  const orderCols = cols("orders");
  const addOrder: [string, string][] = [
    ["user_uid", "INTEGER"],
    ["sale_money", "REAL NOT NULL DEFAULT 0"],
    ["cost_money", "REAL NOT NULL DEFAULT 0"],
    ["source", "TEXT NOT NULL DEFAULT 'admin'"],
    ["settled", "INTEGER NOT NULL DEFAULT 0"],
  ];
  for (const [name, def] of addOrder) {
    if (!orderCols.has(name)) db.exec(`ALTER TABLE orders ADD COLUMN ${name} ${def}`);
  }
  // 新列就绪后再建索引
  db.exec("CREATE INDEX IF NOT EXISTS idx_orders_user ON orders(user_uid)");

  // 历史上 2 位小数舍入会让 0.031 这类小额成本被舍成 0.03（低于成本），统一改为 4 位
  db.exec("UPDATE pricing_rules SET rounding = 'round4' WHERE rounding = 'round2'");

  // 加密充值：地址池双链地址 + 订单申请 CNY
  const poolCols = cols("bybit_pool");
  if (!poolCols.has("address_trx")) db.exec("ALTER TABLE bybit_pool ADD COLUMN address_trx TEXT NOT NULL DEFAULT ''");
  if (!poolCols.has("address_bsc")) db.exec("ALTER TABLE bybit_pool ADD COLUMN address_bsc TEXT NOT NULL DEFAULT ''");
  const orderColsCrypto = cols("crypto_orders");
  if (!orderColsCrypto.has("requested_cny")) db.exec("ALTER TABLE crypto_orders ADD COLUMN requested_cny REAL NOT NULL DEFAULT 0");

  // 兑换码：补充生效/截止时间（无批次化）
  const codeCols = cols("redeem_codes");
  if (!codeCols.has("start_at")) db.exec("ALTER TABLE redeem_codes ADD COLUMN start_at INTEGER");
  if (!codeCols.has("end_at")) db.exec("ALTER TABLE redeem_codes ADD COLUMN end_at INTEGER");

  // 账本不可篡改
  db.exec(`
    CREATE TRIGGER IF NOT EXISTS ledger_no_update
      BEFORE UPDATE ON ledger BEGIN SELECT RAISE(ABORT, 'ledger is immutable'); END;
    CREATE TRIGGER IF NOT EXISTS ledger_no_delete
      BEFORE DELETE ON ledger BEGIN SELECT RAISE(ABORT, 'ledger is immutable'); END;
  `);
}

function hashPassword(password: string): string {
  const salt = crypto.randomBytes(8).toString("hex");
  const hash = crypto.scryptSync(password, salt, 32).toString("hex");
  return `${salt}:${hash}`;
}

export function verifyPassword(password: string, stored: string): boolean {
  const [salt, hash] = stored.split(":");
  if (!salt || !hash) return false;
  const test = crypto.scryptSync(password, salt, 32).toString("hex");
  return crypto.timingSafeEqual(Buffer.from(hash, "hex"), Buffer.from(test, "hex"));
}

export function hashNewPassword(password: string): string {
  return hashPassword(password);
}

function seed(db: Database.Database): void {
  const now = Date.now();

  // 默认定价规则：全站加价 10%，四舍五入 2 位
  const ruleCount = (db.prepare("SELECT COUNT(*) c FROM pricing_rules").get() as { c: number }).c;
  if (ruleCount === 0) {
    db.prepare(
      `INSERT INTO pricing_rules(name, scope, scope_id, formula_type, amount, percent, multiplier, fixed, rounding, priority, enabled, created_at, updated_at)
       VALUES('全站默认加价 10%', 'global', '', 'ratio', 0, 10, 1, 0, 'round4', 0, 1, ?, ?)`,
    ).run(now, now);
  }

  // 默认会员等级
  const levelCount = (db.prepare("SELECT COUNT(*) c FROM levels").get() as { c: number }).c;
  if (levelCount === 0) {
    const stmt = db.prepare(
      `INSERT INTO levels(name, threshold, discount, sort, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
    );
    stmt.run("普通会员", 0, 1, 0, now, now);
    stmt.run("白银会员", 500, 0.98, 1, now, now);
    stmt.run("黄金会员", 2000, 0.95, 2, now, now);
    stmt.run("钻石会员", 5000, 0.92, 3, now, now);
  }

  // 管理员 + 演示用户
  const userCount = (db.prepare("SELECT COUNT(*) c FROM users").get() as { c: number }).c;
  if (userCount === 0) {
    const stmt = db.prepare(
      `INSERT INTO users(username, nickname, password, phone, email, role, status, balance, total_spent, level_id, level_locked, created_at, updated_at)
       VALUES(?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, 0, ?, ?)`,
    );
    const basicLevel = (
      db.prepare("SELECT id FROM levels ORDER BY threshold ASC LIMIT 1").get() as { id: number }
    ).id;
    stmt.run("admin", "系统管理员", hashPassword("admin123"), "", "", "admin", 0, 0, basicLevel, now, now);
    stmt.run("demo", "演示用户", hashPassword("demo123"), "13800000000", "demo@example.com", "user", 100, 0, basicLevel, now, now);
  }

  // 首次启动时把环境变量迁移到数据库设置（之后以管理员页面配置为准）
  const insSetting = db.prepare("INSERT OR IGNORE INTO sync_meta(key, value) VALUES(?, ?)");
  const envSettings: [string, string | undefined][] = [
    ["tianqi_base_url", process.env.TIANQI_BASE_URL],
    ["tianqi_app_id", process.env.TIANQI_APP_ID],
    ["tianqi_app_secret", process.env.TIANQI_APP_SECRET],
  ];
  for (const [k, v] of envSettings) {
    if (v) insSetting.run(k, v);
  }

  // 加密充值默认配置（可在后台修改）
  const cryptoDefaults: [string, string | undefined][] = [
    ["bybit_api_key", process.env.BYBIT_API_KEY],
    ["bybit_api_secret", process.env.BYBIT_API_SECRET],
    ["bybit_chain", "TRX"],
    ["bybit_rate", "7.2"],
    ["bybit_min_usdt", "2"],
    ["bybit_pool_uids", "598363709,598363661"],
    ["bybit_enabled", "1"],
  ];
  for (const [k, v] of cryptoDefaults) {
    if (v) insSetting.run(k, v);
  }

  // 公告种子
  const annCount = (db.prepare("SELECT COUNT(*) c FROM announcements").get() as { c: number }).c;
  if (annCount === 0) {
    const insAnn = db.prepare(
      "INSERT INTO announcements(type, title, content, enabled, sort, created_at, updated_at) VALUES('normal', ?, ?, 1, ?, ?, ?)",
    );
    insAnn.run("系统公告", "系统已完成上游商品同步，价格与库存实时更新", 40, now, now);
    insAnn.run("新增专区", "新增「谷歌邮箱」企业号专区，支持批量采购", 30, now, now);
    insAnn.run("兑换码", "兑换码支持自定义有效期，过期自动失效", 20, now, now);
    insAnn.run("订单提示", "下单后请在「个人中心 - 我的订单」查看卡券", 10, now, now);
  }

  // 地址池：把配置里的 UID 补齐到表中（地址留空，首次使用时自动刷新）
  const poolUidsRaw =
    (db.prepare("SELECT value FROM sync_meta WHERE key = 'bybit_pool_uids'").get() as
      | { value: string }
      | undefined)?.value ?? "";
  const poolUids = poolUidsRaw.split(",").map((x) => x.trim()).filter(Boolean);
  const insPool = db.prepare(
    "INSERT OR IGNORE INTO bybit_pool(sub_uid, chain, status, updated_at) VALUES(?, 'TRX', 'available', ?)",
  );
  for (const uid of poolUids) insPool.run(uid, now);
}

function backfillRootCategory(db: Database.Database): void {
  const cats = db.prepare("SELECT id, parent_id FROM categories").all() as {
    id: number;
    parent_id: number | null;
  }[];
  if (cats.length === 0) return;
  const byId = new Map(cats.map((c) => [c.id, c]));
  const rootOf = (id: number): number | null => {
    let cur = byId.get(id);
    let guard = 0;
    while (cur && cur.parent_id != null && guard++ < 20) cur = byId.get(cur.parent_id);
    return cur ? cur.id : null;
  };
  const rows = db
    .prepare("SELECT goods_id, dir_ids, root_category_id FROM products WHERE root_category_id IS NULL")
    .all() as { goods_id: number; dir_ids: string; root_category_id: number | null }[];
  if (rows.length === 0) return;
  const upd = db.prepare("UPDATE products SET root_category_id = ? WHERE goods_id = ?");
  db.transaction(() => {
    for (const r of rows) {
      let root: number | null = null;
      try {
        for (const d of JSON.parse(r.dir_ids || "[]") as number[]) {
          root = rootOf(d);
          if (root != null) break;
        }
      } catch {
        /* ignore */
      }
      if (root != null) upd.run(root, r.goods_id);
    }
  })();
}

function init(): Database.Database {
  if (!fs.existsSync(DATA_DIR)) fs.mkdirSync(DATA_DIR, { recursive: true });
  const db = new Database(DB_PATH);
  db.pragma("journal_mode = WAL");
  db.pragma("foreign_keys = ON");
  createTables(db);
  migrate(db);
  seed(db);
  backfillRootCategory(db);
  return db;
}

export function getDb(): Database.Database {
  if (!global.__fakaDb) global.__fakaDb = init();
  return global.__fakaDb;
}

export function getMeta(key: string): string | null {
  const row = getDb()
    .prepare("SELECT value FROM sync_meta WHERE key = ?")
    .get(key) as { value: string } | undefined;
  return row?.value ?? null;
}

export function setMeta(key: string, value: string): void {
  getDb()
    .prepare(
      "INSERT INTO sync_meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
    )
    .run(key, value);
}
