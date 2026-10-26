# HStore 项目约定

## 架构（2026-10-25 定稿）

- **前端：Vite 7 + React 19 + React Router 7，纯 SPA，无 SSR。** 不再使用 Next.js。
- **后端：Go 1.23（`backend/`，module `hstore/backend`）**，唯一的数据访问层，`modernc.org/sqlite` 纯 Go 实现。
- **前端不直连数据库**。所有数据走 `/api/*`。`lib/` 只放纯类型与纯函数，禁止出现 Node 端 DB/网络代码。
- **生产部署 = 单二进制**：`vite build` → `backend/web/dist` → `go:embed` → `hstore-server`。
  `npm run build:all` 是标准生产构建命令，**顺序必须是 `vite build && node scripts/build-api.mjs`**
  （Go 在前端之前编译会把上一轮的旧 assets 打进二进制）。
- **零配置文件**：没有 `.env`。业务配置（上游 AppID/密钥、定价、兑换码、Bybit、汇率）全部在
  管理后台写入 SQLite 的 `sync_meta`；会话密钥首次启动随机生成落库；
  WAF Cookie 由 `internal/upstream/waf.go` 用 goja 在进程内执行挑战脚本获得，不依赖浏览器/Node。
  仅 `PORT` / `FAKA_ADDR` / `FAKA_DB_PATH` 是运行时变量。

## 目录约定

- `src/` 放应用骨架：`App.tsx`（路由表）、`pages/`、`layouts/`、`hooks/`、`styles/`。
- `components/` 放业务组件，按 `store/ account/ admin/ layout/` 分域，原地保留、不放进 `src/`。
- `@` 别名指向**项目根**（`vite.config.ts` + `tsconfig.json` 的 paths 一致），所以 `@/components/x`、`@/lib/x`、`@/src/x` 都能用。
- 子页面不需要建 page 文件，直接在 `src/App.tsx` 的路由表里指向 Section 组件。

## 编码注意

- react-router 的 `<Link to="...">`（不是 `href`）；`useNavigate()`（不是 `useRouter().push`）。
- 登录/退出后调用 `invalidateSession()`（`src/hooks/useSession.ts`），否则路由守卫读旧缓存。
- **取件是公开页 `/pickup`，不要放进 `/account`**（那边有登录守卫）。后端 `/api/mailbox/list|detail`
  用 `mailboxActor()` 支持匿名（`uid=0`）；匿名只认兑换码，订单号必须登录；匿名按 IP 限流。
- 邮箱阅读器是 `components/MailboxPanel.tsx`（可内嵌），`MailboxDrawer` 只是它的抽屉壳。
- 订单/卡券表里 `ALTER TABLE` 追加的列可能为 NULL，`Scan` 到非指针类型会失败并让 `GetOrder` 返回 nil，
  查询这类列一律加 `COALESCE`。
- 组件里的 `"use client"` 指令已无意义，新代码不要写。
- Arco Design 与 React 19 的兼容补丁由 `scripts/patch-arco.mjs` 在 `postinstall` 自动执行，不要删。

## 开发

- `npm run dev` 一键起 Go(3001) + Vite(3000)；`dev.cmd` / `dev.sh` 是快捷方式。
- 后端端口被占用时**报错而不是自动换端口**（避免两个 Go 进程写同一个 SQLite）；前端端口占用会自动 +1。
- 删除大量文件/目录前先停掉占用它的 dev 进程，否则 safe-delete 的 trash 会失败。
