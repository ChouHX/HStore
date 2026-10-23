import { getDb } from "./db";
import { recomputeAllPrices } from "./pricing";
import type { Platform, RemoteProduct } from "./types";
import { detectPlatform } from "./platform";
import { getDetail } from "./tianqi";

/** 从上游列表导入/上架商品（按选中的分类 dirId） */
export function importProducts(items: RemoteProduct[], dirId: number, platform: Platform): number {
  const db = getDb();
  const now = Date.now();
  const stmt = db.prepare(`
    INSERT INTO products(
      goods_id, name, face_value, sales_price, cost_price, final_price, goods_type, warranty_days, status,
      multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids, platform, root_category_id, enabled, updated_at)
    VALUES(
      @goodsId, @name, @faceValue, @salesPrice, @costPrice, @finalPrice, @goodsType, @warrantyDays, @status,
      @multiple, @isRepeat, @isRefOrder, @isRefMoney, @skuType, @imgUrl, @dirIds, @platform, @rootCategoryId, 1, @updatedAt)
    ON CONFLICT(goods_id) DO UPDATE SET
      name = excluded.name, face_value = excluded.face_value, sales_price = excluded.sales_price,
      cost_price = excluded.cost_price, goods_type = excluded.goods_type, warranty_days = excluded.warranty_days,
      status = excluded.status, multiple = excluded.multiple, is_repeat = excluded.is_repeat,
      is_ref_order = excluded.is_ref_order, is_ref_money = excluded.is_ref_money, sku_type = excluded.sku_type,
      img_url = excluded.img_url, dir_ids = excluded.dir_ids, root_category_id = excluded.root_category_id,
      enabled = 1, updated_at = excluded.updated_at
  `);

  const rootId = getDirRootId(dirId);
  let count = 0;
  db.transaction(() => {
    for (const p of items) {
      stmt.run({
        goodsId: p.goodsId,
        name: p.name,
        faceValue: p.faceValue ?? 0,
        salesPrice: p.salesPrice ?? 0,
        costPrice: p.salesPrice ?? 0,
        finalPrice: p.salesPrice ?? 0,
        goodsType: p.goodsType ?? 1,
        warrantyDays: p.warrantyDays ?? p.warrantyTime ?? 0,
        status: p.status ?? 1,
        multiple: p.multiple ?? 1,
        isRepeat: p.isRepeat ?? 0,
        isRefOrder: p.isRefOrder ?? 0,
        isRefMoney: p.isRefMoney ?? 0,
        skuType: p.skuType ?? 0,
        imgUrl: p.imgUrl ?? "",
        dirIds: JSON.stringify(p.dirIds?.length ? p.dirIds : [dirId]),
        platform,
        rootCategoryId: rootId,
        updatedAt: now,
      });
      count++;
    }
  })();
  recomputeAllPrices();
  return count;
}


/** 向上找到顶级分类 ID */
export function getDirRootId(dirId: number): number | null {
  const db = getDb();
  const get = db.prepare("SELECT id, parent_id FROM categories WHERE id = ?");
  let cur = get.get(dirId) as { id: number; parent_id: number | null } | undefined;
  let guard = 0;
  while (cur && cur.parent_id != null && guard++ < 20) {
    cur = get.get(cur.parent_id) as typeof cur;
  }
  return cur?.id ?? null;
}

/** 向上找到顶级分类名称 */
export function getDirRootName(dirId: number): string {
  const db = getDb();
  const get = db.prepare("SELECT id, name, parent_id FROM categories WHERE id = ?");
  let cur = get.get(dirId) as { id: number; name: string; parent_id: number | null } | undefined;
  let guard = 0;
  while (cur && cur.parent_id != null && guard++ < 20) {
    cur = get.get(cur.parent_id) as typeof cur;
  }
  return cur?.name ?? "";
}

export function platformOfDir(dirId: number): Platform {
  return detectPlatform(getDirRootName(dirId));
}

export function importedGoodsSet(ids: number[]): Set<number> {
  if (ids.length === 0) return new Set();
  const rows = getDb()
    .prepare(`SELECT goods_id FROM products WHERE goods_id IN (${ids.map((n) => Number(n)).join(",")})`)
    .all() as { goods_id: number }[];
  return new Set(rows.map((r) => r.goods_id));
}

/** 导入后低频拉取详情补全库存（列表接口不返回库存） */
export async function enrichImportedStock(ids: number[]): Promise<void> {
  if (ids.length === 0) return;
  const db = getDb();
  const update = db.prepare("UPDATE products SET stock_count = ?, stock_synced_at = ? WHERE goods_id = ?");
  let cursor = 0;
  const list = ids.slice(0, 30);
  async function worker() {
    while (cursor < list.length) {
      const id = list[cursor++];
      try {
        const d = await getDetail(id);
        update.run(typeof d.stockCount === "number" ? d.stockCount : -1, Date.now(), id);
      } catch {
        /* ignore */
      }
      await new Promise((r) => setTimeout(r, 150));
    }
  }
  await Promise.all([worker(), worker()]);
}

/** 商城加载商品时，批量刷新上游库存（节流：60 秒内不重复拉取） */
export async function refreshStockForProducts(
  items: { goodsId: number; stockCount?: number }[],
  max = 20,
): Promise<void> {
  const ids = items.map((i) => Number(i.goodsId)).slice(0, max).filter(Boolean);
  if (ids.length === 0) return;
  const db = getDb();
  const cutoff = Date.now() - 60_000;
  const stale = db
    .prepare(`SELECT goods_id FROM products WHERE goods_id IN (${ids.join(",")}) AND stock_synced_at < ?`)
    .all(cutoff) as { goods_id: number }[];
  if (stale.length === 0) return;

  let cursor = 0;
  const update = db.prepare("UPDATE products SET stock_count = ?, stock_synced_at = ? WHERE goods_id = ?");
  async function worker() {
    while (cursor < stale.length) {
      const id = stale[cursor++].goods_id;
      try {
        const d = await getDetail(id);
        const stock = typeof d.stockCount === "number" ? d.stockCount : -1;
        update.run(stock, Date.now(), id);
        const it = items.find((x) => Number(x.goodsId) === id);
        if (it) it.stockCount = stock;
      } catch {
        /* ignore */
      }
      await new Promise((r) => setTimeout(r, 80));
    }
  }
  await Promise.all(Array.from({ length: Math.min(4, stale.length) }, () => worker()));
}
