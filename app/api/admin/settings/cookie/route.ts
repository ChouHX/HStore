import { NextResponse } from "next/server";
import { guardAdmin } from "@/lib/admin";
import { getTianqiSettings, saveCookie } from "@/lib/settings";
import { refreshWafCookieOnce } from "@/lib/waf";
import { writeAudit } from "@/lib/users";

export const dynamic = "force-dynamic";

export async function POST() {
  const g = await guardAdmin();
  if (g.res) return g.res;
  const { baseUrl } = getTianqiSettings();

  const r = await refreshWafCookieOnce(baseUrl);
  if (!r.ok) {
    return NextResponse.json({ ok: false, message: r.message ?? "获取 Cookie 失败" }, { status: 500 });
  }
  saveCookie(r.cookie);
  writeAudit({ operatorId: g.user.uid, action: "settings_cookie_refresh", remark: "自动获取 WAF Cookie" });
  return NextResponse.json({
    ok: true,
    cookieAt: Date.now(),
    cookiePreview: r.cookie.replace(/(acw_sc__v2=)[^;]+/, "$1****"),
  });
}
