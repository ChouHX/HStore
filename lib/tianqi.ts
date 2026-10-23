import crypto from "node:crypto";
import { getCookie, getTianqiSettings, saveCookie } from "./settings";
import { refreshWafCookieOnce } from "./waf";
import type {
  Card,
  CreateOrderResult,
  GoodsDetail,
  OrderDetail,
  RemoteCategory,
  RemoteProduct,
} from "./types";

const VERSION = "3.0";

export interface TianqiConfig {
  baseUrl: string;
  appId: string;
  appSecret: string;
  cookie: string;
}

export function getConfig(): TianqiConfig {
  const s = getTianqiSettings();
  return { ...s, cookie: getCookie() };
}

export function isConfigured(): boolean {
  const { appId, appSecret, baseUrl } = getConfig();
  return Boolean(appId && appSecret && baseUrl);
}

/**
 * 签名规则 (docs/8391731m0.md):
 *   X-Signature = MD5(X-App-Id + appSecret + X-Version + X-Timestamp + bodyJson)
 */
export function sign(appId: string, appSecret: string, timestamp: string, bodyJson: string): string {
  return crypto
    .createHash("md5")
    .update(appId + appSecret + VERSION + timestamp + bodyJson, "utf8")
    .digest("hex");
}

interface ApiEnvelope<T> {
  code: number;
  msg: string;
  data?: T;
}

export class TianqiError extends Error {
  code: number;
  constructor(code: number, message: string) {
    super(message);
    this.name = "TianqiError";
    this.code = code;
  }
}

function buildHeaders(appId: string, sig: string, ts: string, cookie: string): Record<string, string> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    "X-App-Id": appId,
    "X-Version": VERSION,
    "X-Timestamp": ts,
    "X-Signature": sig,
  };
  if (cookie) headers["Cookie"] = cookie;
  return headers;
}

async function request<T>(path: string, body: unknown): Promise<T> {
  const cfg = getConfig();
  if (!cfg.appId || !cfg.appSecret) {
    throw new TianqiError(-1, "未配置客户编号 / API 密钥，请在「系统设置」中填写");
  }

  const bodyJson = JSON.stringify(body ?? {});
  const timestamp = Math.floor(Date.now() / 1000).toString();
  const signature = sign(cfg.appId, cfg.appSecret, timestamp, bodyJson);
  const url = `${cfg.baseUrl}${path}`;

  const doFetch = (cookie: string) =>
    fetch(url, {
      method: "POST",
      headers: buildHeaders(cfg.appId, signature, timestamp, cookie),
      body: bodyJson,
      cache: "no-store",
    });

  const parse = (text: string): T | null => {
    try {
      const json = JSON.parse(text) as ApiEnvelope<T>;
      if (json.code !== 1000) throw new TianqiError(json.code, json.msg || "接口调用失败");
      return json.data as T;
    } catch (err) {
      if (err instanceof TianqiError) throw err;
      return null;
    }
  };

  let cookie = cfg.cookie;
  // 没有 Cookie 时先自动获取，避免首次必然被挑战
  if (!cookie) {
    const r = await refreshWafCookieOnce(cfg.baseUrl);
    if (r.ok) {
      cookie = r.cookie;
      saveCookie(cookie);
    }
  }

  let res = await doFetch(cookie);
  let text = await res.text();
  let data = parse(text);
  if (data !== null) return data;

  // 返回非 JSON（WAF 人机校验）→ 自动重新获取 Cookie 并重试一次
  const refreshed = await refreshWafCookieOnce(cfg.baseUrl);
  if (refreshed.ok) {
    cookie = refreshed.cookie;
    saveCookie(cookie);
    res = await doFetch(cookie);
    text = await res.text();
    data = parse(text);
    if (data !== null) return data;
  }
  throw new TianqiError(
    res.status,
    `接口返回非 JSON（可能被站点 WAF 拦截）。自动获取 Cookie ${refreshed.ok ? "成功但仍被拦截" : "失败：" + (refreshed.message ?? "")}`,
  );
}

/** 商品分类接口 */
export function getDirs(): Promise<RemoteCategory[]> {
  return request<RemoteCategory[]>("/api/v3/goods/getDirs", {});
}

export interface GoodsListData {
  allCount: number;
  allPage: number;
  items: RemoteProduct[];
}

/** 商品列表接口 */
export function getList(params: {
  page?: number;
  goodsType?: string;
  keyWord?: string;
  skuType?: string;
  showDirId?: string;
  dirId?: string;
  brandId?: string;
}): Promise<GoodsListData> {
  return request<GoodsListData>("/api/v3/goods/getList", {
    page: params.page ?? 1,
    goodsType: params.goodsType ?? "",
    keyWord: params.keyWord ?? "",
    skuType: params.skuType ?? "",
    showDirId: params.showDirId ?? "1",
    dirId: params.dirId ?? "",
    brandId: params.brandId ?? "",
  });
}

/** 账户查询接口 */
export function getAccount(): Promise<{ balance: number }> {
  return request<{ balance: number }>("/api/v3/user/getAccount", {});
}

/** 商品详情接口 */
export function getDetail(goodsId: number): Promise<GoodsDetail> {
  return request<GoodsDetail>("/api/v3/goods/getDetail", { goodsId });
}

/** 商品下单接口 */
export function createOrder(params: {
  goodsId: number;
  count: number;
  notifyUrl?: string;
  outerNumber: string;
  safePrice: number;
  sku?: string;
  attach?: { name: string; value: string }[];
}): Promise<CreateOrderResult> {
  return request<CreateOrderResult>("/api/v3/order/create", {
    goodsId: params.goodsId,
    count: params.count,
    notifyUrl: params.notifyUrl ?? "",
    outerNumber: params.outerNumber,
    safePrice: params.safePrice,
    sku: params.sku ?? "",
    attach: params.attach ?? [],
  });
}

/** 订单详情接口 */
export function getOrderDetail(params: {
  orderNumber?: string;
  outerNumber?: string;
}): Promise<OrderDetail> {
  return request<OrderDetail>("/api/v3/order/getDetail", {
    orderNumber: params.orderNumber ?? "",
    outerNumber: params.outerNumber ?? "",
  });
}

/** 卡券查询接口 */
export function getCardDetail(cardId: string): Promise<Card> {
  return request<Card>("/api/v3/card/getDetail", { cardId });
}

/** 卡券作废接口 */
export function voidCard(cardId: string): Promise<{ status: number; result?: string }> {
  return request<{ status: number; result?: string }>("/api/v3/card/void", { cardId });
}
