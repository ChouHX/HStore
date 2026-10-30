/**
 * 本机订单记录：下单成功后把订单号存到 localStorage，
 * 下次进入「订单查询」页可以直接列出这些订单，不用再翻聊天记录。
 */
export interface SavedOrder {
  orderNumber: string;
  contact: string;
  goodsName?: string;
  amount?: number;
  at: number;
}

const KEY = "hstore:orders";
const CONTACT_KEY = "hstore:last-contact";
const MAX = 50;

function read(): SavedOrder[] {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const list = JSON.parse(raw);
    if (!Array.isArray(list)) return [];
    return list.filter((x) => x && typeof x.orderNumber === "string" && x.orderNumber);
  } catch {
    return [];
  }
}

function write(list: SavedOrder[]) {
  try {
    localStorage.setItem(KEY, JSON.stringify(list.slice(0, MAX)));
  } catch {
    /* 隐私模式下写不进去，忽略 */
  }
}

export function loadSavedOrders(): SavedOrder[] {
  return read().sort((a, b) => (b.at || 0) - (a.at || 0));
}

export function loadSavedOrderNumbers(): string[] {
  return loadSavedOrders().map((o) => o.orderNumber);
}

/** 记一笔订单；同一订单号只保留最新一条。 */
export function saveOrder(item: Omit<SavedOrder, "at"> & { at?: number }) {
  if (!item.orderNumber) return;
  const next: SavedOrder = {
    orderNumber: item.orderNumber,
    contact: item.contact || "",
    goodsName: item.goodsName,
    amount: item.amount,
    at: item.at || Date.now(),
  };
  const list = read().filter((o) => o.orderNumber !== next.orderNumber);
  list.unshift(next);
  write(list);
  if (next.contact) saveLastContact(next.contact);
}

export function removeSavedOrder(orderNumber: string) {
  write(read().filter((o) => o.orderNumber !== orderNumber));
}

export function clearSavedOrders() {
  try {
    localStorage.removeItem(KEY);
  } catch {
    /* ignore */
  }
}

export function saveLastContact(contact: string) {
  try {
    if (contact) localStorage.setItem(CONTACT_KEY, contact);
  } catch {
    /* ignore */
  }
}

export function loadLastContact(): string {
  try {
    return localStorage.getItem(CONTACT_KEY) || "";
  } catch {
    return "";
  }
}

// ===== 游客购买凭证 =====
// 每个商品只保留最近一笔游客单的 token，用于「再次点击购买」时直接打开订单页。

const GUEST_PREFIX = "hstore:guest-purchase:";

function guestKey(goodsId: number) {
  return `${GUEST_PREFIX}${goodsId}`;
}

export function saveGuestToken(token: string, goodsId: number) {
  try {
    localStorage.setItem(guestKey(goodsId), JSON.stringify({ token, goodsId, at: Date.now() }));
  } catch {
    /* ignore */
  }
}

export function loadGuestToken(goodsId: number): string {
  try {
    const raw = localStorage.getItem(guestKey(goodsId));
    if (!raw) return "";
    const saved = JSON.parse(raw) as { token?: string; goodsId?: number };
    return saved.goodsId === goodsId && saved.token ? saved.token : "";
  } catch {
    return "";
  }
}

export function clearGuestToken(goodsId: number) {
  try {
    localStorage.removeItem(guestKey(goodsId));
  } catch {
    /* ignore */
  }
}

// ===== 本机游客订单 =====
// 游客单在发货前没有订单号，只能靠 token 找回；下单成功即记录，
// 「订单查询」页据此直接列出本机下过的单，不用再输入凭证。

export interface SavedGuestOrder {
  token: string;
  goodsName?: string;
  amount?: number;
  at: number;
}

const GUEST_ORDERS_KEY = "hstore:guest-orders";
const GUEST_MAX = 50;

function readGuestOrders(): SavedGuestOrder[] {
  try {
    const raw = localStorage.getItem(GUEST_ORDERS_KEY);
    if (!raw) return [];
    const list = JSON.parse(raw);
    if (!Array.isArray(list)) return [];
    return list.filter((x) => x && typeof x.token === "string" && x.token);
  } catch {
    return [];
  }
}

function writeGuestOrders(list: SavedGuestOrder[]) {
  try {
    localStorage.setItem(GUEST_ORDERS_KEY, JSON.stringify(list.slice(0, GUEST_MAX)));
  } catch {
    /* ignore */
  }
}

/** 记一笔游客单；同一 token 只保留最新一条。 */
export function saveGuestOrder(item: { token: string; goodsName?: string; amount?: number; at?: number }) {
  if (!item.token) return;
  const next: SavedGuestOrder = {
    token: item.token,
    goodsName: item.goodsName,
    amount: item.amount,
    at: item.at || Date.now(),
  };
  const list = readGuestOrders().filter((o) => o.token !== next.token);
  list.unshift(next);
  writeGuestOrders(list);
}

/**
 * 读取本机游客单。
 * 同时兼容旧版本的「每个商品一个 token」（hstore:guest-purchase:*），
 * 老用户之前买过的单也能在新版订单查询里出现。
 */
export function loadSavedGuestOrders(): SavedGuestOrder[] {
  const list = readGuestOrders();
  const seen = new Set(list.map((o) => o.token));
  try {
    for (let i = 0; i < localStorage.length; i += 1) {
      const key = localStorage.key(i);
      if (!key || !key.startsWith(GUEST_PREFIX)) continue;
      const raw = localStorage.getItem(key);
      if (!raw) continue;
      const parsed = JSON.parse(raw) as { token?: string; at?: number };
      if (!parsed?.token || seen.has(parsed.token)) continue;
      seen.add(parsed.token);
      list.push({ token: parsed.token, at: parsed.at || 0 });
    }
  } catch {
    /* ignore */
  }
  return list.sort((a, b) => (b.at || 0) - (a.at || 0)).slice(0, GUEST_MAX);
}

export function removeSavedGuestOrder(token: string) {
  writeGuestOrders(readGuestOrders().filter((o) => o.token !== token));
}
