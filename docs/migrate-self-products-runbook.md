# 生产执行手册：#5768 / #5769 / #5500 迁移为自营商品

> 本文件只是**待执行的操作手册**，本次任务**没有**对生产库做过任何写操作。
> 唯一的只读确认已经做过（见「0. 实测到的生产现状」），可以放心照抄。

---

## 0. 实测到的生产现状（只读确认，2026-11-03）

服务器 `root@43.166.8.180`，容器 `hstore`（`ghcr.io/chouhx/hstore:latest`，healthy）。
数据库是**宿主机 bind mount**，不是 docker volume：

| 项目 | 值 |
| --- | --- |
| 宿主机路径 | `/root/api/shop/data/faka.db` |
| 容器内路径 | `/data/faka.db`（`FAKA_DB_PATH`） |
| 挂载方式 | `bind /root/api/shop/data → /data`（`docker inspect hstore` 已确认） |
| 宿主机工具 | 有 `sqlite3 3.37.2`；容器内**没有** sqlite3、没有 go |
| 目录内多余文件 | 有个误建的 `+%Y%m%d%H%M%S)` 空文件（以前备份命令引号写错留下的），无害，可删 |

三商品当前状态（**与迁移前一致，说明还没人动过**）：

```
5500|microsoft|upstream|0|17|1|195664
5768|microsoft|upstream|0|17|1|159221
5769|microsoft|upstream|0|17|1|304675
```

`categories` 里 `900001|自营商品|self` **已经存在**，所以迁移只需要动 `products` 三行。

库里**已有一个自营商品** `900018|platform=self|fulfillment=self|root=900001|enabled=1`，
带 1254 条 `self_stock` 记录——它证明自营链路在本库是通的，也说明 900001 板块
在前台有货可展示（`StoreBoards` 能正常显示「自营商品」卡片）。

> ⚠️ 因为库是 bind mount，`docker compose down -v` 不会删数据，但**备份也绝不能靠 volume**，
> 直接 `cp` 宿主机上的 `faka.db` 即可。

---

## 1. 备份（**必做，第一步**）

```bash
sshpass -p '147258aA@' ssh -o StrictHostKeyChecking=no root@43.166.8.180

# 在服务器上执行：
TS=$(date +%Y%m%d%H%M%S)
cd /root/api/shop/data
# 用 sqlite3 的在线备份 API，能把 -wal 里尚未 checkpoint 的事务一并安全合并。
# 直接 cp faka.db 会丢掉 4MB 的 faka.db-wal，拿到的是一个 5 小时前的旧库！
sqlite3 faka.db ".backup '/root/api/shop/data/faka.db.bak-${TS}'"
ls -la /root/api/shop/data/faka.db.bak-${TS}
# 立即校验备份可用（能读出三商品 = 备份有效）
sqlite3 -readonly /root/api/shop/data/faka.db.bak-${TS} \
  "SELECT goods_id, fulfillment, fallback_goods_id, root_category_id FROM products WHERE goods_id IN (5768,5769,5500) ORDER BY goods_id;"
echo "备份文件: /root/api/shop/data/faka.db.bak-${TS}"
```

**把打印出来的备份文件名记下来**，回滚要用。

> 为什么必须用 `.backup` 而不是 `cp`：容器正在跑，WAL 里 `2026-11-03 00:52` 还有 4MB 未落盘的改动，
> 主库文件的时间戳停在 `00:46`。`cp faka.db` 会得到一个**落后几小时的旧库**，回滚时会把订单数据一起丢掉。

---

## 2. 执行前快照（记下"执行前"用于对比）

```bash
sqlite3 /root/api/shop/data/faka.db \
  "SELECT goods_id, platform, fulfillment, fallback_goods_id, COALESCE(root_category_id,0) AS root_cat, COALESCE(enabled,0) AS enabled, stock_count
   FROM products WHERE goods_id IN (5768,5769,5500) ORDER BY goods_id;"
```

预期输出（执行前，2026-11-03 实测值）：

```
5500|microsoft|upstream|0|17|1|195664
5768|microsoft|upstream|0|17|1|159221
5769|microsoft|upstream|0|17|1|304675
```

记录要点：`fulfillment=upstream`、`fallback_goods_id=0`、`root_cat=17`、`enabled=1`。
如果第 2 步看到的已经是 `self` / `900001`，说明之前跑过，直接跳到第 4 步核对。

---

## 3. 执行迁移

把仓库里的 `scripts/migrate_self_products.sql` 传到服务器后执行：

```bash
# 本地（仓库根目录）：
sshpass -p '147258aA@' scp -o StrictHostKeyChecking=no \
  scripts/migrate_self_products.sql root@43.166.8.180:/root/api/shop/migrate_self_products.sql
```

```bash
# 服务器上：
cd /root/api/shop
sqlite3 data/faka.db < migrate_self_products.sql
```

脚本是**幂等**的：重复执行不会产生任何变化（WHERE 里带「值已经正确就不改」条件，
第二次执行 `RowsAffected` 为 0）。

---

## 4. 执行后核对（对比第 2 步）

```bash
sqlite3 /root/api/shop/data/faka.db \
  "SELECT goods_id, platform, fulfillment, fallback_goods_id, COALESCE(root_category_id,0) AS root_cat, COALESCE(enabled,0) AS enabled, stock_count
   FROM products WHERE goods_id IN (5768,5769,5500) ORDER BY goods_id;"
```

预期输出（执行后）——**只有 fulfillment / fallback / root_cat 三列变化，价格/库存/上下架原封不动**：

```
5500|microsoft|self|5500|900001|1|195664
5768|microsoft|self|5768|900001|1|159221
5769|microsoft|self|5769|900001|1|304675
```

再确认自营分类还在（脚本会 upsert 它，理论上不变）：

```bash
sqlite3 data/faka.db "SELECT id,name,platform FROM categories WHERE id=900001;"
# 900001|自营商品|self
```

---

## 5. 功能确认

> ⚠️ 「库存脱敏成 1/0」依赖 `handlers_store.go` 里的 `selfStockAvailable()` / `maskSelfProductStock()`，
> 这**两个函数同样只存在于未提交的工作区**（HEAD 里 `grep -c selfStockAvailable` = 0），
> 当前线上镜像**没有**这段逻辑。线上 `stock_count` 会原样返回上游的数字。
> 这不影响下单（真正决定能不能买的是 `service/order.go` 的 `placeSelfOrder`），
> 只是前台展示口径的差别。部署含此改动的镜像后才会变成 1/0。

直接确认商品在前台可见：

```bash
curl -s --noproxy '*' 'http://127.0.0.1:3001/api/store/products?rootId=900001&pageSize=20' | head -c 500
```

预期：`total >= 3`，且能搜到 `5768 / 5769 / 5500`。

> 说明：这三个商品的 `self_stock` 本来就是空的，**不要造数据**。兑换时逻辑是
> `available(0) < count` → `upstreamNeed = count` → 全额走上游 `fallback_goods_id`
> （= 自身 goods_id）采购，功能完全正常。
>
> 另：这三个商品的 `final_price` 线上目前是很低的价格（实测 0.05/0.06 一类），
> 迁移脚本**不改价格**。如果这个价格不是你有意设的，请在迁移前/后到后台确认并勾上 `price_locked`。

---

## 6. ⚠️ 同步行为取决于线上跑的是哪份二进制（**必读**）

这里有两种行为，**取决于线上镜像里编进去的是哪个版本的 `catalog.go`**：

### 6a. 当前线上镜像：**会**把 `root_category_id` 冲回 17

线上镜像构建于 `2026-11-02T16:24:58Z`，容器内 `hstore-server` 时间戳 `Nov 3 00:24`，
而修复改动（见 6b）是这之后才写进工作区的（`catalog.go` mtime `11-03 00:54`），
**没进当前镜像**。所以现在点「同步商品」之后：

| 字段 | 行为 |
| --- | --- |
| `fulfillment` | ✅ 保住（清理语句 `... AND COALESCE(fulfillment,'upstream') <> 'self'` 跳过它们，不会被删） |
| `fallback_goods_id` | ✅ 保住（不在 UPDATE 列表里） |
| `root_category_id` | ❌ **被改回 `17`** |
| `final_price` | ❌ **被改回上游价**（实测 0.06/0.05 → 9.90） |
| `name` / `stock_count` | ❌ 被上游覆盖（本来就是上游商品，正常） |
| `enabled` | ✅ 保住（不在 UPDATE 列表里） |

→ **暂缓同步 / 同步后立即重跑迁移脚本**，把 `root_category_id` 补回来：

```bash
cd /root/api/shop && sqlite3 data/faka.db < migrate_self_products.sql
```

> 注意：脚本只补 `fulfillment / fallback_goods_id / root_category_id`，**不负责价格**。
> 价格若被同步冲掉，需要在后台「商品管理」重新设价并勾上 `price_locked`，
> 或者先部署 6b 的镜像再同步。
>
> 另外这里有个**线上现状需要你确认**：仓库工作区里的 `catalog.go` 有一份未提交的修复
> （即 6b），但线上镜像不含它。如果这份修复是别的同事正准备提交部署的，那么**部署后
> 同步就安全了**，本节的补刀动作可以省略。请以最终部署的镜像版本为准。

### 6b. 修复后的代码（已在本仓库工作区，**未提交、未部署**）

`backend/internal/upstream/catalog.go` 工作区里已有一版未提交的修改，对
`fulfillment='self'` 的商品做了保护：

```sql
root_category_id = CASE WHEN COALESCE(products.fulfillment,'upstream')='self'
  THEN products.root_category_id ELSE excluded.root_category_id END,
final_price = CASE WHEN COALESCE(products.fulfillment,'upstream')='self'
  OR COALESCE(products.price_locked,0)=1
  THEN products.final_price ELSE excluded.final_price END,
```

**本地已验证这版是有效的**：用假上游跑真实的 `upstream.RunSync()`，
迁移后的三商品 `root_category_id` 保持 `900001`、`final_price` 保持原值、
`fulfillment='self'`、`fallback` 指回自身，只有 `name`/`stock_count` 被上游覆盖（正常）。
对照实验：同一场景换成 HEAD 版 SQL（= 线上镜像行为），三者 `root` 全变 `17`、价格变 `9.90`。

→ **建议：先部署包含 6b 修复的新镜像，再执行本迁移。** 之后同步就无需再补刀。

自查（应该没有任何输出）：

```bash
cd /root/api/shop && sqlite3 data/faka.db \
  "SELECT goods_id FROM products WHERE goods_id IN (5768,5769,5500)
     AND (fulfillment<>'self' OR fallback_goods_id<>goods_id OR COALESCE(root_category_id,0)<>900001);"
```

> 仍可讨论的点：`store.SelfDirs()` 是**全局**集合，一旦有自营商品挂在 900001，
> `SaveCategories` 就会把 900001 从「可清理」名单里剔除——这是保护自营分类的机制。

## 7. 回滚

```bash
# 用第 1 步打印的备份文件名
TS=<你的备份时间戳>

# 停容器，避免回滚时 WAL 仍在写
docker stop hstore

cd /root/api/shop/data
rm -f faka.db faka.db-wal faka.db-shm
cp faka.db.bak-${TS} faka.db
chown 100:101 faka.db      # 实测原文件属主 100:101（容器内 app:app），不改会写不进去
chmod 644 faka.db

docker start hstore
docker ps --filter name=hstore --format '{{.Names}}\t{{.Status}}'
docker logs --tail 20 hstore
```

> 只回滚这三行也可以，不必整库回滚（见附录脚本）。
> 但整库回滚会丢掉备份之后产生的订单，**优先用附录的定向回滚**。

---

## 附录 A：只回滚这三行（推荐）

```bash
cat > /tmp/rollback_self.sql <<'SQL'
BEGIN TRANSACTION;
UPDATE products
SET fulfillment = 'upstream',
    fallback_goods_id = 0,
    root_category_id = 17
WHERE goods_id IN (5768, 5769, 5500);
COMMIT;
SQL
sqlite3 /root/api/shop/data/faka.db < /tmp/rollback_self.sql
```

（`root_category_id = 17` 是迁移前实测值。如果期间做过同步，它本来就是 17，等于没改。）

---

## 附录 B：为什么生产用 SQL 而不是 `go run ./cmd/migrate_self`

容器镜像里只有一个静态编译的 `hstore-server`，**没有 go、没有 sqlite3**，
宿主机也没有 go 工具链。所以生产路径走 `sqlite3 < scripts/migrate_self_products.sql`，
和 `cmd/migrate_self` 的 SQL **逐句等价**（分类 upsert + 三行 UPDATE，条件也一样）。

本地验证时两条路径结果完全一致：先用 `cmd/migrate_self` 迁移，
再跑 SQL 脚本得到 `RowsAffected=0`、达标 3/3，证明二者等价且都幂等。

如果哪台机器有 go 工具链，也可以直接：

```bash
cd backend
FAKA_DB_PATH=/root/api/shop/data/faka.db go run ./cmd/migrate_self -dry-run   # 先看
FAKA_DB_PATH=/root/api/shop/data/faka.db go run ./cmd/migrate_self            # 再改
FAKA_DB_PATH=/root/api/shop/data/faka.db go run ./cmd/migrate_self -verify    # 事后自检
```
