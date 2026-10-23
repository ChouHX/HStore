// 自动获取天启云商的 WAF 放行 Cookie（TIANQI_COOKIE）
// 用法：node scripts/get-cookie.mjs
// 依赖：本机已安装 Chromium / Chrome（可用 CHROME_BIN 指定路径）
import { spawn } from "node:child_process";

const SITE = process.env.TIANQI_BASE_URL || "https://tianqi996.com";
const CHROME = process.env.CHROME_BIN || "chromium";
const PORT = 9200 + Math.floor(Math.random() * 700);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const chrome = spawn(
  CHROME,
  [
    "--headless=new",
    "--disable-gpu",
    "--no-sandbox",
    "--disable-dev-shm-usage",
    `--remote-debugging-port=${PORT}`,
    "--user-data-dir=/tmp/tianqi-cookie-profile",
    "about:blank",
  ],
  { stdio: "ignore" },
);

let ready = false;
for (let i = 0; i < 60; i++) {
  try {
    const r = await fetch(`http://127.0.0.1:${PORT}/json/version`);
    if (r.ok) {
      ready = true;
      break;
    }
  } catch {
    /* retry */
  }
  await sleep(250);
}
if (!ready) {
  console.error("无法启动 Chromium，请安装 chromium 或用 CHROME_BIN 指定浏览器路径。");
  chrome.kill();
  process.exit(1);
}

const list = await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json();
const page = list.find((t) => t.type === "page");
const ws = new WebSocket(page.webSocketDebuggerUrl);
let id = 0;
const pending = new Map();
const send = (method, params = {}) =>
  new Promise((resolve) => {
    const mid = ++id;
    pending.set(mid, resolve);
    ws.send(JSON.stringify({ id: mid, method, params }));
  });
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data);
  if (msg.id && pending.has(msg.id)) {
    pending.get(msg.id)(msg.result);
    pending.delete(msg.id);
  }
};

await new Promise((r) => (ws.onopen = r));
await send("Network.enable");
await send("Page.enable");
await send("Page.navigate", { url: SITE });
await sleep(6000);

const { cookies } = await send("Network.getAllCookies");
const host = new URL(SITE).hostname;
const picked = cookies.filter(
  (c) => c.domain.includes("tianqi") || host.endsWith(c.domain.replace(/^\./, "")),
);
const cookieStr = picked.map((c) => `${c.name}=${c.value}`).join("; ");

ws.close();
chrome.kill();

if (!cookieStr) {
  console.error("未获取到 Cookie，请确认站点可访问后重试。");
  process.exit(1);
}

console.log("\n把下面这一行追加到项目根目录 .env.local（已存在则替换）：\n");
console.log(`TIANQI_COOKIE=${cookieStr}\n`);
console.log("然后重启 npm run dev（或重新 build）使其生效。");
