#!/usr/bin/env node
/**
 * 编译 Go 后端为单文件二进制：backend/bin/hstore-server[.exe]
 * 跨平台（Windows 自动加 .exe），无需 cgo。
 *
 * 用法：npm run build:api
 */
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const BACKEND = path.join(ROOT, "backend");
const isWin = process.platform === "win32";
const OUT = path.join(BACKEND, "bin", isWin ? "hstore-server.exe" : "hstore-server");

if (!fs.existsSync(path.join(BACKEND, "cmd", "server"))) {
  console.error(`[build] 找不到 Go 后端源码：${path.join(BACKEND, "cmd", "server")}`);
  process.exit(1);
}

const probe = spawnSync(isWin ? "go.exe" : "go", ["version"], { encoding: "utf8" });
if (probe.status !== 0) {
  console.error("[build] 未找到 Go 工具链，请安装 Go 1.23+：https://go.dev/dl/");
  process.exit(1);
}
console.log(`[build] ${probe.stdout.trim()}`);

fs.mkdirSync(path.dirname(OUT), { recursive: true });

const t0 = Date.now();
const res = spawnSync(
  isWin ? "go.exe" : "go",
  ["build", "-trimpath", "-ldflags=-s -w", "-o", OUT, "./cmd/server"],
  {
    cwd: BACKEND,
    stdio: "inherit",
    env: { ...process.env, GOTOOLCHAIN: process.env.GOTOOLCHAIN || "local", CGO_ENABLED: "0" },
  },
);

if (res.status !== 0) {
  console.error("[build] 编译失败");
  process.exit(res.status ?? 1);
}

const size = (fs.statSync(OUT).size / 1024 / 1024).toFixed(1);
console.log(`[build] 完成 ${OUT}  ${size} MB  ${((Date.now() - t0) / 1000).toFixed(1)}s`);
console.log(`[build] 启动：npm run dev:api -- --bin`);
