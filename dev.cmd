@echo off
rem ===================================================================
rem  HStore 一键启动开发环境（Windows）
rem  双击运行，或在终端执行：dev.cmd [-w 3000] [-a 3001] [--bin]
rem  实际逻辑在 scripts/dev-all.mjs，此文件只做环境检查与透传。
rem ===================================================================
setlocal
chcp 65001 >nul 2>nul
cd /d "%~dp0"

where node >nul 2>nul
if errorlevel 1 (
  echo.
  echo [ERROR] Node.js not found. Please install Node.js 20+ first.
  echo         https://nodejs.org/
  echo.
  pause
  exit /b 1
)

if not exist "node_modules\vite\bin\vite.js" (
  echo.
  echo [ERROR] Dependencies not installed. Run: npm install
  echo.
  pause
  exit /b 1
)

where go >nul 2>nul
if errorlevel 1 (
  echo.
  echo [ERROR] Go not found. Please install Go 1.23+ first.
  echo         https://go.dev/dl/
  echo.
  pause
  exit /b 1
)

node "%~dp0scripts\dev-all.mjs" %*
set "CODE=%ERRORLEVEL%"

rem 出错或 Ctrl+C 时保留窗口，方便看日志
if not "%CODE%"=="0" pause
exit /b %CODE%
