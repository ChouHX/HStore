import { NextResponse } from "next/server";
import { getCookie, getTianqiSettings, saveCookie } from "@/lib/settings";
import { refreshWafCookieOnce } from "@/lib/waf";

export const dynamic = "force-dynamic";

/**
 * 商品图片代理：
 * - 上游图片为 http:// 且带防盗链/WAF，统一转 https 并由服务端带 Cookie/Referer 拉取；
 * - Cookie 自动获取（无需 .env）。
 */
export async function GET(req: Request) {
  const raw = new URL(req.url).searchParams.get("url");
  if (!raw) return new NextResponse("missing url", { status: 400 });

  let target: URL;
  try {
    target = new URL(raw);
  } catch {
    return new NextResponse("bad url", { status: 400 });
  }

  const { baseUrl } = getTianqiSettings();
  const allowedHost = new URL(baseUrl).hostname;
  if (!(target.hostname === allowedHost || target.hostname.endsWith(`.${allowedHost}`))) {
    return new NextResponse("host not allowed", { status: 403 });
  }
  target.protocol = "https:";

  const headersFor = (cookie: string): Record<string, string> => ({
    Referer: `${baseUrl}/`,
    "User-Agent":
      "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    ...(cookie ? { Cookie: cookie } : {}),
  });

  const fetchImage = (cookie: string) =>
    fetch(target.toString(), { headers: headersFor(cookie), cache: "no-store" });

  try {
    let cookie = getCookie();
    if (!cookie) {
      const r = await refreshWafCookieOnce(baseUrl);
      if (r.ok) {
        cookie = r.cookie;
        saveCookie(cookie);
      }
    }
    let upstream = await fetchImage(cookie);
    let ct = upstream.headers.get("content-type") || "";

    if (!upstream.ok || !ct.startsWith("image/")) {
      const r = await refreshWafCookieOnce(baseUrl);
      if (r.ok) {
        saveCookie(r.cookie);
        upstream = await fetchImage(r.cookie);
        ct = upstream.headers.get("content-type") || "";
      }
    }

    if (!upstream.ok || !upstream.body || !ct.startsWith("image/")) {
      return new NextResponse("upstream blocked", { status: 502 });
    }
    return new NextResponse(upstream.body, {
      status: 200,
      headers: {
        "Content-Type": ct,
        "Cache-Control": "public, max-age=86400, immutable",
      },
    });
  } catch {
    return new NextResponse("fetch failed", { status: 502 });
  }
}
