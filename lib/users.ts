import { getDb } from "./db";
import type { LedgerEntry, Level, User } from "./types";

interface UserRow {
  uid: number;
  username: string;
  nickname: string;
  phone: string;
  email: string;
  role: "user" | "admin";
  status: "active" | "frozen";
  balance: number;
  total_spent: number;
  level_id: number | null;
  level_locked: number;
  created_at: number;
  updated_at: number;
  level_name?: string;
}

function mapUser(r: UserRow): User {
  return {
    uid: r.uid,
    username: r.username,
    nickname: r.nickname,
    phone: r.phone,
    email: r.email,
    role: r.role,
    status: r.status,
    balance: r.balance,
    totalSpent: r.total_spent,
    levelId: r.level_id,
    levelName: r.level_name ?? "",
    levelLocked: r.level_locked,
    createdAt: r.created_at,
    updatedAt: r.updated_at,
  };
}

const USER_SELECT = `
  SELECT u.*, l.name AS level_name
  FROM users u LEFT JOIN levels l ON l.id = u.level_id
`;

export function getUser(uid: number): User | null {
  const r = getDb().prepare(`${USER_SELECT} WHERE u.uid = ?`).get(uid) as UserRow | undefined;
  return r ? mapUser(r) : null;
}

export function getUserByUsername(username: string): User | null {
  const r = getDb().prepare(`${USER_SELECT} WHERE u.username = ?`).get(username) as
    | UserRow
    | undefined;
  return r ? mapUser(r) : null;
}

export interface UserFilter {
  keyword?: string;
  status?: string;
  levelId?: number;
  role?: string;
}

export function listUsers(
  filter: UserFilter = {},
  args: { limit?: number; offset?: number } = {},
): User[] {
  const where: string[] = [];
  const params: Record<string, unknown> = {};
  if (filter.keyword) {
    where.push("(u.username LIKE @kw OR u.nickname LIKE @kw OR u.phone LIKE @kw OR u.email LIKE @kw OR CAST(u.uid AS TEXT) = @kwExact)");
    params.kw = `%${filter.keyword}%`;
    params.kwExact = filter.keyword;
  }
  if (filter.status) {
    where.push("u.status = @status");
    params.status = filter.status;
  }
  if (filter.levelId) {
    where.push("u.level_id = @levelId");
    params.levelId = filter.levelId;
  }
  if (filter.role) {
    where.push("u.role = @role");
    params.role = filter.role;
  }
  const sql = `${USER_SELECT} ${where.length ? "WHERE " + where.join(" AND ") : ""}
    ORDER BY u.uid ASC LIMIT @limit OFFSET @offset`;
  return (
    getDb()
      .prepare(sql)
      .all({ ...params, limit: args.limit ?? 20, offset: args.offset ?? 0 }) as UserRow[]
  ).map(mapUser);
}

export function countUsers(filter: UserFilter = {}): number {
  const where: string[] = [];
  const params: Record<string, unknown> = {};
  if (filter.keyword) {
    where.push("(u.username LIKE @kw OR u.nickname LIKE @kw OR u.phone LIKE @kw OR u.email LIKE @kw OR CAST(u.uid AS TEXT) = @kwExact)");
    params.kw = `%${filter.keyword}%`;
    params.kwExact = filter.keyword;
  }
  if (filter.status) {
    where.push("u.status = @status");
    params.status = filter.status;
  }
  if (filter.levelId) {
    where.push("u.level_id = @levelId");
    params.levelId = filter.levelId;
  }
  if (filter.role) {
    where.push("u.role = @role");
    params.role = filter.role;
  }
  const row = getDb()
    .prepare(`SELECT COUNT(*) c FROM users u ${where.length ? "WHERE " + where.join(" AND ") : ""}`)
    .get(params) as { c: number };
  return row.c;
}

export function updateUser(
  uid: number,
  patch: { nickname?: string; phone?: string; email?: string; status?: "active" | "frozen" },
): User | null {
  const cur = getUser(uid);
  if (!cur) return null;
  getDb()
    .prepare(
      `UPDATE users SET nickname=@nickname, phone=@phone, email=@email, status=@status, updated_at=@now WHERE uid=@uid`,
    )
    .run({
      uid,
      nickname: patch.nickname ?? cur.nickname,
      phone: patch.phone ?? cur.phone,
      email: patch.email ?? cur.email,
      status: patch.status ?? cur.status,
      now: Date.now(),
    });
  return getUser(uid);
}

export function writeAudit(entry: {
  operatorId?: number | null;
  action: string;
  targetType?: string;
  targetId?: string;
  before?: unknown;
  after?: unknown;
  remark?: string;
}): void {
  getDb()
    .prepare(
      `INSERT INTO audit_logs(operator_id, action, target_type, target_id, before_json, after_json, remark, created_at)
       VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
    )
    .run(
      entry.operatorId ?? null,
      entry.action,
      entry.targetType ?? "",
      entry.targetId ?? "",
      entry.before === undefined ? "" : JSON.stringify(entry.before),
      entry.after === undefined ? "" : JSON.stringify(entry.after),
      entry.remark ?? "",
      Date.now(),
    );
}

interface LedgerInput {
  uid: number;
  type: LedgerEntry["type"];
  amount: number;
  before: number;
  after: number;
  orderNumber?: string;
  code?: string;
  operatorId?: number | null;
  remark?: string;
}

function insertLedger(db: ReturnType<typeof getDb>, e: LedgerInput): void {
  db.prepare(
    `INSERT INTO ledger(uid, type, amount, balance_before, balance_after, order_number, code, operator_id, remark, created_at)
     VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
  ).run(
    e.uid,
    e.type,
    Number(e.amount.toFixed(4)),
    e.before,
    e.after,
    e.orderNumber ?? "",
    e.code ?? "",
    e.operatorId ?? null,
    e.remark ?? "",
    Date.now(),
  );
}

export function resolveLevelId(totalSpent: number): number | null {
  const row = getDb()
    .prepare("SELECT id FROM levels WHERE threshold <= ? ORDER BY threshold DESC LIMIT 1")
    .get(totalSpent) as { id: number } | undefined;
  return row?.id ?? null;
}

export function getLevelDiscount(levelId: number | null): number {
  if (!levelId) return 1;
  const row = getDb().prepare("SELECT discount FROM levels WHERE id = ?").get(levelId) as
    | { discount: number }
    | undefined;
  return row?.discount ?? 1;
}

/** 事务内按累计消费自动升级（若未锁定） */
function autoAssignLevel(db: ReturnType<typeof getDb>, uid: number): number | null {
  const u = db.prepare("SELECT level_id, level_locked, total_spent FROM users WHERE uid = ?").get(uid) as
    | { level_id: number | null; level_locked: number; total_spent: number }
    | undefined;
  if (!u || u.level_locked === 1) return u?.level_id ?? null;
  const target = resolveLevelId(u.total_spent);
  if (target && target !== u.level_id) {
    db.prepare("UPDATE users SET level_id = ?, updated_at = ? WHERE uid = ?").run(target, Date.now(), uid);
    return target;
  }
  return u.level_id;
}

/** 管理员手动调整余额（备注自动生成） */
export function adjustBalance(
  uid: number,
  delta: number,
  operatorId: number,
  remark = "",
): { ok: boolean; message?: string; before?: number; after?: number } {
  const db = getDb();
  let finalRemark = remark.trim();
  if (!finalRemark) {
    const op = db.prepare("SELECT nickname, username FROM users WHERE uid = ?").get(operatorId) as
      | { nickname?: string; username?: string }
      | undefined;
    finalRemark = `管理员调整（${op?.nickname || op?.username || `#${operatorId}`}）`;
  }
  try {
    const result = db.transaction(() => {
      const u = db.prepare("SELECT balance FROM users WHERE uid = ?").get(uid) as
        | { balance: number }
        | undefined;
      if (!u) throw new Error("用户不存在");
      const before = u.balance;
      const after = Number((before + delta).toFixed(4));
      if (after < 0) throw new Error("调整后余额不能为负");
      db.prepare("UPDATE users SET balance = ?, updated_at = ? WHERE uid = ?").run(after, Date.now(), uid);
      insertLedger(db, {
        uid,
        type: "admin_adjust",
        amount: delta,
        before,
        after,
        operatorId,
        remark: finalRemark,
      });
      writeAudit({
        operatorId,
        action: "adjust_balance",
        targetType: "user",
        targetId: String(uid),
        before: { balance: before },
        after: { balance: after, delta },
        remark: finalRemark,
      });
      return { before, after };
    })();
    return { ok: true, ...result };
  } catch (err) {
    return { ok: false, message: err instanceof Error ? err.message : "调整失败" };
  }
}

/** 入账：充值 / 兑换码 / 其它（不影响累计消费） */
export function credit(
  uid: number,
  amount: number,
  type: LedgerEntry["type"],
  opts: { code?: string; orderNumber?: string; operatorId?: number | null; remark?: string } = {},
): { before: number; after: number } {
  const db = getDb();
  return db.transaction(() => {
    const u = db.prepare("SELECT balance FROM users WHERE uid = ?").get(uid) as { balance: number };
    const before = u.balance;
    const after = Number((before + amount).toFixed(4));
    db.prepare("UPDATE users SET balance = ?, updated_at = ? WHERE uid = ?").run(after, Date.now(), uid);
    insertLedger(db, {
      uid,
      type,
      amount,
      before,
      after,
      code: opts.code,
      orderNumber: opts.orderNumber,
      operatorId: opts.operatorId,
      remark: opts.remark,
    });
    return { before, after };
  })();
}

/** 消费：扣余额 + 记账本（累计消费在订单结算时另计） */
export function consume(
  uid: number,
  amount: number,
  orderNumber: string,
): { ok: boolean; message?: string; before?: number; after?: number } {
  const db = getDb();
  try {
    const result = db.transaction(() => {
      const u = db.prepare("SELECT balance, status FROM users WHERE uid = ?").get(uid) as
        | { balance: number; status: string }
        | undefined;
      if (!u) throw new Error("用户不存在");
      if (u.status !== "active") throw new Error("账号已被冻结");
      if (u.balance < amount) throw new Error("余额不足");
      const before = u.balance;
      const after = Number((before - amount).toFixed(4));
      db.prepare("UPDATE users SET balance = ?, updated_at = ? WHERE uid = ?").run(after, Date.now(), uid);
      insertLedger(db, { uid, type: "consume", amount: -amount, before, after, orderNumber, remark: "购买商品" });
      return { before, after };
    })();
    return { ok: true, ...result };
  } catch (err) {
    return { ok: false, message: err instanceof Error ? err.message : "扣款失败" };
  }
}

/** 订单结算完成：累计实际消费 + 自动升级（幂等，由订单 settled 标记保证只调用一次） */
export function settleConsumption(
  uid: number,
  amount: number,
  orderNumber: string,
): { totalSpent: number; levelId: number | null } {
  const db = getDb();
  return db.transaction(() => {
    db.prepare(
      "UPDATE users SET total_spent = ROUND(total_spent + ?, 4), updated_at = ? WHERE uid = ?",
    ).run(amount, Date.now(), uid);
    const levelId = autoAssignLevel(db, uid);
    const row = db.prepare("SELECT total_spent FROM users WHERE uid = ?").get(uid) as {
      total_spent: number;
    };
    writeAudit({
      action: "settle_consumption",
      targetType: "order",
      targetId: orderNumber,
      after: { uid, amount, totalSpent: row.total_spent, levelId },
      remark: "订单结算，累加消费额",
    });
    return { totalSpent: row.total_spent, levelId };
  })();
}

/** 退款/补偿回滚消费（内部使用，不影响累计消费口径的展示一致性） */
export function refundConsume(uid: number, amount: number, orderNumber: string, remark = "下单失败退回"): void {
  const db = getDb();
  db.transaction(() => {
    const u = db.prepare("SELECT balance FROM users WHERE uid = ?").get(uid) as { balance: number };
    const before = u.balance;
    const after = Number((before + amount).toFixed(4));
    db.prepare("UPDATE users SET balance = ?, updated_at = ? WHERE uid = ?").run(after, Date.now(), uid);
    insertLedger(db, { uid, type: "recharge", amount, before, after, orderNumber, remark });
  })();
}

export function listLedger(uid?: number, args: { limit?: number; offset?: number } = {}): LedgerEntry[] {
  const rows = getDb()
    .prepare(
      `SELECT l.*, u.username FROM ledger l LEFT JOIN users u ON u.uid = l.uid
       ${uid ? "WHERE l.uid = ?" : ""} ORDER BY l.id DESC LIMIT ? OFFSET ?`,
    )
    .all(
      ...(uid
        ? [uid, args.limit ?? 20, args.offset ?? 0]
        : [args.limit ?? 20, args.offset ?? 0]),
    ) as (LedgerEntryRow & { username: string })[];
  return rows.map((r) => ({
    id: r.id,
    uid: r.uid,
    username: r.username ?? "",
    type: r.type,
    amount: r.amount,
    balanceBefore: r.balance_before,
    balanceAfter: r.balance_after,
    orderNumber: r.order_number,
    code: r.code,
    operatorId: r.operator_id,
    remark: r.remark,
    createdAt: r.created_at,
  }));
}

interface LedgerEntryRow {
  id: number;
  uid: number;
  type: LedgerEntry["type"];
  amount: number;
  balance_before: number;
  balance_after: number;
  order_number: string;
  code: string;
  operator_id: number | null;
  remark: string;
  created_at: number;
}

export interface AuditLog {
  id: number;
  operatorId: number | null;
  action: string;
  targetType: string;
  targetId: string;
  before: string;
  after: string;
  remark: string;
  createdAt: number;
}

export function countLedger(uid?: number): number {
  const row = getDb()
    .prepare(`SELECT COUNT(*) c FROM ledger ${uid ? "WHERE uid = ?" : ""}`)
    .get(...(uid ? [uid] : [])) as { c: number };
  return row.c;
}

export function listAudit(args: { limit?: number; offset?: number } = {}): AuditLog[] {
  const rows = getDb()
    .prepare("SELECT * FROM audit_logs ORDER BY id DESC LIMIT ? OFFSET ?")
    .all(args.limit ?? 20, args.offset ?? 0) as {
    id: number;
    operator_id: number | null;
    action: string;
    target_type: string;
    target_id: string;
    before_json: string;
    after_json: string;
    remark: string;
    created_at: number;
  }[];
  return rows.map((r) => ({
    id: r.id,
    operatorId: r.operator_id,
    action: r.action,
    targetType: r.target_type,
    targetId: r.target_id,
    before: r.before_json,
    after: r.after_json,
    remark: r.remark,
    createdAt: r.created_at,
  }));
}

export function countAudit(): number {
  return (getDb().prepare("SELECT COUNT(*) c FROM audit_logs").get() as { c: number }).c;
}

// ===== 会员等级 =====
export function listLevels(): Level[] {
  return (
    getDb().prepare("SELECT * FROM levels ORDER BY threshold ASC, sort ASC").all() as {
      id: number;
      name: string;
      threshold: number;
      discount: number;
      sort: number;
    }[]
  ).map((r) => ({ id: r.id, name: r.name, threshold: r.threshold, discount: r.discount, sort: r.sort }));
}

export function createLevel(input: { name: string; threshold: number; discount: number; sort?: number }): Level {
  const now = Date.now();
  const info = getDb()
    .prepare(
      "INSERT INTO levels(name, threshold, discount, sort, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)",
    )
    .run(input.name, input.threshold, input.discount, input.sort ?? 0, now, now);
  return listLevels().find((l) => l.id === Number(info.lastInsertRowid))!;
}

export function updateLevel(
  id: number,
  patch: { name?: string; threshold?: number; discount?: number; sort?: number },
): void {
  const cur = listLevels().find((l) => l.id === id);
  if (!cur) return;
  getDb()
    .prepare("UPDATE levels SET name=?, threshold=?, discount=?, sort=?, updated_at=? WHERE id=?")
    .run(
      patch.name ?? cur.name,
      patch.threshold ?? cur.threshold,
      patch.discount ?? cur.discount,
      patch.sort ?? cur.sort,
      Date.now(),
      id,
    );
}

export function deleteLevel(id: number): void {
  const db = getDb();
  db.transaction(() => {
    db.prepare("UPDATE users SET level_id = NULL WHERE level_id = ?").run(id);
    db.prepare("DELETE FROM levels WHERE id = ?").run(id);
    const users = db.prepare("SELECT uid FROM users").all() as { uid: number }[];
    for (const u of users) autoAssignLevel(db, u.uid);
  })();
}

/** 管理员手动指定等级 / 锁定等级 */
export function setUserLevel(
  uid: number,
  levelId: number | null,
  locked: boolean,
  operatorId: number,
): User | null {
  const before = getUser(uid);
  if (!before) return null;
  const db = getDb();
  db.prepare("UPDATE users SET level_id = ?, level_locked = ?, updated_at = ? WHERE uid = ?").run(
    levelId,
    locked ? 1 : 0,
    Date.now(),
    uid,
  );
  // 取消锁定且未指定等级时，立即按累计消费自动判定
  if (!locked && levelId == null) autoAssignLevel(db, uid);
  const after = getUser(uid);
  writeAudit({
    operatorId,
    action: "set_level",
    targetType: "user",
    targetId: String(uid),
    before: { levelId: before.levelId, locked: before.levelLocked },
    after: { levelId: after?.levelId, locked: after?.levelLocked },
    remark: "管理员调整会员等级",
  });
  return after;
}

/** 重新计算所有用户的累计消费等级（跳过锁定） */
export function recomputeAllLevels(): number {
  const db = getDb();
  const users = db.prepare("SELECT uid FROM users WHERE level_locked = 0").all() as { uid: number }[];
  db.transaction(() => {
    for (const u of users) autoAssignLevel(db, u.uid);
  })();
  return users.length;
}
