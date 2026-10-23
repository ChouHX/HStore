import { getMeta, setMeta } from "./db";

export const SK = {
  baseUrl: "tianqi_base_url",
  appId: "tianqi_app_id",
  appSecret: "tianqi_app_secret",
  cookie: "tianqi_cookie",
  cookieAt: "tianqi_cookie_at",
  redeemPurchaseUrl: "redeem_purchase_url",
} as const;

export interface TianqiSettings {
  baseUrl: string;
  appId: string;
  appSecret: string;
}

/** 配置以数据库为准，环境变量仅作为兜底（首次会写入数据库） */
export function getTianqiSettings(): TianqiSettings {
  return {
    baseUrl: getMeta(SK.baseUrl) || process.env.TIANQI_BASE_URL || "https://tianqi996.com",
    appId: getMeta(SK.appId) || process.env.TIANQI_APP_ID || "",
    appSecret: getMeta(SK.appSecret) || process.env.TIANQI_APP_SECRET || "",
  };
}

export function saveTianqiSettings(patch: {
  baseUrl?: string;
  appId?: string;
  appSecret?: string;
}): void {
  if (patch.baseUrl != null) setMeta(SK.baseUrl, patch.baseUrl.replace(/\/+$/, ""));
  if (patch.appId != null) setMeta(SK.appId, patch.appId.trim());
  if (patch.appSecret) setMeta(SK.appSecret, patch.appSecret.trim());
}

export function getCookie(): string {
  return getMeta(SK.cookie) || "";
}

export function getCookieAt(): number {
  return Number(getMeta(SK.cookieAt) || 0);
}

export function saveCookie(cookie: string): void {
  setMeta(SK.cookie, cookie);
  setMeta(SK.cookieAt, String(Date.now()));
}

export function clearCookie(): void {
  setMeta(SK.cookie, "");
  setMeta(SK.cookieAt, "0");
}

export function getRedeemPurchaseUrl(): string {
  return getMeta(SK.redeemPurchaseUrl) || "";
}

export function saveRedeemPurchaseUrl(url: string): void {
  setMeta(SK.redeemPurchaseUrl, url.trim());
}

/** 脱敏展示密钥 */
export function maskSecret(s: string): string {
  if (!s) return "";
  if (s.length <= 8) return "****";
  return `${s.slice(0, 4)}****${s.slice(-4)}`;
}
