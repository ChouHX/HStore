# 发卡商城

**Vite + React 19 + Go + SQLite + Arco Design** 的发卡系统，对接「天启云商 商家客户 API 3.0」，
覆盖商品同步、动态定价、用户资产、支付宝（PerPay）/ USDT 充值、兑换码（面值码 / 商品码）、取件、会员等级与下单闭环。

前端只负责页面渲染，所有业务逻辑都在 Go 后端；`vite build` 的产物通过 `go:embed` 打进二进制，
**生产环境只需部署一个可执行文件**。

> **零配置文件**：项目没有 `.env`。上游 AppID / 密钥、定价规则、兑换码、Bybit、PerPay 等
> 全部在「管理后台」维护（写入 SQLite 的 `sync_meta`）；
> 会话签名密钥首次启动随机生成并落库；WAF 放行 Cookie 由 Go 用 goja 直接执行站点挑战脚本获取，
> **不需要浏览器、不需要 Node、不需要人工填 Cookie**。
> 仅剩的运行时变量只有 `PORT` / `FAKA_ADDR` / `FAKA_DB_PATH`（都有合理默认值）。

## 技术栈

| 层 | 选型 |
| --- | --- |
| 前端 | **Vite 7 + React 19 + React Router 7**（纯 SPA，无 SSR） |
| 后端 | **Go 1.23**（`backend/`，`net/http` + 纯 Go SQLite，无 cgo） |
| UI | **Arco Design**（`@arco-design/web-react`）+ Tailwind CSS 4（`@tailwindcss/vite`） |
| 数据库 | SQLite（`modernc.org/sqlite`，WAL 模式） |
| 部署 | 前端产物 `go:embed` 进二进制，单文件运行 |
| 对接 | 天启云商开放平台 API 3.0（MD5 签名）、Bybit 子账户、PerPay 支付宝（HMAC-SHA256）、IMAP / Graph 邮箱取件 |

## 快速开始

```bash
npm install

npm run dev          # 一键启动：Go 后端(3001) + Vite 前端(3000)
# Windows 也可直接双击 dev.cmd；macOS / Linux / Git Bash 用 ./dev.sh
```

打开 http://localhost:3000 。开发时 Vite 把 `/api/*` 代理到 Go 服务（`GO_API_ORIGIN`，
启动脚本会按实际后端端口自动注入）；生产环境由 Go 同源托管，不存在跨域问题。

### 常用命令

| 命令 | 作用 |
| --- | --- |
| `npm run dev` | 一键启动前后端（推荐） |
| `npm run dev:web` | 只启动 Vite（后端需另起） |
| `npm run dev:api` | 只启动 Go 后端 |
| `npm run dev -- --bin` | 用已编译二进制启动后端（跳过 `go run` 编译） |
| `npm run dev -- -w 4000 -a 4001` | 自定义前端 / 后端端口 |
| `npm run build` | 构建前端 → `backend/web/dist` |
| `npm run build:api` | 编译 `backend/bin/hstore-server[.exe]`（已内嵌前端） |
| `npm run build:all` | 先构建前端，再编译后端（**推荐的生产构建**） |
| `npm run typecheck` | `tsc --noEmit` |

启动脚本行为：自动创建 `data/` 目录、统一注入 `FAKA_DB_PATH`、等后端健康检查通过后再起前端、
`[api] / [web]` 彩色日志前缀、Ctrl+C 整组停止。
后端端口被占用时会明确报错而不是静默换端口（避免两个进程同时写同一个 SQLite）。

### 生产部署

```bash
npm run build:all
./backend/bin/hstore-server        # 或 hstore-server.exe
```

启动后访问 `http://127.0.0.1:3001` 即为完整站点（页面 + API 同源）。
静态资源走 `Cache-Control: immutable`，未命中的路径回退 `index.html`（SPA history 路由）。

| 账号 | 密码 | 角色 | 入口 |
| --- | --- | --- | --- |
| `admin` | `admin123` | 管理员 | `/admin/overview` |
| `demo` | `demo123` | 普通用户 | 商城 `/` + 个人中心 `/account` |

### Docker 一键部署（GHCR）

推送到 `main` 后，GitHub Actions 会执行测试并构建多架构镜像，推送到：

```text
ghcr.io/chouhx/hstore:latest
ghcr.io/chouhx/hstore:main
ghcr.io/chouhx/hstore:sha-<commit>
```

服务器只需 Docker 与 Compose v2：

```bash
# 首次启动
docker compose pull
docker compose up -d

# 后续更新
docker compose pull
docker compose up -d
```

默认映射 `3001:3001`，数据保存在 `hstore-data` 卷（容器内 `/data/faka.db`）。
修改端口：`HSTORE_PORT=8080 docker compose up -d`。
固定版本：`HSTORE_IMAGE=ghcr.io/chouhx/hstore:sha-xxxxxxx docker compose up -d`。

查看日志与健康状态：

```bash
docker compose logs -f app
docker compose ps
```

如果 GHCR 包被设为私有，先登录再拉取：

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u ChouHX --password-stdin
```

## 界面

- **商城 `/`**：简洁顶栏（头像下拉点击展开；移动端新增「公告」按钮）+ **Hero 区（轮播图；公告卡片桌面显示，移动端收进公告抽屉）** + **板块以无边框列表展示**（图片/名称/件数，名称自动去掉括号说明、不截断，点击切换）+ **商品分类（桌面为左侧边栏、移动端为抽屉，抽屉标题带板块名，均带分类图片）** + **Header 全局搜索（跨板块）**/排序 + 商品网格（**商品按需加载（接口分页）、有货优先、默认按名称排序、价格去除多余 0**）；
  右上角图标可切换 **列表 / 一列两个 / 一列四个** 布局；卡片展示**库存**，缺货置灰；窄屏子分类转为下拉。
- **个人中心 `/account`**：Arco `Layout` 侧边栏 + Header + 内容区，含概览、充值/兑换、我的订单、余额明细。
- **取件 `/pickup`（公开页，无需登录）**：独立于个人中心的全屏页面，输入兑换码（未登录）或兑换码/订单号（已登录）
  直接打开邮箱；登录后下方还会自动列出**我的所有可取出邮箱**。商城顶栏常驻「取件」入口（未登录也显示）。
- **管理后台 `/admin`**：侧边栏 + Header + 内容区；默认「数据概览」（KPI + 平台分布 + 最近操作）。移动端侧边栏收起为**抽屉**，由 Header 左侧按钮展开，仅保留一个 Header。
- 列表统一使用 Arco `Table`（服务端分页）、`Input.Search`/`Select` 筛选、`Modal` 弹窗、`Popconfirm` 删除确认、`Message` 提示。
- **订单**：列表分页展示，点击「查看详情」打开抽屉查看卡券与状态；详情支持**下载 TXT（一行一个卡券，卡号----密码）**，避免数据过多无法展示。

## 功能模块

### 1. 退款模块已下线
- 无前台退款入口，订单状态不含「退款中/已退款」；
- 仅保留内部补偿 `refundConsume`（上游下单失败自动退回已扣余额）。

### 2. 商品同步与定价
- **分类上架**：后台「分类上架」以树形展示全部分类与子分类，选中分类即可拉取上游商品，勾选后一键上架（**只负责选择与上架，不在此编辑**）；上架后自动补全库存，编辑请到「商品与上下架」；
- **商品自定义**：也可按商品编号单独设置自定义标题 / 描述 / 图片，覆盖商城展示，不影响上游下单；
- 一键同步上游分类与商品；库存由详情接口低频并发补全并入库；
- 商品列表新增**板块列**（并保留分类列）；**板块 / 分类 / 状态筛选图标放在表头 th 中**（Excel 式 Popover 多选：全选/半选、清空、已选/总数，选项只显示已有值）；支持**单条/批量上下架**、**本地编辑**（自定义标题/描述/图片，可清除恢复上游）、锁价与**删除本地商品**，操作写入审计日志；
- 定价引擎：`全站 / 平台 / 分类 / 单品`，公式 `成本 ± X`、`成本 × (1+Y%)`、`成本 × 倍率 ± 固定`；舍入支持 4 位 / 3 位 / 2 位四舍五入、向上、向下、取整到元；优先级 `单品 > 分类 > 平台 > 全站`；
  （小额成本默认 4 位小数，避免 0.031 × 1.1 被舍成 0.03 低于成本）
- 单品手动锁价，重算时保留。

### 3. 用户与资产
- 用户列表按 UID/手机/邮箱/昵称/状态/等级筛选，服务端分页；
- 编辑资料、冻结/解冻；
- 管理员增减余额**强制备注**，记录操作人/前后金额到审计日志；
- **不可篡改账本**：`ledger` 表触发器禁止 `UPDATE/DELETE`。

### 4. 兑换码（无批次）
- **两种码**：面值码（核销后充入余额）、商品码（关联某个商品 + 数量，核销后自动向上游下单）；
- 后台「兑换码」页可切换类型创建，商品码支持搜索商品、指定数量、批量生成；
- 表格展示**全部兑换码**，支持按状态、兑换码关键字筛选与分页；
- 导出 CSV、单码作废；
- 核销并发安全：`unused → redeeming → used` 状态机 + `redeeming_at` 锁（3 分钟未完成自动回收），
  面值码入账为单事务原子操作；
- 商品码核销复用 `PlaceOrder(payment:"code")`：不扣余额、走上游下单、订单号回写兑换码，
  订单与卡券直接复用 `orders` / `cards`，**不额外存储任何账号数据**；
- 防刷：1 分钟失败 5 次临时锁定。

### 5. 取件（读邮箱）
- **取件页 `/pickup` 是公开页面，不需要登录**（参考 Redeem 的取件页形态：卡密即凭证）。
  未登录：输入兑换码 `XXXX-XXXX-XXXX-XXXX` 直接取件；已登录：额外支持订单号，
  并自动列出自己所有可取出邮箱（`GET /api/mailbox/cards`）；
- 取件结果直接内嵌在页面里阅读（`MailboxPanel`），订单详情里仍用抽屉（`MailboxDrawer` 是它的薄壳）；
- **兑换码是凭证本身，不绑定用户**：后台批量生成的兑换码不记录归属，谁持有谁就能取件，
  登录与否、是不是当初核销的那个人都不影响（与 Redeem 的卡密模型一致）；
  输入时会自动规整（大小写、有无 `-` 都认）；
- **订单号仍然校验归属**：订单号来自上游、可被枚举，必须登录且是订单所有者；
  匿名使用订单号会被拒。匿名调用另按 IP 限流（1 分钟 12 次，超出 429）；
- 订单详情里含 `@` 的卡券同样有「取件」按钮，兑换商品码成功后也会直接给出取件入口；
- 卡券若是「邮箱----密码[----refresh_token]」格式，即可打开邮箱，
  浏览收件箱 / 垃圾箱并阅读邮件正文（正文在沙箱 iframe 中渲染）；
- 后端 `backend/internal/mail` 从同级 `Redeem` 项目移植：有 `refresh_token` 走 OAuth（XOAUTH2 + 自动刷新），
  只有邮箱密码时用 IMAP `LOGIN`；Outlook 走 IMAP，微软 Graph 作为备选通道；
- **邮件服务参数全部内置**（与 Redeem 一致），后台无需任何配置：ClientID、Token 地址、IMAP 服务器
  （`outlook.office365.com:993`）、收件箱 / 垃圾箱目录名都写在 `internal/service/mailbox.go`。
  接入非 Outlook 租户时用环境变量覆盖即可：`IMAP_SERVER` / `IMAP_PORT` / `MAIL_CLIENT_ID` / `MAIL_TOKEN_URL`。

### 6. 加密充值（USDT / Bybit 子账户地址池）
- 管理后台「加密充值」页：Bybit API 配置、汇率/最小金额/默认链、子账户地址池、充值订单；
- 用户按 **CNY** 输入金额，并**自选链路（TRC20 / BEP20）**；最小 **2 USDT（约 ¥14.4）**，订单 **30 分钟**有效；汇率默认 `1 USDT = 7.2 CNY`（可改）；
- **地址池**：多个 Bybit 子账户作为并发槽位（当前 2 个），每个子账户同时维护 **TRC20 + BEP20** 两个地址；下单时按所选链事务内原子占用，完成后自动释放；
- **唯一精确金额**：在申请金额上加 0.0001–0.0099 的随机尾数，必须精确转账；匹配时校验 链 + 地址 + 金额 + txID；
- **自动到账**：Go 服务内置每 30 秒轮询 Bybit 子账户充值记录（`cmd/server` 的 `cryptoLoop`），
  也支持外部 cron `/api/crypto/cron`；前端不再包含任何轮询逻辑，避免重复结算；
- 后台可手动「检查订单」或「模拟到账（测试）」；订单过期自动释放地址并标记 expired；
- 入账复用不可篡改账本（`ledger`，类型 recharge，单号 `crypto-<id>`）。

### 7. 支付宝充值（PerPay）
- 管理后台「支付宝支付」页维护 PerPay 地址、客户端 ID、API 密钥、通知密钥、本商城公开地址、金额区间与商品名；
- 用户按 **CNY** 下单后跳转 PerPay 托管收银台扫码付款；收银台返回 `?perpay=商户订单号` 时自动查单；
- 后端按 PerPay 规范做 **HMAC-SHA256 请求签名**（8 行签名原文、Unix 秒 + 32 字节 base64url nonce）；
- 支付确认有两条路径：内置每 20 秒查单（`perPayLoop`），以及 `/api/webhooks/perpay` 签名回调；
- 回调按原始请求体 **HMAC 验签**（Unix 毫秒时间戳、5 分钟偏差、事件 ID 去重），返回 PerPay 固定 ACK；
- 只有服务端查单或验签通知为 `CONFIRMED` 才入账，且账本只写一次；已入账订单不会被乱序的 UNPAID 快照回退；
- 支持关闭未支付收银台、管理员查单/重试/模拟到账，回调记录与订单状态可在后台查看；
- 入账复用不可篡改账本（`ledger`，类型 recharge，单号 `perpay-<id>`）。

**PerPay 侧准备：** 部署 PerPay → 创建业务网站 → 生成 API 密钥；如需通知，生成通知密钥、开启通知，并把
`notification_origin` 与 `return_origin` 都配置为本商城公开来源（例如 `https://shop.example.com`）。
本项目的通知地址为 `https://shop.example.com/api/webhooks/perpay`。未启用通知也能靠轮询确认。

### 8. 会员等级
- 后台自定义等级（名称/门槛/折扣率）已并入「用户与资产」页面，通过「会员等级」按钮在弹窗中管理（新增/编辑/删除）；
- 自动升级以「累计实际已消费」为准，订单结算完成才累加；
- 人工指定 + 锁定等级，锁定后不再自动变更。

### 9. 上游对接与 WAF（纯 Go）
- 上游域名前挂了阿里云 ESA/WAF，未带 Cookie 时返回一段 JS 挑战页。`internal/upstream/waf.go`
  用 **goja** 在进程内执行挑战脚本（垫了 `document` / `location` / `navigator` / `storage` /
  `atob` / `setTimeout` 等最小 DOM 环境），直接从脚本写回的 `document.cookie` 里取出放行 Cookie；
- 触发时机全自动：Cookie 为空时先取一次；请求返回非 JSON（被判为挑战）时再取一次并重放；
  取到后写回 `sync_meta`，后台「系统设置」可看到时间与预览，也可点「刷新 Cookie」手动触发；
- 并发去重（`RefreshWafCookieOnce`）：同时有多个请求需要刷新时只跑一次脚本，其余等待同一个结果；
- 整条链路（签名 → Cookie → 解析 → 下单）都在 Go 里，**无需浏览器 / Node / 人工粘贴 Cookie**，
  实测刷新一次约 1 秒。

## 目录结构

```
index.html                       Vite 入口
vite.config.ts                   别名 @、/api 代理、产物输出到 backend/web/dist
src/
  main.tsx                       挂载 React（Arco CSS + Tailwind）
  App.tsx                        路由表（商城 / 登录 / 取件 / 个人中心 / 管理后台）
  layouts/AccountLayout.tsx      个人中心布局 + 登录守卫
  layouts/AdminLayout.tsx        管理后台布局 + 管理员守卫
  pages/HomePage.tsx             商城（拉 /api/store/home）
  pages/LoginPage.tsx            登录（Arco Form）
  pages/PickupPage.tsx           取件（公开页，无需登录）
  hooks/useSession.ts            /api/auth/me 缓存 + 失效
  components/PageLoading.tsx     全屏加载占位
  styles/globals.css             Tailwind + 全局样式
components/
  layout/DashboardShell.tsx      Arco Layout 侧边栏 + Header + 内容
  store/                         StoreHeader / HeroSection / PopupAnnouncement
  admin/*Section.tsx             后台模块（Arco Table + Modal + Message）
  account/*Section.tsx           个人中心模块
  MailboxPanel.tsx               邮箱阅读器（列表 + 正文沙箱 iframe）
  MailboxDrawer.tsx              MailboxPanel 的抽屉壳（订单详情用）
  ProductBrowser.tsx PurchaseModal.tsx OrdersPanel.tsx
lib/
  api-types.ts                   与 Go JSON 对齐的展示类型（纯类型）
  types.ts format.ts status.ts platform.ts site.ts pagination.ts

backend/                         Go 后端（模块 hstore/backend）
  cmd/server/main.go             入口：PORT / FAKA_ADDR / FAKA_DB_PATH，内置加密轮询 + 静态托管
  internal/db/                   建表、迁移、seed、scrypt、会话签名
  internal/store/                products / pricing / users / orders / redeem / crypto / perpay
  internal/upstream/             天启客户端、goja 执行 WAF 挑战、Bybit
  internal/perpay/               PerPay HMAC 客户端、订单接口、回调验签
  internal/service/              共用下单、兑换、取件、PerPay 充值、演示数据
  internal/httpapi/              全部 API 路由与处理器
  internal/mail/                 IMAP / Graph 取件引擎（移植自 Redeem 项目）
  web/embed.go                   go:embed 前端产物
  web/dist/                      vite build 输出（assets 已 gitignore）
  bin/                           编译产物（已 gitignore）

Dockerfile                       多阶段构建（Node 构建前端 + Go 编译 + Alpine 运行）
docker-compose.yml               直接拉取 GHCR 镜像的一键启动配置
.github/workflows/ci.yml         Web / Go 校验 + 多架构镜像构建与推送
scripts/
  dev-all.mjs                    一键启动前后端（dev.cmd / dev.sh 的入口）
  build-api.mjs                  跨平台编译 Go 后端
  patch-arco.mjs                 React 19 兼容补丁

dev.cmd / dev.sh                 Windows / macOS·Linux 一键启动快捷方式
```

## React 19 兼容

Arco Design 与 React 19 存在两处不兼容，项目通过 `scripts/patch-arco.mjs` 在 `postinstall` 时自动修补（幂等）：

1. 读取 `element.ref` 报错 → 改为读取 `props.ref`（`callbackOriginRef` / `Trigger` / `AutoComplete`）；
2. `CopyReactDOM.render is not a function` → React 19 的 `react-dom` 不再导出 `createRoot`，补丁改为从 `react-dom/client` 获取。

## 数据表

`categories` `products` `orders` `cards` `users` `ledger` `audit_logs`
`pricing_rules` `levels` `redeem_codes` `redeem_failures` `sync_meta`
`bybit_pool` `crypto_orders` `perpay_orders` `perpay_webhook_events` `announcements`

## 已验证

- 商城：40 张商品卡展示库存、缺货置灰，无质保标签；
- 后台：Arco Table 分页、筛选、批量上下架、锁价、定价规则弹窗、等级弹窗；
- 兑换码：直接生成、全部券码列表 + 状态/关键字筛选、导出、单码作废；
- 核销：成功 +¥5，重复核销被拒，按码筛选可定位核销用户；
- 个人中心：概览、充值/兑换、订单、余额明细分页均正常；
- 订单：列表分页 + 查看详情抽屉（卡券、状态、刷新、下载 TXT）正常；
- 购买弹窗：展示上游「商品详情 / 购买须知」，默认展开、区域可滚动；
- 加密充值：Bybit 只读 API 验证通过；2 个子账户 **TRC20 + BEP20** 地址均获取成功；
- 地址池：2 笔并发下单成功、第 3 笔返回“通道繁忙”；模拟到账后自动释放槽位；
- 唯一金额：`2.5062` / `3.0028` / `4.0089`，无重复；过期订单自动释放并标记 expired；
- 支付宝（PerPay）：请求签名通过、查单确认入账、回调验签与重复事件 ACK、未支付关闭均验证；
- CNY 下单：¥20(TRC20) → 应付 2.7852 USDT；¥30(BEP20) → 应付 4.1741 USDT；到账按汇率折算；
- 链路自选：用户页选择 BEP20 后展示 `0x...` 地址与 BSC (BEP20) 网络；后台地址池同时显示 TRC20/BEP20 地址；
- 入账：账本记录 `crypto-1/2/3`，余额按 `USDT × 汇率` 正确增加；
- 用户页：地址、精确金额、二维码、倒计时正常；后台配置/地址池/订单页正常；
- 商城搜索：Header 全局搜索（跨板块），搜索时隐藏板块与分类；移动端搜索图标打开弹窗；
- 订单表格：去掉强制最小宽度，宽屏不再出现多余横向滚动条；窄屏自适应压缩，无横向溢出；
- 后台商品：表头筛选生效——板块（4 项）/分类（20 项）/状态（仅显示已有值）均为多选 Popover，含全选/清空；新增板块列；分类筛选按子分类过滤（如 dir 500 → 1 件），板块+状态组合筛选正常；
- 分类：空分类自动隐藏（Google 板块 8 个子分类 → 显示 7 个 + 全部商品）；板块/分类均为无边框列表，名称不截断；
- 子分类：左侧边栏点击可筛选（40 → 2 条），移动端为下拉；
- 布局切换：4 列 → 列表(1) → 2 列 → 4 列，图标按钮生效；
- 定价：小额成本 0.031 → 0.0341（4 位舍入）；指定平台规则仅影响该平台商品；
- 移动端：侧边栏为抽屉、仅一个 Header，菜单点击跳转后自动关闭；
- 商城头像下拉为点击展开，含订单/充值/个人中心等入口；
- 商品自定义：按编号设置标题/描述后商城即时生效；
- Hero：轮播 3 屏 + 公告 4 条正常展示；
- React 19：控制台无 `element.ref` 报错。
