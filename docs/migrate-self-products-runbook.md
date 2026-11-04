# 生产执行手册：#5768 / #5769 / #5500 迁移为自营商品

> ✅ **已于 2026-11-04 09:00 在生产执行完毕**，本文件转为「执行记录 + 回滚手册」保留。
>
> 实际执行结果：
>
> - 镜像：`ghcr.io/chouhx/hstore:latest`（提交 `b1779e1`，含 6b 的自营保护，同步不再冲掉 `root_category_id` / 价格）；
> - 备份：`/root/api/shop/data/faka.db.bak-20261104085451`（`.backup` 生成，4.7MB）；
> - 迁移：`sqlite3 data/faka.db < migrate_self_products.sql`；重跑一次 `RowsAffected=0`（幂等已验证）；
> - 结果：`5500 / 5768 / 5769` 三行 = `self | fallback=自身 | root=900001`，价格与上下架未变，自检 SQL 无输出。
>
> 下面第 0~5 节保留原始步骤与当时的实测值，供复盘与回滚参考；**不要重复执行第 3 节**（虽然幂等）。

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

三商品**迁移前**状态（2026-11-04 备份后实测，`goods_id|platform|fulfillment|fallback|root|enabled|stock_count|price|price_locked`）：

```
5500|microsoft|upstream|0|17|1|195541|0.05|0
5768|microsoft|upstream|0|17|1|121448|0.064|0
5769|microsoft|upstream|0|17|1|543205|0.064|0
```

> 早期的草稿里记过 `195664 / 159221 / 304675`，那是更早一次只读采样；
> `stock_count` 每两分钟被 `stockLoop` 刷新，数字会变，**不是**判断依据，看 `fulfillment / root` 即可。

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

预期输出（执行前，2026-11-04 实测值）：

```
5500|microsoft|upstream|0|17|1|195541
5768|microsoft|upstream|0|17|1|121448
5769|microsoft|upstream|0|17|1|543205
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
5500|microsoft|self|5500|900001|1|195541
5768|microsoft|self|5768|900001|1|121448
5769|microsoft|self|5769|900001|1|543205
```

> 2026-11-04 实际执行输出与上面完全一致（`stock_count` 之后每两分钟随 `stockLoop` 变化，属正常）。
> 备份文件：`/root/api/shop/data/faka.db.bak-20261104085451`。

再确认自营分类还在（脚本会 upsert 它，理论上不变）：

```bash
sqlite3 data/faka.db "SELECT id,name,platform FROM categories WHERE id=900001;"
# 900001|自营商品|self
```

---

## 5. 功能确认

> 📌 早期版本的 `selfStockAvailable()` / `maskSelfProductStock()` 已废弃，改为
> `handlers_store.go` 里的字段白名单（`storeProductViewOf` / `storeDetailView`）：
> `stock_count` 按真实数字返回（-1 未知 / 0 缺货 / N 可售），前台展示为「库存 -」「缺货」「库存 N」。
> 2026-11-04 实测线上 `?rootId=900001` 返回 `stockCount` 0/0/0/1204，字段集合
> `customDesc,customImg,customName,finalPrice,fulfillment,goodsId,goodsType,imgUrl,name,status,stockCount`，
> 无 `platform` / `cost` / 上游编号泄漏。

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

## 6. 同步行为：线上已是「保护版」（本节留作背景）

### 6a. 旧镜像的行为（**已不再适用**）

线上镜像构建于 `2026-11-02T16:24:58Z`，那份 `catalog.go` 会把 `root_category_id` 冲回 `17`、
`final_price` 冲回上游价。**这段描述对应部署 `b1779e1` 之前的镜像，已作废。**

| 字段 | 旧镜像 | 现镜像（`b1779e1`） |
| --- | --- | --- |
| `fulfillment` | ✅ 保住 | ✅ 保住 |
| `fallback_goods_id` | ✅ 保住 | ✅ 保住 |
| `root_category_id` | ❌ 冲回 17 | ✅ 保住（`CASE WHEN fulfillment='self'`） |
| `final_price` | ❌ 冲回上游价 | ✅ 保住（`self` 或 `price_locked=1`） |
| `name` / `stock_count` | ❌ 被上游覆盖 | ❌ 仍被上游覆盖（正常） |
| `enabled` | ✅ 保住 | ✅ 保住 |

→ 现镜像下**同步不再需要补刀**；若哪天回滚到旧镜像，才需要「同步后重跑迁移脚本」。

### 6b. 保护版 SQL（**已提交、已部署**）

`backend/internal/upstream/catalog.go` 对 `fulfillment='self'` 的商品做了保护：

```sql
root_category_id = CASE WHEN COALESCE(products.fulfillment,'upstream')='self'
  THEN products.root_category_id ELSE excluded.root_category_id END,
final_price = CASE WHEN COALESCE(products.fulfillment,'upstream')='self'
  OR COALESCE(products.price_locked,0)=1
  THEN products.final_price ELSE excluded.final_price END,
```

提交 `dcd5197` / `b1779e1`，CI 通过后已随 `ghcr.io/chouhx/hstore:latest` 上线（2026-11-04）。
本地对照实验结论仍成立：保护版同步后三者 `root` 保持 `900001`、价格不变。

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
# 用第 1 步打印的备份文件名（本次执行的是 20261104085451）
TS=20261104085451

# 停容器，避免回滚时 WAL 仍在写
docker stop hstore

cd /root/api/shop/data
rm -f faka.db faka.db-wal faka.db-shm
cp faka.db.bak-${TS} faka.db
chown 100:101 faka.db      # 容器内是 app(100:101)，不改属主会被拒绝写入
chmod 644 faka.db

docker start hstore
docker ps --filter name=hstore --format '{{.Names}}\t{{.Status}}'
docker logs --tail 20 hstore
```

> 现网 `data/` 目录是 `drwxrwxrwx`，所以即便宿主机 `faka.db` 属主是 `_apt:systemd-journal`
> 容器也能正常读写（2026-11-04 实测 healthy）。回滚时仍建议显式 `chown 100:101`，
> 免得以后目录权限被收紧。
>
> `.backup` 会顺带生成 `faka.db.bak-<TS>-shm` / `-wal` 两个伴生文件（本次为 0 字节），
> 属于正常现象，不要拿它们当数据文件。
>
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
