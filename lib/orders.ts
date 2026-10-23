import { getDb } from "./db";
import type { Card, LocalOrder, OrderDetail, Platform } from "./types";

interface OrderRow {
  order_number: string;
  outer_number: string;
  goods_id: number;
  goods_name: string;
  count: number;
  money: number;
  status: number;
  result: string;
  platform: Platform;
  created_at: number;
  updated_at: number;
  user_uid: number | null;
  sale_money: number;
  cost_money: number;
  source: "admin" | "user";
  settled: number;
  username?: string;
}

interface CardRow {
  card_id: string;
  order_number: string;
  card_no: string;
  card_pwd: string;
  status: number;
  cancel_request: number;
  result: string;
  created_at: number;
}

function mapCard(r: CardRow): Card {
  return {
    cardId: r.card_id,
    cardNo: r.card_no,
    cardPwd: r.card_pwd,
    status: r.status,
    cancelRequest: r.cancel_request,
    result: r.result,
  };
}

function toLocal(r: OrderRow, cards: Card[]): LocalOrder {
  return {
    orderNumber: r.order_number,
    outerNumber: r.outer_number,
    goodsId: r.goods_id,
    goodsName: r.goods_name,
    count: r.count,
    money: r.money,
    status: r.status,
    result: r.result,
    platform: r.platform,
    createdAt: r.created_at,
    updatedAt: r.updated_at,
    userUid: r.user_uid,
    username: r.username ?? "",
    saleMoney: r.sale_money,
    costMoney: r.cost_money,
    source: r.source,
    settled: r.settled,
    cards,
  };
}

export interface UpsertOrderInput {
  orderNumber: string;
  outerNumber: string;
  goodsId: number;
  goodsName: string;
  count: number;
  money: number;
  status: number;
  result: string;
  platform: Platform;
  userUid: number | null;
  saleMoney: number;
  costMoney: number;
  source: "admin" | "user";
}

export function upsertOrder(o: UpsertOrderInput): void {
  const now = Date.now();
  getDb()
    .prepare(
      `INSERT INTO orders(order_number, outer_number, goods_id, goods_name, count, money, status, result, platform,
         user_uid, sale_money, cost_money, source, settled, created_at, updated_at)
       VALUES(@orderNumber, @outerNumber, @goodsId, @goodsName, @count, @money, @status, @result, @platform,
         @userUid, @saleMoney, @costMoney, @source, 0, @now, @now)
       ON CONFLICT(order_number) DO UPDATE SET
         status = excluded.status, result = excluded.result, money = excluded.money,
         count = excluded.count, updated_at = excluded.updated_at`,
    )
    .run({ ...o, now });
}

export function upsertCards(orderNumber: string, cards: Card[]): void {
  const now = Date.now();
  const stmt = getDb().prepare(
    `INSERT INTO cards(card_id, order_number, card_no, card_pwd, status, cancel_request, result, created_at)
     VALUES(@cardId, @orderNumber, @cardNo, @cardPwd, @status, @cancelRequest, @result, @now)
     ON CONFLICT(card_id) DO UPDATE SET
       card_no = excluded.card_no, card_pwd = excluded.card_pwd, status = excluded.status,
       cancel_request = excluded.cancel_request, result = excluded.result`,
  );
  getDb().transaction((list: Card[]) => {
    for (const c of list) {
      stmt.run({
        cardId: String(c.cardId),
        orderNumber,
        cardNo: c.cardNo ?? "",
        cardPwd: c.cardPwd ?? "",
        status: c.status ?? 0,
        cancelRequest: c.cancelRequest ?? 0,
        result: c.result ?? "",
        now,
      });
    }
  })(cards);
}

const ORDER_SELECT = `SELECT o.*, u.username FROM orders o LEFT JOIN users u ON u.uid = o.user_uid`;

export function getOrder(orderNumber: string): LocalOrder | null {
  const row = getDb().prepare(`${ORDER_SELECT} WHERE o.order_number = ?`).get(orderNumber) as
    | OrderRow
    | undefined;
  if (!row) return null;
  const cards = getDb()
    .prepare("SELECT * FROM cards WHERE order_number = ? ORDER BY created_at ASC")
    .all(orderNumber) as CardRow[];
  return toLocal(row, cards.map(mapCard));
}

export interface OrderFilter {
  uid?: number;
  statuses?: number[];
  keyword?: string;
}

function buildOrderWhere(f: OrderFilter): { where: string; params: Record<string, unknown> } {
  const where: string[] = [];
  const params: Record<string, unknown> = {};
  if (f.uid) {
    where.push("o.user_uid = @uid");
    params.uid = f.uid;
  }
  if (f.statuses?.length) {
    where.push(`o.status IN (${f.statuses.map((n) => Number(n)).join(",")})`);
  }
  if (f.keyword) {
    where.push("(o.order_number LIKE @kw OR o.goods_name LIKE @kw)");
    params.kw = `%${f.keyword}%`;
  }
  return { where: where.length ? "WHERE " + where.join(" AND ") : "", params };
}

export function listOrders(
  f: OrderFilter & { limit?: number; offset?: number } = {},
): LocalOrder[] {
  const { where, params } = buildOrderWhere(f);
  const rows = getDb()
    .prepare(
      `${ORDER_SELECT} ${where} ORDER BY o.created_at DESC LIMIT @limit OFFSET @offset`,
    )
    .all({ ...params, limit: f.limit ?? 20, offset: f.offset ?? 0 }) as OrderRow[];
  if (rows.length === 0) return [];
  const cards = getDb().prepare("SELECT * FROM cards ORDER BY created_at ASC").all() as CardRow[];
  const byOrder = new Map<string, Card[]>();
  for (const c of cards) {
    const arr = byOrder.get(c.order_number) ?? [];
    arr.push(mapCard(c));
    byOrder.set(c.order_number, arr);
  }
  return rows.map((r) => toLocal(r, byOrder.get(r.order_number) ?? []));
}

export function countOrders(f: OrderFilter = {}): number {
  const { where, params } = buildOrderWhere(f);
  const row = getDb()
    .prepare(`SELECT COUNT(*) c FROM orders o ${where}`)
    .get(params) as { c: number };
  return row.c;
}

/** 用远端订单详情更新本地记录（保留本地业务字段） */
export function applyRemoteOrder(detail: OrderDetail): LocalOrder {
  const existing = getOrder(detail.orderNumber);
  upsertOrder({
    orderNumber: detail.orderNumber,
    outerNumber: detail.outerNumber ?? existing?.outerNumber ?? "",
    goodsId: existing?.goodsId ?? 0,
    goodsName: existing?.goodsName ?? "",
    count: detail.count ?? existing?.count ?? 0,
    money: detail.money ?? existing?.money ?? 0,
    status: detail.status ?? existing?.status ?? 1,
    result: detail.result ?? existing?.result ?? "",
    platform: existing?.platform ?? "other",
    userUid: existing?.userUid ?? null,
    saleMoney: existing?.saleMoney ?? 0,
    costMoney: existing?.costMoney ?? 0,
    source: existing?.source ?? "admin",
  });
  if (detail.cards?.length) upsertCards(detail.orderNumber, detail.cards);
  return getOrder(detail.orderNumber)!;
}

export function markSettled(orderNumber: string): void {
  getDb().prepare("UPDATE orders SET settled = 1, updated_at = ? WHERE order_number = ?").run(Date.now(), orderNumber);
}

export function updateCardStatus(cardId: string, status: number, result = ""): void {
  getDb()
    .prepare("UPDATE cards SET status = ?, result = ? WHERE card_id = ?")
    .run(status, result, cardId);
}

export function cardOwner(cardId: string): string | null {
  const row = getDb()
    .prepare("SELECT order_number FROM cards WHERE card_id = ?")
    .get(cardId) as { order_number: string } | undefined;
  return row?.order_number ?? null;
}
