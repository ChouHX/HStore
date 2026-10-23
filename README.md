# 发卡商城

基于 **Next.js (App Router) + better-sqlite3 + Arco Design** 的发卡系统，对接「天启云商 商家客户 API 3.0」，
覆盖商品同步、动态定价、用户资产、兑换码、会员等级与下单闭环。

## 技术栈

| 层 | 选型 |
| --- | --- |
| 框架 | Next.js 16 (App Router, Turbopack) |
| UI | **Arco Design**（`@arco-design/web-react`）+ Tailwind CSS（仅布局工具类） |
| 数据库 | SQLite（better-sqlite3） |
| 对接 | 天启云商开放平台 API 3.0（MD5 签名） |

## 快速开始

```bash
npm install
npm run dev      # http://localhost:3000
```

| 账号 | 密码 | 角色 | 入口 |
| --- | --- | --- | --- |
| `admin` | `admin123` | 管理员 | `/admin/overview` |
| `demo` | `demo123` | 普通用户 | 商城 `/` + 个人中心 `/account` |

## 界面

- **商城 `/`**：简洁顶栏（头像下拉点击展开；移动端新增「公告」按钮）+ **Hero 区（轮播图；公告卡片桌面显示，移动端收进公告抽屉）** + **板块以无边框列表展示**（图片/名称/件数，名称自动去掉括号说明、不截断，点击切换）+ **商品分类（桌面为左侧边栏、移动端为抽屉，抽屉标题带板块名，均带分类图片）** + **Header 全局搜索（跨板块）**/排序 + 商品网格（**商品按需加载（接口分页）、有货优先、默认按名称排序、价格去除多余 0**）；
  右上角图标可切换 **列表 / 一列两个 / 一列四个** 布局；卡片展示**库存**，缺货置灰；窄屏子分类转为下拉。
- **个人中心 `/account`**：Arco `Layout` 侧边栏 + Header + 内容区，含概览、充值/兑换、我的订单、余额明细。
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
- 直接生成兑换码：填写面值、数量、有效期（空=永久）即可生成；
- 表格展示**全部兑换码**，支持按状态、兑换码关键字筛选与分页；
- 导出 CSV、单码作废；
- 核销并发安全：事务 + `UPDATE ... WHERE status='unused'` 原子占用；
- 防刷：1 分钟失败 5 次临时锁定。

### 6. 加密充值（USDT / Bybit 子账户地址池）
- 管理后台「加密充值」页：Bybit API 配置、汇率/最小金额/默认链、子账户地址池、充值订单；
- 用户按 **CNY** 输入金额，并**自选链路（TRC20 / BEP20）**；最小 **2 USDT（约 ¥14.4）**，订单 **30 分钟**有效；汇率默认 `1 USDT = 7.2 CNY`（可改）；
- **地址池**：多个 Bybit 子账户作为并发槽位（当前 2 个），每个子账户同时维护 **TRC20 + BEP20** 两个地址；下单时按所选链事务内原子占用，完成后自动释放；
- **唯一精确金额**：在申请金额上加 0.0001–0.0099 的随机尾数，必须精确转账；匹配时校验 链 + 地址 + 金额 + txID；
- **自动到账**：服务端每 30 秒轮询 Bybit 子账户充值记录（`instrumentation.ts` + `lib/crypto-scheduler.ts`），也支持外部 cron `/api/crypto/cron`；
- 后台可手动「检查订单」或「模拟到账（测试）」；订单过期自动释放地址并标记 expired；
- 入账复用不可篡改账本（`ledger`，类型 recharge，单号 `crypto-<id>`）。

### 5. 会员等级
- 后台自定义等级（名称/门槛/折扣率）已并入「用户与资产」页面，通过「会员等级」按钮在弹窗中管理（新增/编辑/删除）；
- 自动升级以「累计实际已消费」为准，订单结算完成才累加；
- 人工指定 + 锁定等级，锁定后不再自动变更。

## 目录结构

```
app/
  page.tsx                      商城
  login/page.tsx                登录（Arco Form）
  account/                      个人中心（layout + overview/recharge/orders/ledger）
  admin/crypto/                 加密充值（Bybit 配置/地址池/订单）
  api/crypto/*                  用户充值下单/查询、cron
  api/admin/crypto              后台地址池与订单管理
  instrumentation.ts            服务启动时启动充值轮询
  admin/                        管理后台（layout + overview/products/pricing/users/redeem/levels/logs）
  api/
    auth/{login,logout,me}
    me, me/ledger
    redeem, order/*, card/void, goods/*, image, sync, account
    admin/{stats,products,pricing,users,levels,redeem,ledger,audit}
components/
  layout/DashboardShell.tsx     Arco Layout 侧边栏 + Header + 内容
  admin/*Section.tsx            后台模块（Arco Table + Modal + Message）
  admin/LevelsPanel.tsx         会员等级管理（嵌入用户页弹窗）
  account/*Section.tsx          个人中心模块
  ProductBrowser.tsx PurchaseModal.tsx OrdersPanel.tsx
lib/
  db.ts session.ts admin.ts pagination.ts
  tianqi.ts sync.ts pricing.ts queries.ts
  users.ts redeem.ts orders.ts status.ts platform.ts types.ts
  bybit.ts crypto.ts crypto-scheduler.ts
```

## React 19 兼容

Arco Design 与 React 19 存在两处不兼容，项目通过 `scripts/patch-arco.mjs` 在 `postinstall` 时自动修补（幂等）：

1. 读取 `element.ref` 报错 → 改为读取 `props.ref`（`callbackOriginRef` / `Trigger` / `AutoComplete`）；
2. `CopyReactDOM.render is not a function` → React 19 的 `react-dom` 不再导出 `createRoot`，补丁改为从 `react-dom/client` 获取。

## 数据表

`categories` `products` `orders` `cards` `users` `ledger` `audit_logs`
`pricing_rules` `levels` `redeem_codes` `redeem_failures` `sync_meta`
`bybit_pool` `crypto_orders`

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
