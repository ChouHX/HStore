import { getDb, setMeta } from "./db";
import { detectPlatform } from "./platform";
import { recomputeAllPrices } from "./pricing";
import { getDirs, getDetail, getList, isConfigured, TianqiError } from "./tianqi";
import type { Platform, RemoteCategory, RemoteProduct, SyncResult } from "./types";

interface FlatCategory {
  id: number;
  name: string;
  img: string;
  parentId: number | null;
  platform: Platform;
  rootId: number;
}

function flattenCategories(
  cats: RemoteCategory[],
  parentId: number | null = null,
  inherited: Platform = "other",
  inheritedRoot: number | null = null,
  out: FlatCategory[] = [],
): FlatCategory[] {
  for (const c of cats) {
    // 平台只由「一级板块」决定，子分类继承父级平台
    const platform: Platform = parentId === null ? detectPlatform(c.name) : inherited;
    const rootId = parentId === null ? c.id : (inheritedRoot ?? c.id);
    out.push({
      id: c.id,
      name: c.name,
      img: c.img ?? "",
      parentId,
      platform,
      rootId,
    });
    if (c.children?.length) flattenCategories(c.children, c.id, platform, rootId, out);
  }
  return out;
}

const MAX_PAGES = 60;

/** 兼容 warrantyDays 与 warrantyTime/warrantyUnit 两种字段，统一换算为天数 */
function normalizeWarranty(p: RemoteProduct): number {
  if (typeof p.warrantyDays === "number") return p.warrantyDays;
  const time = p.warrantyTime ?? 0;
  const unit = p.warrantyUnit ?? 0;
  if (time <= 0) return 0;
  if (unit === 1) return time * 30;
  if (unit === 2) return time * 365;
  return time;
}

/** 该分类没有商品时接口返回 code=1001 / msg=无商品数据，视为空即可 */
function isEmptyDataError(err: unknown): boolean {
  return err instanceof TianqiError && /无商品数据|无数据/.test(err.message);
}

/** 低频并发拉取商品详情补写库存（商品列表接口不返回库存；过快会触发站点限流） */
async function enrichStock(ids: number[]): Promise<void> {
  if (ids.length === 0) return;
  const db = getDb();
  const update = db.prepare("UPDATE products SET stock_count = ?, stock_synced_at = ? WHERE goods_id = ?");
  const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

  async function run(list: number[], concurrency: number, delayMs: number) {
    let cursor = 0;
    async function worker() {
      while (cursor < list.length) {
        const id = list[cursor++];
        try {
          const d = await getDetail(id);
          update.run(typeof d.stockCount === "number" ? d.stockCount : -1, Date.now(), id);
        } catch {
          /* 单个失败保留原值 */
        }
        if (delayMs) await sleep(delayMs);
      }
    }
    await Promise.all(Array.from({ length: Math.min(concurrency, list.length) }, () => worker()));
  }

  await run(ids, 3, 150);
  // 对仍未知的商品再低频重试一次
  const remaining = (
    db.prepare("SELECT goods_id FROM products WHERE stock_count < 0").all() as {
      goods_id: number;
    }[]
  ).map((r) => r.goods_id);
  if (remaining.length) await run(remaining, 1, 400);
}

/** 拉取远端分类 + 商品（微软邮箱 / 谷歌邮箱），写入 SQLite */
export async function syncFromRemote(): Promise<SyncResult> {
  const now = Date.now();
  const dirs = await getDirs();
  const flat = flattenCategories(dirs);
  const dirRoot = new Map<number, number>(flat.map((c) => [c.id, c.rootId]));

  const db = getDb();
  // 远端同步前先清空本平台旧数据（含演示数据），保证库内为远端最新快照
  db.prepare("DELETE FROM products WHERE platform IN ('microsoft','google')").run();
  db.prepare("DELETE FROM categories WHERE platform IN ('microsoft','google')").run();
  const upsertCategory = db.prepare(`
    INSERT INTO categories(id, name, img, parent_id, platform, updated_at)
    VALUES(@id, @name, @img, @parentId, @platform, @updatedAt)
    ON CONFLICT(id) DO UPDATE SET
      name = excluded.name, img = excluded.img, parent_id = excluded.parent_id,
      platform = excluded.platform, updated_at = excluded.updated_at
  `);

  const targetDirs: FlatCategory[] = [];

  db.transaction(() => {
    for (const c of flat) {
      upsertCategory.run({ ...c, updatedAt: now });
      // 一级分类若自身命中平台，或其子分类命中，则纳入抓取目标
      if (c.platform !== "other") targetDirs.push(c);
    }
  })();

  // 子分类命中的同时把父分类也纳入，保证分类展示完整
  const dirIdsByPlatform: Record<Platform, Set<number>> = {
    microsoft: new Set(),
    google: new Set(),
    other: new Set(),
  };
  for (const c of targetDirs) dirIdsByPlatform[c.platform].add(c.id);

  const upsertProduct = db.prepare(`
    INSERT INTO products(
      goods_id, name, face_value, sales_price, cost_price, final_price, goods_type, warranty_days, status,
      multiple, is_repeat, is_ref_order, is_ref_money, sku_type, img_url, dir_ids,
      platform, root_category_id, updated_at)
    VALUES(
      @goodsId, @name, @faceValue, @salesPrice, @costPrice, @finalPrice, @goodsType, @warrantyDays, @status,
      @multiple, @isRepeat, @isRefOrder, @isRefMoney, @skuType, @imgUrl, @dirIds,
      @platform, @rootCategoryId, @updatedAt)
    ON CONFLICT(goods_id) DO UPDATE SET
      name = excluded.name, face_value = excluded.face_value, sales_price = excluded.sales_price,
      cost_price = excluded.cost_price, final_price = excluded.final_price,
      goods_type = excluded.goods_type, warranty_days = excluded.warranty_days, status = excluded.status,
      multiple = excluded.multiple, is_repeat = excluded.is_repeat, is_ref_order = excluded.is_ref_order,
      is_ref_money = excluded.is_ref_money, sku_type = excluded.sku_type, img_url = excluded.img_url,
      dir_ids = excluded.dir_ids, platform = excluded.platform,
      root_category_id = excluded.root_category_id, updated_at = excluded.updated_at
  `);

  const seen = new Set<number>();
  let count = 0;

  for (const platform of ["microsoft", "google"] as const) {
    for (const dirId of dirIdsByPlatform[platform]) {
      let page = 1;
      let totalPages = 1;
      do {
        let data;
        try {
          data = await getList({ page, dirId: String(dirId), showDirId: "1" });
        } catch (err) {
          if (isEmptyDataError(err)) break;
          throw err;
        }
        const items = data?.items ?? [];
        for (const p of items) {
          if (seen.has(p.goodsId)) continue;
          seen.add(p.goodsId);
          upsertProduct.run({
            goodsId: p.goodsId,
            name: p.name,
            faceValue: p.faceValue ?? 0,
            salesPrice: p.salesPrice ?? 0,
            costPrice: p.salesPrice ?? 0,
            finalPrice: p.salesPrice ?? 0,
            goodsType: p.goodsType ?? 1,
            warrantyDays: normalizeWarranty(p),
            status: p.status ?? 1,
            multiple: p.multiple ?? 1,
            isRepeat: p.isRepeat ?? 0,
            isRefOrder: p.isRefOrder ?? 0,
            isRefMoney: p.isRefMoney ?? 0,
            skuType: p.skuType ?? 0,
            imgUrl: p.imgUrl ?? "",
            dirIds: JSON.stringify(p.dirIds ?? [dirId]),
            platform,
            rootCategoryId: dirRoot.get(p.dirIds?.[0] ?? dirId) ?? dirRoot.get(dirId) ?? null,
            updatedAt: now,
          });
          count++;
        }
        totalPages = Math.max(1, data?.allPage || 1);
        page++;
      } while (page <= totalPages && page <= MAX_PAGES);
    }
  }

  // 商品列表不返回库存，逐个拉取详情补写库存
  await enrichStock([...seen]);

  // 同步完成后按当前定价规则重算最终售价（手动锁价优先）
  recomputeAllPrices();

  setMeta("last_sync", String(now));
  setMeta("last_source", "remote");

  const counts = countByPlatform();
  return {
    ok: true,
    source: "remote",
    categories: flat.length,
    products: count,
    microsoft: counts.microsoft,
    google: counts.google,
    message: `已从远端同步 ${count} 个商品（微软 ${counts.microsoft} / 谷歌 ${counts.google}）`,
  };
}

export function countByPlatform(): Record<Platform, number> {
  const rows = getDb()
    .prepare("SELECT platform, COUNT(*) AS c FROM products GROUP BY platform")
    .all() as { platform: Platform; c: number }[];
  const result: Record<Platform, number> = { microsoft: 0, google: 0, other: 0 };
  for (const r of rows) result[r.platform] = r.c;
  return result;
}

export async function runSync(): Promise<SyncResult> {
  if (isConfigured()) {
    return syncFromRemote();
  }
  return {
    ok: false,
    source: "demo",
    categories: 0,
    products: 0,
    microsoft: 0,
    google: 0,
    message:
      "未检测到 X-App-Id（客户编号），无法调用远端接口。请在 .env.local 中填写 TIANQI_APP_ID 后重试。",
  };
}
