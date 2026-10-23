import vm from "node:vm";

const UA =
  "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";

export interface WafResult {
  ok: boolean;
  cookie: string;
  message?: string;
}

function parseSetCookie(headers: Headers): Record<string, string> {
  const jar: Record<string, string> = {};
  for (const c of headers.getSetCookie?.() ?? []) {
    const kv = c.split(";")[0];
    const i = kv.indexOf("=");
    if (i > 0) jar[kv.slice(0, i)] = kv.slice(i + 1);
  }
  return jar;
}

/**
 * 自动通过阿里云 ESA/WAF 的 JS 挑战，获取放行 Cookie（无需浏览器）。
 * 思路：拉取挑战页 → 在 Node vm 中执行页面脚本（提供最小 DOM 垫片）→ 捕获 document.cookie。
 */
export async function refreshWafCookie(baseUrl: string): Promise<WafResult> {
  const root = baseUrl.replace(/\/+$/, "");
  let res: Response;
  try {
    res = await fetch(`${root}/`, {
      headers: { "User-Agent": UA, Accept: "text/html,application/xhtml+xml" },
      cache: "no-store",
    });
  } catch {
    return { ok: false, cookie: "", message: "网络错误，无法访问站点" };
  }

  const text = await res.text();
  const jar = parseSetCookie(res.headers);

  // 正常 JSON/无挑战脚本 → 直接使用已下发的 cookie（可能为空表示无需挑战）
  const scripts = [...text.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)]
    .map((m) => m[1])
    .filter(Boolean);

  if (scripts.length === 0 || !/arg1/.test(text)) {
    const cookie = Object.entries(jar)
      .map(([k, v]) => `${k}=${v}`)
      .join("; ");
    return cookie
      ? { ok: true, cookie }
      : { ok: false, cookie: "", message: "站点未返回挑战，也未能获取 Cookie" };
  }

  const document = {
    cookie: "",
    createElement: () => ({ style: {}, setAttribute() {}, getElementsByTagName: () => [] }),
    getElementsByTagName: () => [],
    addEventListener() {},
  };
  const location = {
    href: `${root}/`,
    host: new URL(root).host,
    hostname: new URL(root).hostname,
    protocol: "https:",
    pathname: "/",
    search: "",
    hash: "",
    reload() {},
    replace() {},
    assign() {},
  };
  const storage = {
    getItem: () => null,
    setItem() {},
    removeItem() {},
    clear() {},
  };

  const sandbox: Record<string, unknown> = {
    document,
    location,
    navigator: {
      userAgent: UA,
      platform: "Linux x86_64",
      language: "zh-CN",
      languages: ["zh-CN", "zh"],
      cookieEnabled: true,
    },
    screen: { width: 1920, height: 1080 },
    history: { pushState() {}, replaceState() {} },
    localStorage: storage,
    sessionStorage: storage,
    console: { log() {}, warn() {}, error() {}, info() {} },
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    atob: (s: string) => Buffer.from(s, "base64").toString("binary"),
    btoa: (s: string) => Buffer.from(s, "binary").toString("base64"),
    encodeURIComponent,
    decodeURIComponent,
  };
  sandbox.window = sandbox;
  sandbox.self = sandbox;
  sandbox.globalThis = sandbox;
  sandbox.top = sandbox;
  sandbox.parent = sandbox;
  sandbox.frames = sandbox;

  const ctx = vm.createContext(sandbox);
  for (const s of scripts) {
    try {
      vm.runInContext(s, ctx, { timeout: 5000 });
    } catch {
      // 脚本末尾通常会调用 location.reload()，垫片会抛错，可忽略；Cookie 已写入
    }
  }

  // document.cookie 形如 "acw_sc__v2=xxx;expires=...;path=/"，只取第一段
  const first = document.cookie.split(";")[0];
  const i = first.indexOf("=");
  if (i > 0) jar[first.slice(0, i).trim()] = first.slice(i + 1).trim();

  delete jar.expires;
  delete jar.path;
  delete jar["max-age"];
  delete jar.domain;

  const cookie = Object.entries(jar)
    .filter(([k, v]) => k && v)
    .map(([k, v]) => `${k}=${v}`)
    .join("; ");

  if (!cookie) return { ok: false, cookie: "", message: "挑战脚本未产生 Cookie" };
  return { ok: true, cookie };
}

let refreshing: Promise<WafResult> | null = null;

/** 并发去重的 Cookie 刷新 */
export function refreshWafCookieOnce(baseUrl: string): Promise<WafResult> {
  if (!refreshing) {
    refreshing = refreshWafCookie(baseUrl).finally(() => {
      refreshing = null;
    });
  }
  return refreshing;
}
