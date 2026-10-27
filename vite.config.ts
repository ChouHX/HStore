import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { vitePluginForArco } from "@arco-plugins/vite-react";
import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";

const ROOT = fileURLToPath(new URL(".", import.meta.url));

/**
 * 前端只负责页面渲染，所有数据来自 Go 后端。
 * - 开发：/api 代理到 GO_API_ORIGIN（默认 http://127.0.0.1:3001）
 * - 生产：`vite build` 产物由 Go 用 go:embed 托管，天然同源
 */
const API_TARGET = (process.env.GO_API_ORIGIN ?? "http://127.0.0.1:3001").replace(/\/$/, "");

export default defineConfig({
  plugins: [react(), tailwindcss(), vitePluginForArco({ style: "css" })],
  resolve: {
    alias: { "@": ROOT },
  },
  server: {
    port: Number(process.env.WEB_PORT) || 3000,
    strictPort: false,
    proxy: {
      "/api": {
        target: API_TARGET,
        changeOrigin: false,
      },
    },
  },
  build: {
    // 产物直接给 Go 的 go:embed 用，部署时只需一个二进制
    outDir: "backend/web/dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 1200,
  },
});
