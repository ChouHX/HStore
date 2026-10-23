import { getDb, getMeta, setMeta } from "./db";
import { getSubMemberAddress, getSubMemberDeposits, isBybitConfigured } from "./bybit";

export const CRYPTO_CHAINS: Record<string, { code: string; label: string; display: string }> = {
  TRX: { code: "TRX", label: "TRON (TRC20)", display: "TRC20" },
  BSC: { code: "BSC", label: "BSC (BEP20)", display: "BEP20" },
};

export interface CryptoConfig {
  enabled: boolean;
  chain: string;
  chainLabel: string;
  chainDisplay: string;
  rate: number;
  minUsdt: number;
}

export function getCryptoConfig(): CryptoConfig {
  const chain = getMeta("bybit_chain") || "TRX";
  const info = CRYPTO_CHAINS[chain] ?? CRYPTO_CHAINS.TRX;
  return {
    enabled: (getMeta("bybit_enabled") ?? "1") === "1",
    chain: info.code,
    chainLabel: info.label,
    chainDisplay: info.display,
    rate: Number(getMeta("bybit_rate") || "7.2"),
    minUsdt: Number(getMeta("bybit_min_usdt") || "2"),
  };
}

export function saveCryptoConfig(patch: {
  enabled?: boolean;
  chain?: string;
  rate?: number;
  minUsdt?: number;
  apiKey?: string;
  apiSecret?: string;
}): void {
  if (patch.enabled != null) setMeta("bybit_enabled", patch.enabled ? "1" : "0");
  if (patch.chain && CRYPTO_CHAINS[patch.chain]) setMeta("bybit_chain", patch.chain);
  if (patch.rate != null) setMeta("bybit_rate", String(patch.rate));
  if (patch.minUsdt != null) setMeta("bybit_min_usdt", String(patch.minUsdt));
  if (patch.apiKey) setMeta("bybit_api_key", patch.apiKey.trim());
  if (patch.apiSecret) setMeta("bybit_api_secret", patch.apiSecret.trim());
}

// ===== 地址池 =====
interface PoolRowDb {
  id: number;
  sub_uid: string;
  username: string;
  address: string;
  address_trx: string;
  address_bsc: string;
  chain: string;
  status: string;
  current_order_id: number | null;
  reserved_until: number | null;
  updated_at: number;
}

export interface PoolSlot {
  id: number;
  subUid: string;
  username: string;
  address: string;
  addressTrx: string;
  addressBsc: string;
  chain: string;
  status: "available" | "reserved" | "disabled";
  currentOrderId: number | null;
  reservedUntil: number | null;
  updatedAt: number;
}

function mapSlot(r: PoolRowDb): PoolSlot {
  return {
    id: r.id,
    subUid: r.sub_uid,
    username: r.username,
    address: r.address,
    addressTrx: r.address_trx,
    addressBsc: r.address_bsc,
    chain: r.chain,
    status: r.status as PoolSlot["status"],
    currentOrderId: r.current_order_id,
    reservedUntil: r.reserved_until,
    updatedAt: r.updated_at,
  };
}

export function listPool(): PoolSlot[] {
  return (getDb().prepare("SELECT * FROM bybit_pool ORDER BY id ASC").all() as PoolRowDb[]).map(mapSlot);
}

function addressColumn(chain: string): "address_trx" | "address_bsc" {
  return chain === "BSC" ? "address_bsc" : "address_trx";
}

export async function refreshPoolAddresses(): Promise<{ updated: number; failed: number; errors: string[] }> {
  if (!isBybitConfigured()) return { updated: 0, failed: 0, errors: ["未配置 Bybit API"] };
  const db = getDb();
  const rows = db.prepare("SELECT * FROM bybit_pool").all() as PoolRowDb[];
  let updated = 0;
  let failed = 0;
  const errors: string[] = [];

  for (const r of rows) {
    if (r.status === "reserved") continue;
    let trx = r.address_trx;
    let bsc = r.address_bsc;
    let changed = false;
    for (const code of ["TRX", "BSC"] as const) {
      try {
        const a = await getSubMemberAddress(r.sub_uid, "USDT", code);
        if (a?.address) {
          if (code === "TRX") trx = a.address;
          else bsc = a.address;
          changed = true;
        } else {
          failed++;
          errors.push(`${r.sub_uid}/${code}: 未返回地址`);
        }
      } catch (e) {
        failed++;
        errors.push(`${r.sub_uid}/${code}: ${e instanceof Error ? e.message : String(e)}`);
      }
    }
    if (changed) {
      db.prepare(
        "UPDATE bybit_pool SET address_trx=?, address_bsc=?, address=?, chain=?, updated_at=? WHERE id=?",
      ).run(trx, bsc, trx || r.address, trx ? "TRX" : r.chain, Date.now(), r.id);
      updated++;
    }
  }
  return { updated, failed, errors };
}

async function ensurePoolReady(chain: string): Promise<void> {
  const col = addressColumn(chain);
  const row = getDb()
    .prepare(`SELECT COUNT(*) c FROM bybit_pool WHERE status='available' AND ${col} <> ''`)
    .get() as { c: number };
  if (row.c === 0) await refreshPoolAddresses();
}

function syncPoolUidsSetting(): void {
  const uids = (
    getDb().prepare("SELECT sub_uid FROM bybit_pool ORDER BY id ASC").all() as { sub_uid: string }[]
  )
    .map((r) => r.sub_uid)
    .join(",");
  setMeta("bybit_pool_uids", uids);
}

export function addPoolUid(subUid: string): boolean {
  const uid = subUid.trim();
  if (!uid) return false;
  const r = getDb()
    .prepare("INSERT OR IGNORE INTO bybit_pool(sub_uid, chain, status, updated_at) VALUES(?, ?, 'available', ?)")
    .run(uid, getCryptoConfig().chain, Date.now());
  syncPoolUidsSetting();
  return r.changes === 1;
}

export function removePoolSlot(id: number): boolean {
  const r = getDb().prepare("DELETE FROM bybit_pool WHERE id=? AND status<>'reserved'").run(id);
  syncPoolUidsSetting();
  return r.changes === 1;
}

export function setPoolSlotEnabled(id: number, enabled: boolean): boolean {
  const r = getDb()
    .prepare("UPDATE bybit_pool SET status=?, updated_at=? WHERE id=? AND status<>'reserved'")
    .run(enabled ? "available" : "disabled", Date.now(), id);
  return r.changes === 1;
}

// ===== 订单 =====
interface OrderRowDb {
  id: number;
  uid: number;
  pool_id: number;
  sub_uid: string;
  address: string;
  chain: string;
  requested_cny: number;
  requested_usdt: string;
  usdt_amount: string;
  rate: number;
  credit_cny: number;
  status: string;
  txid: string | null;
  received_usdt: string | null;
  credited: number | null;
  expires_at: number;
  created_at: number;
  paid_at: number | null;
}

export interface CryptoOrder {
  id: number;
  uid: number;
  poolId: number;
  subUid: string;
  address: string;
  chain: string;
  chainLabel: string;
  requestedCny: number;
  requestedUsdt: string;
  usdtAmount: string;
  rate: number;
  creditCny: number;
  status: "waiting" | "paid" | "expired" | "cancelled";
  txid: string | null;
  receivedUsdt: string | null;
  credited: number | null;
  expiresAt: number;
  createdAt: number;
  paidAt: number | null;
}

function mapOrder(r: OrderRowDb): CryptoOrder {
  return {
    id: r.id,
    uid: r.uid,
    poolId: r.pool_id,
    subUid: r.sub_uid,
    address: r.address,
    chain: r.chain,
    chainLabel: CRYPTO_CHAINS[r.chain]?.label ?? r.chain,
    requestedCny: r.requested_cny,
    requestedUsdt: r.requested_usdt,
    usdtAmount: r.usdt_amount,
    rate: r.rate,
    creditCny: r.credit_cny,
    status: r.status as CryptoOrder["status"],
    txid: r.txid,
    receivedUsdt: r.received_usdt,
    credited: r.credited,
    expiresAt: r.expires_at,
    createdAt: r.created_at,
    paidAt: r.paid_at,
  };
}

export function getCryptoOrder(id: number): CryptoOrder | null {
  const r = getDb().prepare("SELECT * FROM crypto_orders WHERE id=?").get(id) as OrderRowDb | undefined;
  return r ? mapOrder(r) : null;
}

export function listCryptoOrders(opts: { uid?: number; limit?: number } = {}): CryptoOrder[] {
  const limit = opts.limit ?? 100;
  const rows = (
    opts.uid
      ? getDb().prepare("SELECT * FROM crypto_orders WHERE uid=? ORDER BY id DESC LIMIT ?").all(opts.uid, limit)
      : getDb().prepare("SELECT * FROM crypto_orders ORDER BY id DESC LIMIT ?").all(limit)
  ) as OrderRowDb[];
  return rows.map(mapOrder);
}

export function expireCryptoOrders(): number {
  const db = getDb();
  const now = Date.now();
  const rows = db
    .prepare("SELECT id, pool_id FROM crypto_orders WHERE status='waiting' AND expires_at < ?")
    .all(now) as { id: number; pool_id: number }[];
  if (rows.length === 0) return 0;
  db.transaction(() => {
    for (const r of rows) {
      const u = db.prepare("UPDATE crypto_orders SET status='expired' WHERE id=? AND status='waiting'").run(r.id);
      if (u.changes === 1) {
        db.prepare(
          "UPDATE bybit_pool SET status='available', current_order_id=NULL, reserved_until=NULL WHERE id=? AND current_order_id=?",
        ).run(r.pool_id, r.id);
      }
    }
  })();
  return rows.length;
}

function uniqueExpectedAmount(requested: number): string {
  const db = getDb();
  for (let i = 0; i < 50; i++) {
    const tail = (Math.floor(Math.random() * 99) + 1) / 10000;
    const val = Number((requested + tail).toFixed(4)).toFixed(4);
    const exists = db.prepare("SELECT 1 FROM crypto_orders WHERE status='waiting' AND usdt_amount=?").get(val);
    if (!exists) return val;
  }
  throw new Error("生成唯一支付金额失败，请重试");
}

/** 用户按 CNY 充值，自选链路（TRX / BSC） */
export async function createCryptoOrder(uid: number, cnyInput: number, chainInput?: string): Promise<CryptoOrder> {
  const cfg = getCryptoConfig();
  if (!cfg.enabled) throw new Error("USDT 充值功能未开启");
  if (!isBybitConfigured()) throw new Error("管理员尚未配置 Bybit API");

  const chain = chainInput && CRYPTO_CHAINS[chainInput] ? chainInput : cfg.chain;
  const cny = Number(cnyInput);
  if (!Number.isFinite(cny) || cny <= 0) throw new Error("请输入充值金额");

  const requestedUsdtRaw = cny / cfg.rate;
  if (requestedUsdtRaw < cfg.minUsdt) {
    throw new Error(`最小充值 ${cfg.minUsdt} USDT（约 ¥${(cfg.minUsdt * cfg.rate).toFixed(2)}）`);
  }
  if (requestedUsdtRaw > 100000) throw new Error("单笔充值金额过大");

  expireCryptoOrders();
  await ensurePoolReady(chain);

  const db = getDb();
  const base = Number(requestedUsdtRaw.toFixed(2));
  const expected = uniqueExpectedAmount(base);
  const now = Date.now();
  const expires = now + 30 * 60 * 1000;
  const col = addressColumn(chain);

  const create = db.transaction(() => {
    const slot = db
      .prepare(`SELECT * FROM bybit_pool WHERE status='available' AND ${col} <> '' ORDER BY id ASC LIMIT 1`)
      .get() as PoolRowDb | undefined;
    if (!slot) throw new Error("充值通道繁忙，请稍后重试");
    const address = chain === "BSC" ? slot.address_bsc : slot.address_trx;
    if (!address) throw new Error("该链暂无可用地址，请稍后重试");
    const upd = db
      .prepare("UPDATE bybit_pool SET status='reserved', reserved_until=?, updated_at=? WHERE id=? AND status='available'")
      .run(expires, now, slot.id);
    if (upd.changes !== 1) throw new Error("分配充值地址失败，请重试");
    const info = db
      .prepare(
        `INSERT INTO crypto_orders(uid,pool_id,sub_uid,address,chain,requested_cny,requested_usdt,usdt_amount,rate,credit_cny,status,expires_at,created_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,'waiting',?,?)`,
      )
      .run(
        uid,
        slot.id,
        slot.sub_uid,
        address,
        chain,
        Number(cny.toFixed(2)),
        base.toFixed(2),
        expected,
        cfg.rate,
        Number((Number(expected) * cfg.rate).toFixed(4)),
        expires,
        now,
      );
    const id = Number(info.lastInsertRowid);
    db.prepare("UPDATE bybit_pool SET current_order_id=? WHERE id=?").run(id, slot.id);
    return id;
  });

  const id = create();
  return getCryptoOrder(id)!;
}

function settle(
  order: CryptoOrder,
  opts: { txid: string; received: string; remark?: string },
): CryptoOrder | null {
  const db = getDb();
  const now = Date.now();
  const credited = Number((Number(opts.received) * order.rate).toFixed(4));
  db.transaction(() => {
    const upd = db
      .prepare("UPDATE crypto_orders SET status='paid', txid=?, received_usdt=?, credited=?, paid_at=? WHERE id=? AND status='waiting'")
      .run(opts.txid, opts.received, credited, now, order.id);
    if (upd.changes !== 1) return;

    const u = db.prepare("SELECT balance FROM users WHERE uid=?").get(order.uid) as { balance: number } | undefined;
    if (u) {
      const before = u.balance;
      const after = Number((before + credited).toFixed(4));
      db.prepare("UPDATE users SET balance=?, updated_at=? WHERE uid=?").run(after, now, order.uid);
      db.prepare(
        `INSERT INTO ledger(uid,type,amount,balance_before,balance_after,order_number,code,operator_id,remark,created_at)
         VALUES(?,?,?,?,?,?,?,?,?,?)`,
      ).run(
        order.uid,
        "recharge",
        credited,
        before,
        after,
        `crypto-${order.id}`,
        "",
        null,
        opts.remark ?? `USDT 充值到账（${order.chainLabel}）`,
        now,
      );
    }
    db.prepare(
      "UPDATE bybit_pool SET status='available', current_order_id=NULL, reserved_until=NULL WHERE id=? AND current_order_id=?",
    ).run(order.poolId, order.id);
  })();
  return getCryptoOrder(order.id);
}

export async function checkCryptoOrder(id: number): Promise<CryptoOrder | null> {
  expireCryptoOrders();
  const order = getCryptoOrder(id);
  if (!order || order.status !== "waiting") return order;
  if (!isBybitConfigured()) return order;

  let rows;
  try {
    rows = await getSubMemberDeposits(order.subUid, "USDT", order.createdAt - 10 * 60 * 1000);
  } catch {
    return order;
  }

  const match = rows.find((r) => {
    if (r.status !== 3) return false;
    if (String(r.chain).toUpperCase() !== order.chain.toUpperCase()) return false;
    if (String(r.toAddress) !== order.address) return false;
    if (Math.abs(Number(r.amount) - Number(order.usdtAmount)) > 1e-8) return false;
    const at = Number(r.successAt || 0);
    if (at && at < order.createdAt - 120000) return false;
    return true;
  });
  if (!match) return order;

  const used = getDb().prepare("SELECT id FROM crypto_orders WHERE txid=?").get(match.txID) as
    | { id: number }
    | undefined;
  if (used && used.id !== order.id) return order;
  return settle(order, { txid: match.txID, received: match.amount });
}

/** 用户取消订单：立即释放地址池 */
export function cancelCryptoOrder(id: number, uid?: number): CryptoOrder | null {
  const order = getCryptoOrder(id);
  if (!order) return null;
  if (uid != null && order.uid !== uid) throw new Error("无权操作");
  if (order.status !== "waiting") return order;
  const db = getDb();
  db.transaction(() => {
    const u = db.prepare("UPDATE crypto_orders SET status='cancelled' WHERE id=? AND status='waiting'").run(id);
    if (u.changes === 1) {
      db.prepare(
        "UPDATE bybit_pool SET status='available', current_order_id=NULL, reserved_until=NULL WHERE id=? AND current_order_id=?",
      ).run(order.poolId, order.id);
    }
  })();
  return getCryptoOrder(id);
}

/** 管理员测试：模拟一笔到账（不调用链上） */
export function simulateCryptoOrder(id: number, remark = "模拟到账（测试）"): CryptoOrder | null {
  const order = getCryptoOrder(id);
  if (!order || order.status !== "waiting") return order;
  return settle(order, { txid: `SIM-${id}-${Date.now()}`, received: order.usdtAmount, remark });
}
