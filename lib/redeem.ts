import crypto from "node:crypto";
import { getDb } from "./db";
import { credit } from "./users";
import type { RedeemCode } from "./types";

const CHARSET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";
const FAIL_WINDOW_MS = 60_000;
const FAIL_LIMIT = 5;

function randomCode(): string {
  const bytes = crypto.randomBytes(16);
  let s = "";
  for (let i = 0; i < 16; i++) s += CHARSET[bytes[i] % CHARSET.length];
  return `${s.slice(0, 4)}-${s.slice(4, 8)}-${s.slice(8, 12)}-${s.slice(12, 16)}`;
}

interface CodeRow {
  id: number;
  code: string;
  face_value: number;
  status: "unused" | "used" | "voided";
  used_by: number | null;
  used_at: number | null;
  start_at: number | null;
  end_at: number | null;
  created_at: number;
}

function mapCode(r: CodeRow): RedeemCode {
  return {
    id: r.id,
    code: r.code,
    faceValue: r.face_value,
    status: r.status,
    usedBy: r.used_by,
    usedAt: r.used_at,
    startAt: r.start_at,
    endAt: r.end_at,
    createdAt: r.created_at,
  };
}

export interface CodeFilter {
  status?: string;
  keyword?: string;
  usedBy?: number;
}

function buildWhere(filter: CodeFilter): { where: string; params: Record<string, unknown> } {
  const where: string[] = [];
  const params: Record<string, unknown> = {};
  if (filter.status && filter.status !== "all") {
    where.push("status = @status");
    params.status = filter.status;
  }
  if (filter.keyword) {
    where.push("code LIKE @kw");
    params.kw = `%${filter.keyword.toUpperCase()}%`;
  }
  if (filter.usedBy) {
    where.push("used_by = @usedBy");
    params.usedBy = filter.usedBy;
  }
  return { where: where.length ? "WHERE " + where.join(" AND ") : "", params };
}

export interface CreateCodesInput {
  faceValue: number;
  count: number;
  startAt?: number | null;
  endAt?: number | null;
}

/** 直接生成兑换码（无批次概念） */
export function createCodes(input: CreateCodesInput): { count: number } {
  const db = getDb();
  const now = Date.now();
  const count = Math.max(1, Math.min(input.count, 100_000));
  const insert = db.prepare(
    `INSERT OR IGNORE INTO redeem_codes(batch_id, code, face_value, status, start_at, end_at, created_at)
     VALUES(0, ?, ?, 'unused', ?, ?, ?)`,
  );
  let inserted = 0;
  db.transaction(() => {
    let guard = 0;
    while (inserted < count && guard < count * 5) {
      const r = insert.run(randomCode(), input.faceValue, input.startAt ?? null, input.endAt ?? null, now);
      if (r.changes === 1) inserted++;
      guard++;
    }
  })();
  return { count: inserted };
}

export function listCodes(
  filter: CodeFilter,
  args: { limit?: number; offset?: number } = {},
): RedeemCode[] {
  const { where, params } = buildWhere(filter);
  const rows = getDb()
    .prepare(
      `SELECT id, code, face_value, status, used_by, used_at, start_at, end_at, created_at
       FROM redeem_codes ${where} ORDER BY id DESC LIMIT @limit OFFSET @offset`,
    )
    .all({ ...params, limit: args.limit ?? 20, offset: args.offset ?? 0 }) as CodeRow[];
  return rows.map(mapCode);
}

export function countCodes(filter: CodeFilter): number {
  const { where, params } = buildWhere(filter);
  const row = getDb()
    .prepare(`SELECT COUNT(*) c FROM redeem_codes ${where}`)
    .get(params) as { c: number };
  return row.c;
}

export function exportCsv(filter: CodeFilter): string {
  const { where, params } = buildWhere(filter);
  const rows = getDb()
    .prepare(
      `SELECT code, face_value, status, used_by, used_at, start_at, end_at, created_at
       FROM redeem_codes ${where} ORDER BY id DESC`,
    )
    .all(params) as CodeRow[];
  const fmt = (t: number | null) => (t ? new Date(t).toISOString() : "");
  const header = "code,face_value,status,used_by,used_at,start_at,end_at";
  const lines = rows.map((r) =>
    [r.code, r.face_value, r.status, r.used_by ?? "", fmt(r.used_at), fmt(r.start_at), fmt(r.end_at)].join(","),
  );
  return [header, ...lines].join("\n");
}

export function voidCode(id: number): void {
  getDb().prepare("UPDATE redeem_codes SET status = 'voided' WHERE id = ? AND status = 'unused'").run(id);
}

// ===== 防刷 =====
export function recentFailureCount(uid: number): number {
  const since = Date.now() - FAIL_WINDOW_MS;
  return (
    getDb()
      .prepare("SELECT COUNT(*) c FROM redeem_failures WHERE uid = ? AND created_at > ?")
      .get(uid, since) as { c: number }
  ).c;
}

function recordFailure(uid: number): void {
  getDb().prepare("INSERT INTO redeem_failures(uid, created_at) VALUES(?, ?)").run(uid, Date.now());
}

function clearFailures(uid: number): void {
  getDb().prepare("DELETE FROM redeem_failures WHERE uid = ?").run(uid);
}

export interface RedeemResult {
  ok: boolean;
  message: string;
  amount?: number;
  balance?: number;
}

/** 核销兑换码：事务 + 原子占用，保证单码仅核销一次 */
export function redeem(uid: number, rawCode: string): RedeemResult {
  const code = (rawCode || "").trim().toUpperCase();
  if (!code) return { ok: false, message: "请输入兑换码" };

  if (recentFailureCount(uid) >= FAIL_LIMIT) {
    return { ok: false, message: "失败次数过多，请 1 分钟后再试" };
  }

  const db = getDb();
  const row = db.prepare("SELECT * FROM redeem_codes WHERE code = ?").get(code) as CodeRow | undefined;
  if (!row) {
    recordFailure(uid);
    return { ok: false, message: "兑换码不存在" };
  }
  if (row.status === "voided") {
    recordFailure(uid);
    return { ok: false, message: "兑换码已作废" };
  }
  if (row.status === "used") {
    recordFailure(uid);
    return { ok: false, message: "兑换码已被使用" };
  }

  const now = Date.now();
  if (row.start_at && now < row.start_at) return { ok: false, message: "兑换活动尚未开始" };
  if (row.end_at && now > row.end_at) {
    recordFailure(uid);
    return { ok: false, message: "兑换码已过期" };
  }

  try {
    const result = db.transaction(() => {
      const upd = db
        .prepare("UPDATE redeem_codes SET status = 'used', used_by = ?, used_at = ? WHERE id = ? AND status = 'unused'")
        .run(uid, now, row.id);
      if (upd.changes !== 1) throw new Error("兑换码已被使用");
      return credit(uid, row.face_value, "redeem", {
        code: row.code,
        remark: "兑换码核销",
      });
    })();
    clearFailures(uid);
    return { ok: true, message: "兑换成功", amount: row.face_value, balance: result.after };
  } catch (err) {
    recordFailure(uid);
    return { ok: false, message: err instanceof Error ? err.message : "兑换失败" };
  }
}
