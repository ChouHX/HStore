import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import {
  clearCookie,
  getCookie,
  getCookieAt,
  getRedeemPurchaseUrl,
  getTianqiSettings,
  maskSecret,
  saveRedeemPurchaseUrl,
  saveTianqiSettings,
} from "@/lib/settings";
import { writeAudit } from "@/lib/users";

export const dynamic = "force-dynamic";

export async function GET() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const s = getTianqiSettings();
  const cookie = getCookie();
  return NextResponse.json({
    ok: true,
    baseUrl: s.baseUrl,
    appId: s.appId,
    appSecretMasked: maskSecret(s.appSecret),
    hasSecret: Boolean(s.appSecret),
    hasCookie: Boolean(cookie),
    cookieAt: getCookieAt(),
    cookiePreview: cookie ? cookie.replace(/(acw_sc__v2=)[^;]+/, "$1****") : "",
    redeemPurchaseUrl: getRedeemPurchaseUrl(),
  });
}

export async function POST(req: Request) {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const admin = g.user;

  let body: { action?: string; baseUrl?: string; appId?: string; appSecret?: string; redeemPurchaseUrl?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ ok: false, message: "参数错误" }, { status: 400 });
  }

  if (body.action === "clearCookie") {
    clearCookie();
    writeAudit({ operatorId: admin.uid, action: "settings_cookie_clear", remark: "清除 WAF Cookie" });
    return NextResponse.json({ ok: true });
  }

  if (body.action === "saveSite") {
    const url = (body.redeemPurchaseUrl ?? "").trim();
    if (url) {
      try {
        const u = new URL(url);
        if (u.protocol !== "http:" && u.protocol !== "https:") throw new Error("bad protocol");
      } catch {
        return NextResponse.json({ ok: false, message: "链接需为 http/https 地址" }, { status: 400 });
      }
    }
    saveRedeemPurchaseUrl(url);
    writeAudit({
      operatorId: admin.uid,
      action: "settings_site_save",
      after: { redeemPurchaseUrl: url },
      remark: "保存商城设置",
    });
    return NextResponse.json({ ok: true, redeemPurchaseUrl: url });
  }

  const baseUrl = (body.baseUrl ?? "").trim();
  const appId = (body.appId ?? "").trim();
  if (!baseUrl || !appId) {
    return NextResponse.json({ ok: false, message: "基础地址与客户编号不能为空" }, { status: 400 });
  }
  try {
    new URL(baseUrl);
  } catch {
    return NextResponse.json({ ok: false, message: "基础地址格式不正确" }, { status: 400 });
  }

  saveTianqiSettings({ baseUrl, appId, appSecret: body.appSecret });
  writeAudit({
    operatorId: admin.uid,
    action: "settings_save",
    after: { baseUrl, appId, appSecretChanged: Boolean(body.appSecret) },
    remark: "保存开放平台配置",
  });
  return NextResponse.json({ ok: true });
}
