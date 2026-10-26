#!/usr/bin/env node
/**
 * 一键启动 HStore 开发环境：Go 后端 + Vite 前端
 *
 * 特性：
 *  - 自动选择空闲端口（被占用时自动 +1，不会启动失败）
 *  - 自动注入 FAKA_DB_PATH，保证 Go 与 Next 读写同一个 SQLite 文件
 *  - 自动注入 GO_API_ORIGIN，改端口时 Vite 的 /api 代理同步跟随
 *  - 先等 Go 健康检查通过再起前端，避免首屏 502
 *  - 带颜色前缀的日志（[api] / [web]）
 *  - Ctrl+C / 任一进程退出 → 整组一起停止
 *
 * 用法：
 *   node scripts/dev-all.mjs [选项]
 *   npm run dev            # 等价
 *   dev.cmd / ./dev.sh     # Windows / macOS·Linux 快捷入口
 *
 * 选项：
 *   -w, --web-port <n>  前端端口（默认 3000，也可用 WEB_PORT）
 *   -a, --api-port <n>  后端端口（默认 3001，也可用 API_PORT）
 *       --no-web        只启动 Go 后端
 *       --no-api        只启动 Vite 前端
 *       --bin           使用 backend/bin 下已编译的二进制（默认用 go run）
 *   -h, --help          显示帮助
 */
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import path from "node:path";
import readline from "node:readline";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const BACKEND = path.join(ROOT, "backend");
const DB_PATH = process.env.FAKA_DB_PATH || path.join(ROOT, "data", "faka.db");

const isWin = process.platform === "win32";
const useColor = !process.env.NO_COLOR && (process.stdout.isTTY || !!process.env.FORCE_COLOR);
const C = {
  reset: useColor ? "\x1b[0m" : "",
  dim: useColor ? "\x1b[2m" : "",
  bold: useColor ? "\x1b[1m" : "",
  red: useColor ? "\x1b[31m" : "",
  green: useColor ? "\x1b[32m" : "",
  yellow: useColor ? "\x1b[33m" : "",
  blue: useColor ? "\x1b[34m" : "",
  cyan: useColor ? "\x1b[36m" : "",
};

// ─────────────────────────────── 参数解析 ───────────────────────────────

const opts = {
  web: true,
  api: true,
  bin: false,
  webPort: Number(process.env.WEB_PORT) || 3000,
  apiPort: Number(process.env.API_PORT) || 3001,
};

const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  const a = argv[i];
  const val = () => {
    const v = argv[++i];
    if (!v || v.startsWith("-")) die(`参数 ${a} 缺少值`);
    return v;
  };
  if (a === "-h" || a === "--help") {
    printHelp();
    process.exit(0);
  } else if (a === "-w" || a === "--web-port") {
    opts.webPort = Number(val());
  } else if (a === "-a" || a === "--api-port") {
    opts.apiPort = Number(val());
  } else if (a.startsWith("--web-port=")) {
    opts.webPort = Number(a.slice(11));
  } else if (a.startsWith("--api-port=")) {
    opts.apiPort = Number(a.slice(11));
  } else if (a === "--no-web") {
    opts.web = false;
  } else if (a === "--no-api") {
    opts.api = false;
  } else if (a === "--bin") {
    opts.bin = true;
  } else {
    die(`未知参数: ${a}（用 --help 查看用法）`);
  }
}

for (const [k, v] of [["webPort", opts.webPort], ["apiPort", opts.apiPort]]) {
  if (!Number.isInteger(v) || v < 1 || v > 65535) die(`端口非法: ${k}=${v}`);
}
if (!opts.web && !opts.api) die("--no-web 与 --no-api 不能同时使用");

function printHelp() {
  const lines = [
    "",
    `${C.bold}HStore 开发环境一键启动${C.reset}`,
    "",
    `  ${C.cyan}npm run dev${C.reset}                     前端 + 后端（推荐）`,
    `  ${C.cyan}dev.cmd${C.reset}  /  ${C.cyan}./dev.sh${C.reset}          同上，双击或直接执行`,
    "",
    `${C.bold}选项${C.reset}`,
    `  -w, --web-port <n>   前端端口（默认 3000，环境变量 WEB_PORT）`,
    `  -a, --api-port <n>   后端端口（默认 3001，环境变量 API_PORT）`,
    `      --no-web         只启动 Go 后端`,
    `      --no-api         只启动 Vite 前端`,
    `      --bin            使用 backend/bin 下已编译的二进制（默认 go run）`,
    `  -h, --help           显示本帮助`,
    "",
    `${C.bold}示例${C.reset}`,
    `  npm run dev -- -w 4000 -a 4001     # 前端 4000、后端 4001`,
    `  npm run dev:api                    # 只起后端（npm run dev:api -- --bin 用二进制）`,
    "",
  ];
  process.stdout.write(lines.join("\n"));
}

// ─────────────────────────────── 工具函数 ───────────────────────────────

function log(msg) {
  process.stdout.write(`${C.dim}${pad("[dev]", 5)}${C.reset} ${msg}\n`);
}
function warn(msg) {
  process.stdout.write(`${C.dim}${pad("[dev]", 5)}${C.reset} ${C.yellow}${msg}${C.reset}\n`);
}
function die(msg) {
  process.stderr.write(`${C.dim}${pad("[dev]", 5)}${C.reset} ${C.red}${msg}${C.reset}\n`);
  process.exit(1);
}
function pad(s, n = 3) {
  return s.length >= n ? s : s + " ".repeat(n - s.length);
}

/** 端口是否已被监听 */
function isPortBusy(port) {
  return new Promise((resolve) => {
    const sock = net.connect({ port, host: "127.0.0.1" });
    const done = (busy) => {
      sock.removeAllListeners();
      sock.destroy();
      resolve(busy);
    };
    sock.once("connect", () => done(true));
    sock.once("error", () => done(false));
    sock.setTimeout(700, () => done(false));
  });
}

/** 从 start 开始找一个空闲端口，跳过 exclude 中的端口 */
async function pickPort(start, label, exclude = []) {
  for (let p = start; p < start + 30; p++) {
    if (exclude.includes(p)) continue;
    if (!(await isPortBusy(p))) {
      if (p !== start) warn(`${label}端口 ${start} 被占用，改用 ${p}`);
      return p;
    }
  }
  die(`${label}在 ${start}~${start + 29} 范围内找不到可用端口，请用 --web-port / --api-port 指定`);
}

/**
 * 后端端口被占用时直接报错，而不是换端口。
 * 原因：SQLite 是单文件库，两个 Go 进程同时写容易互相 busy，
 * 静默换端口会让「改了代码但连的是旧服务」这种问题非常难排查。
 */
async function requireFreePort(port) {
  if (!(await isPortBusy(port))) return port;

  let hint = "";
  try {
    const r = await fetch(`http://127.0.0.1:${port}/api/health`, {
      signal: AbortSignal.timeout(1200),
    });
    const j = await r.json().catch(() => ({}));
    if (j?.service === "hstore-backend") hint = "该端口上已经有一个 HStore 后端在运行。";
  } catch {
    /* 不是 HTTP 服务，忽略 */
  }

  die(
    [
      `后端端口 ${port} 已被占用。${hint}`,
      "",
      "  解决办法：",
      `  · 结束占用进程：${isWin ? `netstat -ano | findstr :${port}  →  taskkill /PID <PID> /F` : `lsof -ti:${port} | xargs kill`}`,
      "  · 或者换一个端口：npm run dev -- --api-port 3011",
      "  · 只想调前端：    npm run dev:web",
    ].join("\n"),
  );
}

/** 目录下某扩展名文件的最新修改时间 */
function newestMtime(dir, ext, skip = ["bin", "node_modules", ".git", "vendor"]) {
  let newest = 0;
  const walk = (d) => {
    let entries;
    try {
      entries = fs.readdirSync(d, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) {
        if (!skip.includes(e.name)) walk(p);
      } else if (e.name.endsWith(ext)) {
        try {
          const m = fs.statSync(p).mtimeMs;
          if (m > newest) newest = m;
        } catch {
          /* ignore */
        }
      }
    }
  };
  walk(dir);
  return newest;
}

/** 带前缀地转发子进程输出 */
function prefixStream(stream, tag) {
  if (!stream) return;
  const rl = readline.createInterface({ input: stream, crlfDelay: Infinity });
  rl.on("line", (line) => process.stdout.write(`${tag} ${line}\n`));
}

const children = new Set();
let shuttingDown = false;

function killTree(child) {
  if (!child || child.exitCode !== null || child.signalCode) return;
  if (isWin) {
    // /T 递归整棵进程树（go run 会派生出临时 exe，next 会派生子进程）
    spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], { stdio: "ignore" });
  } else {
    try {
      process.kill(-child.pid, "SIGTERM");
    } catch {
      try {
        child.kill("SIGTERM");
      } catch {
        /* ignore */
      }
    }
  }
}

function shutdown(code = 0) {
  if (shuttingDown) return;
  shuttingDown = true;
  if (children.size) log(`正在停止 ${children.size} 个进程…`);
  for (const c of children) killTree(c);
  // 给 taskkill / SIGTERM 一点时间，避免端口残留
  setTimeout(() => process.exit(code), isWin ? 600 : 300);
}

process.on("SIGINT", () => {
  process.stdout.write("\n");
  log("收到 Ctrl+C，正在停止…");
  shutdown(0);
});
process.on("SIGTERM", () => shutdown(0));
process.on("exit", () => {
  for (const c of children) killTree(c);
});

function launch(name, color, cmd, args, { cwd, env }) {
  const child = spawn(cmd, args, {
    cwd,
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
    // POSIX 下放进独立进程组，方便整组 signal
    detached: !isWin,
    // 不能走 shell：Windows 下 shell:true 会用 cmd.exe 解析整条命令行，
    // 带空格的路径（如 C:\Program Files\nodejs\node.exe）会被空格截断成 C:\Program，
    // 同时触发 DEP0190 警告。libuv 在 Windows 上会按 PATHEXT 自行解析 .exe，无需 shell。
    shell: false,
  });
  children.add(child);
  const tag = `${color}${pad(`[${name}]`, 5)}${C.reset}`;
  prefixStream(child.stdout, tag);
  prefixStream(child.stderr, tag);

  child.on("error", (e) => {
    children.delete(child);
    if (!shuttingDown) {
      warn(`${name} 启动失败: ${e.message}`);
      shutdown(1);
    }
  });
  child.on("exit", (code, signal) => {
    children.delete(child);
    if (!shuttingDown) {
      const why = signal ? `signal=${signal}` : `code=${code}`;
      warn(`${name} 已退出（${why}），停止其余进程`);
      shutdown(code ?? 0);
    }
  });
  return child;
}

/** 轮询 /api/health 直到后端就绪 */
async function waitApi(port, timeoutMs = 120_000) {
  const t0 = Date.now();
  let last = "";
  while (Date.now() - t0 < timeoutMs) {
    if (shuttingDown) return false;
    try {
      const r = await fetch(`http://127.0.0.1:${port}/api/health`, {
        signal: AbortSignal.timeout(2000),
      });
      if (r.ok) return true;
      last = `HTTP ${r.status}`;
    } catch (e) {
      last = e?.name === "TimeoutError" ? "timeout" : String(e?.message ?? e);
    }
    await new Promise((r) => setTimeout(r, 400));
  }
  warn(`等待后端就绪超时（${last}）`);
  return false;
}

// ─────────────────────────────── 主流程 ───────────────────────────────

async function main() {
  process.stdout.write(
    `\n${C.bold}HStore 开发环境${C.reset} ${C.dim}(${isWin ? "Windows" : process.platform})${C.reset}\n`,
  );
  log(`数据库 ${C.bold}${DB_PATH}${C.reset}`);
  fs.mkdirSync(path.dirname(DB_PATH), { recursive: true });

  let apiPort = opts.apiPort;
  let webPort = opts.webPort;

  // ── 后端 ──
  if (opts.api) {
    if (!fs.existsSync(path.join(BACKEND, "cmd", "server"))) {
      die(`找不到 Go 后端源码：${path.join(BACKEND, "cmd", "server")}`);
    }
    apiPort = await requireFreePort(opts.apiPort);

    let cmd;
    let args;
    let mode;
    const binCandidates = [
      path.join(BACKEND, "bin", "hstore-server.exe"),
      path.join(BACKEND, "bin", "hstore-server"),
    ];
    const binPath = binCandidates.find((p) => fs.existsSync(p));
    const wantBin =
      opts.bin && binPath && fs.statSync(binPath).mtimeMs >= newestMtime(BACKEND, ".go");

    if (opts.bin && !wantBin) {
      warn(binPath ? "二进制比源码旧，改用 go run 重新编译" : "未找到已编译二进制，改用 go run");
    }

    if (wantBin) {
      cmd = binPath;
      args = [];
      mode = "binary";
    } else {
      const probe = spawnSync(isWin ? "go.exe" : "go", ["version"], { encoding: "utf8" });
      if (probe.status !== 0) {
        die(
          [
            "未找到 Go 工具链，无法启动后端。",
            "  1) 安装 Go 1.23+：https://go.dev/dl/",
            "  2) 或先执行 npm run build:api 生成二进制，再加 --bin",
            "  3) 只想调前端：npm run dev:web",
          ].join("\n"),
        );
      }
      cmd = isWin ? "go.exe" : "go";
      args = ["run", "./cmd/server"];
      mode = "go run";
    }

    log(`启动后端（${mode}）: ${C.cyan}http://127.0.0.1:${apiPort}${C.reset}`);
    launch("api", C.cyan, cmd, args, {
      cwd: BACKEND,
      env: {
        PORT: String(apiPort),
        FAKA_ADDR: "127.0.0.1",
        FAKA_DB_PATH: DB_PATH,
        GOTOOLCHAIN: process.env.GOTOOLCHAIN || "local",
      },
    });

    const ready = await waitApi(apiPort);
    if (ready) log(`后端就绪 ${C.green}✓${C.reset}`);
    else warn("后端未在超时内就绪，前端仍会启动（首个请求可能 502）");
  }

  // ── 前端 ──
  if (opts.web) {
    const viteBin = path.join(ROOT, "node_modules", "vite", "bin", "vite.js");
    if (!fs.existsSync(viteBin)) {
      die(`找不到 Vite，请先执行 ${C.bold}npm install${C.reset}`);
    }
    webPort = await pickPort(opts.webPort, "前端", opts.api ? [apiPort] : []);

    const env = {
      GO_API_ORIGIN: `http://127.0.0.1:${apiPort}`,
      WEB_PORT: String(webPort),
    };
    // 只起前端时沿用外部传入的 GO_API_ORIGIN
    if (!opts.api) {
      env.GO_API_ORIGIN = process.env.GO_API_ORIGIN || "http://127.0.0.1:3001";
    }

    log(`启动前端: ${C.cyan}http://localhost:${webPort}${C.reset} → 代理 ${env.GO_API_ORIGIN}`);
    launch(
      "web",
      C.green,
      process.execPath,
      [viteBin, "--port", String(webPort), "--strictPort"],
      { cwd: ROOT, env },
    );
  }

  // ── 汇总 ──
  const line = "─".repeat(52);
  process.stdout.write(
    [
      "",
      `${C.dim}${line}${C.reset}`,
      opts.web ? `  前端   ${C.bold}http://localhost:${webPort}${C.reset}` : "",
      opts.api ? `  后端   ${C.bold}http://127.0.0.1:${apiPort}${C.reset}` : "",
      opts.api ? `  健康   http://127.0.0.1:${apiPort}/api/health` : "",
      `  数据库 ${DB_PATH}`,
      `${C.dim}${line}${C.reset}`,
      `${C.dim}Ctrl+C 停止全部${C.reset}`,
      "",
    ]
      .filter(Boolean)
      .join("\n") + "\n",
  );
}

main().catch((e) => {
  die(e?.stack || String(e));
});
