import { NextResponse } from "next/server";
import { getMeta } from "@/lib/db";
import { checkCryptoOrder, expireCryptoOrders, listCryptoOrders } from "@/lib/crypto";
import { getSessionUser } from "@/lib/session";

export const dynamic = "force-dynamic";

/**
 * 外部定时任务入口（可选）：
 *   curl "https://your-site/api/crypto/cron?token=YOUR_TOKEN"
 * token 取 CRYPTO_CRON_SECRET 或后台设置 crypto_cron_secret；
 * 未配置 token 时仅允许管理员登录访问。
 */
export async function GET(req: Request) {
  const sp = new URL(req.url).searchParams;
  const token = sp.get("token") || req.headers.get("x-cron-token") || "";
  const expected = process.env.CRYPTO_CRON_SECRET || getMeta("crypto_cron_secret") || "";
  if (expected) {
    if (token !== expected) return NextResponse.json({ ok: false, message: "token 错误" }, { status: 401 });
  } else {
    const user = await getSessionUser();
    if (!user || user.role !== "admin") {
      return NextResponse.json({ ok: false, message: "未授权" }, { status: 401 });
    }
  }

  expireCryptoOrders();
  const waiting = listCryptoOrders({ limit: 50 })
    .filter((o) => o.status === "waiting")
    .slice(0, 20);
  for (const o of waiting) await checkCryptoOrder(o.id);
  return NextResponse.json({ ok: true, checked: waiting.length });
}
