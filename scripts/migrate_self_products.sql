-- ============================================================================
--  migrate_self_products.sql
--  把 #5768 / #5769 / #5500 三个微软商品迁移为「自营商品」，并绑定 fallback 上游采购源。
--
--  特性：
--    * 幂等：重复执行结果完全一致，已经迁移过的行再跑一次不会产生任何变化；
--    * 只更新受影响的 3 行，不动价格、上下架、custom_* 、platform 等业务字段；
--    * 不写入 / 不复制任何库存数据（见文件末尾说明）。
--
--  用法（生产机，务必先备份！）：
--    sqlite3 /root/api/shop/data/faka.db < scripts/migrate_self_products.sql
--  本机没有 sqlite3 命令行时，用等价的 Go 命令：
--    cd backend && FAKA_DB_PATH=<库路径> go run ./cmd/migrate_self
--
--  上游同步兼容性（backend/internal/upstream/catalog.go）：
--    清理语句 DELETE ... WHERE platform IN ('microsoft','google')
--                AND COALESCE(fulfillment,'upstream') <> 'self'
--    → fulfillment='self' 的行被排除，同步不会删除它们；
--    商品 UPSERT 的 ON CONFLICT ... DO UPDATE 列表里**没有** fulfillment /
--    fallback_goods_id → 同步不会把它们改回 upstream，也不会清掉 fallback；
--    但列表里**有** root_category_id → 同步后会被改回上游板块（17），
--    所以同步之后建议重跑一次本脚本（或 cmd/migrate_self）恢复 900001。
--    store.SaveCategories 里的 SelfDirs 会保护自营分类不被清理。
-- ============================================================================

BEGIN TRANSACTION;

-- 1) 确保自营分类 900001「自营商品」存在。
--    platform='self' 的分类不会被上游分类同步清理。
INSERT INTO categories(id, name, img, parent_id, platform, updated_at)
VALUES (900001, '自营商品', '', NULL, 'self', CAST(strftime('%s','now') AS INTEGER) * 1000)
ON CONFLICT(id) DO UPDATE SET name = '自营商品', platform = 'self';

-- 2) 三个商品标记为自营，fallback 指回自身 goods_id。
--    COALESCE / 等值判断保证幂等：值已经正确时 SQLite 不会实际改行。
--    注意：SQLite 的 UPDATE 中 `fallback_goods_id = goods_id` 取的是当前行的原值。
UPDATE products
SET fulfillment      = 'self',
    fallback_goods_id = goods_id,
    root_category_id  = 900001
WHERE goods_id IN (5768, 5769, 5500)
  AND ( COALESCE(fulfillment, '') <> 'self'
        OR COALESCE(fallback_goods_id, 0) <> goods_id
        OR COALESCE(root_category_id, 0) <> 900001 );

COMMIT;

-- ============================================================================
--  迁移后核对（单独执行，不参与上面的迁移事务）：
--
--  SELECT goods_id, fulfillment, fallback_goods_id, root_category_id,
--         COALESCE(enabled,0) AS enabled, stock_count, platform
--  FROM products WHERE goods_id IN (5768, 5769, 5500) ORDER BY goods_id;
--
--  期望结果：
--    | goods_id | fulfillment | fallback_goods_id | root_category_id | platform  |
--    | 5500     | self        | 5500              | 900001           | microsoft |
--    | 5768     | self        | 5768              | 900001           | microsoft |
--    | 5769     | self        | 5769              | 900001           | microsoft |
--
--  关于库存（重要，不要凭空造数据）：
--    self_stock 表里目前没有 5768 / 5769 / 5500 的任何卡密，本脚本也**不会**写入。
--    这是预期行为：自营商品兑换时先扣 self_stock，卡密为空则自动全额走
--    fallback_goods_id 指向的上游采购（fallback = 自身 goods_id，上游同一
--    goods_id 仍可下单），因此 self_stock 为空时功能完全正常。
--    上游的真实库存仍由同步流程写在 products.stock_count 上，保持不动即可。
--    将来如果有本地卡密，用管理后台「自营库存」导入，
--    或者 store.ImportSelfStock(goodsID, contents, source, sourceID)。
--
--  自营商品的可见性：
--    store.ListSelfProducts 查询条件是 `fulfillment='self' AND platform='self'`，
--    而这三个商品保留 platform='microsoft'（这样同步才能继续拉价格/库存/详情）。
--    所以它们出现在上游分类（root_category_id 被同步改回 17）而不是自营板块里；
--    这是刻意取舍，商品在商城前台照常展示、照常可下单。
-- ============================================================================
