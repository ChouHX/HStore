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
