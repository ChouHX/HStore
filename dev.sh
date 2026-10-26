#!/usr/bin/env sh
# ===================================================================
#  HStore 一键启动开发环境（macOS / Linux / Windows Git Bash）
#  用法：./dev.sh [-w 3000] [-a 3001] [--bin]
#  实际逻辑在 scripts/dev-all.mjs，此文件只做环境检查与透传。
# ===================================================================
set -eu
cd "$(dirname "$0")"

if ! command -v node >/dev/null 2>&1; then
  echo "[ERROR] 未找到 Node.js，请先安装 Node.js 20+：https://nodejs.org/" >&2
  exit 1
fi

if [ ! -f "node_modules/vite/bin/vite.js" ]; then
  echo "[ERROR] 依赖未安装，请先执行：npm install" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "[ERROR] 未找到 Go，请先安装 Go 1.23+：https://go.dev/dl/" >&2
  exit 1
fi

exec node scripts/dev-all.mjs "$@"
