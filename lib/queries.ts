import { getDb } from "./db";
import type { Category, Platform, Product } from "./types";

interface ProductRow {
  goods_id: number;
  name: string;
  face_value: number;
  sales_price: number;
  cost_price: number;
  final_price: number;
  goods_type: number;
  warranty_days: number;
  status: number;
  multiple: number;
  is_repeat: number;
  is_ref_order: number;
  is_ref_money: number;
  sku_type: number;
  img_url: string;
  dir_ids: string;
  platform: Platform;
  enabled: number;
  manual_price: number | null;
  price_locked: number;
  stock_count: number;
  root_category_id: number | null;
  custom_name: string | null;
  custom_desc: string | null;
  custom_img: string | null;
  updated_at: number;
}

function mapProduct(r: ProductRow): Product {
  let dirIds: number[] = [];
  try {
    dirIds = JSON.parse(r.dir_ids || "[]");
  } catch {
    dirIds = [];
  }
  return {
    goodsId: r.goods_id,
    name: r.name,
    faceValue: r.face_value,
    costPrice: r.cost_price,
    finalPrice: r.final_price,
    salesPrice: r.sales_price,
    goodsType: r.goods_type,
    warrantyDays: r.warranty_days,
    status: r.status,
    multiple: r.multiple,
    isRepeat: r.is_repeat,
    isRefOrder: r.is_ref_order,
    isRefMoney: r.is_ref_money,
    skuType: r.sku_type,
    imgUrl: r.img_url,
    dirIds,
    platform: r.platform,
    enabled: r.enabled,
    manualPrice: r.manual_price,
    priceLocked: r.price_locked,
    stockCount: r.stock_count ?? -1,
    rootCategoryId: r.root_category_id ?? null,
    customName: r.custom_name ?? null,
    customDesc: r.custom_desc ?? null,
    customImg: r.custom_img ?? null,
    updatedAt: r.updated_at,
  };
}

export function getProduct(goodsId: number): Product | null {
  const row = getDb().prepare("SELECT * FROM products WHERE goods_id = ?").get(goodsId) as
    | ProductRow
    | undefined;
  return row ? mapProduct(row) : null;
}

/** 前台商品：仅上架且在售 */
export function getProducts(platform: Platform): Product[] {
  const rows = getDb()
    .prepare(
      `SELECT * FROM products WHERE platform = ? AND enabled = 1 AND status = 1
       ORDER BY final_price ASC, goods_id DESC`,
    )
    .all(platform) as ProductRow[];
  return rows.map(mapProduct);
}

export interface ProductFilter {
  keyword?: string;
  platform?: string;
  categoryId?: number;
  rootIds?: number[];
  subIds?: number[];
  enabledList?: number[];
  enabled?: string;
  limit?: number;
  offset?: number;
}

function buildProductWhere(filter: ProductFilter): { where: string; params: Record<string, unknown> } {
  const where: string[] = [];
  const params: Record<string, unknown> = {};
  if (filter.keyword) {
    where.push("(name LIKE @kw OR custom_name LIKE @kw OR CAST(goods_id AS TEXT) = @kwExact)");
    params.kw = `%${filter.keyword}%`;
    params.kwExact = filter.keyword;
  }
  if (filter.platform) {
    where.push("platform = @platform");
    params.platform = filter.platform;
  }
  if (filter.categoryId) {
    where.push("dir_ids LIKE @dir");
    params.dir = `%${filter.categoryId}%`;
  }
  if (filter.rootIds?.length) {
    where.push(`root_category_id IN (${filter.rootIds.map((n) => Number(n)).join(",")})`);
  }
  if (filter.subIds?.length) {
    const ors = filter.subIds.map((n) => `dir_ids LIKE '%${Number(n)}%'`);
    where.push(`(${ors.join(" OR ")})`);
  }
  if (filter.enabledList?.length) {
    where.push(`enabled IN (${filter.enabledList.map((n) => Number(n)).join(",")})`);
  } else if (filter.enabled === "1" || filter.enabled === "0") {
    where.push("enabled = @enabled");
    params.enabled = Number(filter.enabled);
  }
  return { where: where.length ? "WHERE " + where.join(" AND ") : "", params };
}

/** 后台商品列表（含下架） */
export interface AdminProduct extends Product {
  rootName: string;
  subName: string;
}

export function listProductsAdmin(filter: ProductFilter = {}): AdminProduct[] {
  const db = getDb();
  const { where, params } = buildProductWhere(filter);
  const rows = db
    .prepare(
      `SELECT * FROM products ${where} ORDER BY root_category_id ASC, final_price ASC LIMIT @limit OFFSET @offset`,
    )
    .all({ ...params, limit: filter.limit ?? 20, offset: filter.offset ?? 0 }) as ProductRow[];

  const cats = db.prepare("SELECT id, name, parent_id FROM categories").all() as {
    id: number;
    name: string;
    parent_id: number | null;
  }[];
  const byId = new Map(cats.map((c) => [c.id, c]));
  const rootOf = (id: number): number | null => {
    let cur = byId.get(id);
    let guard = 0;
    while (cur && cur.parent_id != null && guard++ < 20) cur = byId.get(cur.parent_id);
    return cur ? cur.id : null;
  };

  return rows.map((r) => {
    const p = mapProduct(r);
    const firstDir = p.dirIds[0];
    const rootId = p.rootCategoryId ?? (firstDir ? rootOf(firstDir) : null);
    const rootCat = rootId ? byId.get(rootId) : undefined;
    const subCat = firstDir ? byId.get(firstDir) : undefined;
    return {
      ...p,
      rootName: rootCat?.name ?? "未分类",
      subName: subCat && subCat.id !== rootId ? subCat.name : "",
    };
  });
}

export function deleteProducts(goodsIds: number[]): number {
  const db = getDb();
  const stmt = db.prepare("DELETE FROM products WHERE goods_id = ?");
  db.transaction((ids: number[]) => {
    for (const id of ids) stmt.run(id);
  })(goodsIds);
  return goodsIds.length;
}

export function countProductsAdmin(filter: ProductFilter = {}): number {
  const { where, params } = buildProductWhere(filter);
  const row = getDb()
    .prepare(`SELECT COUNT(*) c FROM products ${where}`)
    .get(params) as { c: number };
  return row.c;
}

export function getCategories(platform: Platform): Category[] {
  const rows = getDb()
    .prepare(
      `SELECT id, name, img, parent_id, platform, updated_at FROM categories
       WHERE platform = ? ORDER BY parent_id IS NOT NULL, name`,
    )
    .all(platform) as {
    id: number;
    name: string;
    img: string;
    parent_id: number | null;
    platform: Platform;
    updated_at: number;
  }[];
  return rows.map((r) => ({
    id: r.id,
    name: r.name,
    img: r.img,
    parentId: r.parent_id,
    platform: r.platform,
    updatedAt: r.updated_at,
  }));
}

export interface PlatformStats {
  total: number;
  onSale: number;
  minPrice: number | null;
  maxPrice: number | null;
}

export function getStats(platform: Platform): PlatformStats {
  const row = getDb()
    .prepare(
      `SELECT COUNT(*) AS total,
              SUM(CASE WHEN enabled = 1 AND status = 1 THEN 1 ELSE 0 END) AS onSale,
              MIN(CASE WHEN enabled = 1 AND status = 1 THEN final_price END) AS minPrice,
              MAX(CASE WHEN enabled = 1 AND status = 1 THEN final_price END) AS maxPrice
       FROM products WHERE platform = ?`,
    )
    .get(platform) as {
    total: number;
    onSale: number | null;
    minPrice: number | null;
    maxPrice: number | null;
  };
  return {
    total: row?.total ?? 0,
    onSale: row?.onSale ?? 0,
    minPrice: row?.minPrice ?? null,
    maxPrice: row?.maxPrice ?? null,
  };
}

/** 上下架（单个或批量） */
export function setProductsEnabled(goodsIds: number[], enabled: boolean): number {
  const db = getDb();
  const stmt = db.prepare("UPDATE products SET enabled = ?, updated_at = ? WHERE goods_id = ?");
  const now = Date.now();
  const tx = db.transaction((ids: number[]) => {
    for (const id of ids) stmt.run(enabled ? 1 : 0, now, id);
  });
  tx(goodsIds);
  return goodsIds.length;
}

/** 手动锁价 / 解除锁价 */
export function setProductManualPrice(goodsId: number, price: number | null, locked: boolean): void {
  const db = getDb();
  if (locked && price != null) {
    db.prepare("UPDATE products SET manual_price = ?, price_locked = 1, final_price = ?, updated_at = ? WHERE goods_id = ?").run(
      price,
      price,
      Date.now(),
      goodsId,
    );
  } else {
    db.prepare("UPDATE products SET manual_price = NULL, price_locked = 0, updated_at = ? WHERE goods_id = ?").run(
      Date.now(),
      goodsId,
    );
  }
}

export function countProducts(): number {
  return (getDb().prepare("SELECT COUNT(*) c FROM products").get() as { c: number }).c;
}

// ===== 商品自定义信息 =====
export interface CustomizedProduct {
  goodsId: number;
  name: string;
  platform: Platform;
  customName: string | null;
  customDesc: string | null;
  customImg: string | null;
}

export function listCustomizedProducts(keyword?: string, limit = 200): CustomizedProduct[] {
  const where = [
    "custom_name IS NOT NULL AND custom_name <> ''",
    keyword ? "(name LIKE @kw OR custom_name LIKE @kw OR CAST(goods_id AS TEXT) = @kwExact)" : "",
  ]
    .filter(Boolean)
    .join(" AND ");
  const params: Record<string, unknown> = { limit };
  if (keyword) {
    params.kw = `%${keyword}%`;
    params.kwExact = keyword;
  }
  const rows = getDb()
    .prepare(
      `SELECT goods_id, name, platform, custom_name, custom_desc, custom_img FROM products
       WHERE ${where} ORDER BY updated_at DESC LIMIT @limit`,
    )
    .all(params) as {
    goods_id: number;
    name: string;
    platform: Platform;
    custom_name: string | null;
    custom_desc: string | null;
    custom_img: string | null;
  }[];
  return rows.map((r) => ({
    goodsId: r.goods_id,
    name: r.name,
    platform: r.platform,
    customName: r.custom_name,
    customDesc: r.custom_desc,
    customImg: r.custom_img,
  }));
}

export function setProductCustomInfo(
  goodsId: number,
  info: { name: string; desc: string; img: string } | null,
): boolean {
  const exists = getDb().prepare("SELECT 1 FROM products WHERE goods_id = ?").get(goodsId);
  if (!exists) return false;
  getDb()
    .prepare(
      "UPDATE products SET custom_name = ?, custom_desc = ?, custom_img = ?, updated_at = ? WHERE goods_id = ?",
    )
    .run(info?.name || null, info?.desc || null, info?.img || null, Date.now(), goodsId);
  return true;
}

// ===== 动态商城分类（按有上架商品的顶级分类生成） =====
export interface StoreSubCategory {
  id: number;
  name: string;
  img: string;
}

export interface StoreRoot {
  id: number;
  name: string;
  img: string;
  children: StoreSubCategory[];
  products: Product[];
  stats: PlatformStats;
}

export function getStorefront(): StoreRoot[] {
  const db = getDb();
  const cats = db
    .prepare("SELECT id, name, img, parent_id FROM categories")
    .all() as { id: number; name: string; img: string; parent_id: number | null }[];
  const byId = new Map(cats.map((c) => [c.id, c]));

  function rootIdOf(id: number): number | null {
    let cur = byId.get(id);
    let guard = 0;
    while (cur && cur.parent_id != null && guard++ < 20) {
      cur = byId.get(cur.parent_id);
    }
    return cur ? cur.id : null;
  }

  const rows = db
    .prepare("SELECT * FROM products WHERE enabled = 1 AND status = 1 ORDER BY final_price ASC")
    .all() as ProductRow[];

  const groups = new Map<number, Product[]>();
  for (const row of rows) {
    const p = mapProduct(row);
    let rid: number | null = null;
    for (const d of p.dirIds) {
      const r = rootIdOf(d);
      if (r != null) {
        rid = r;
        break;
      }
    }
    if (rid == null) continue;
    const arr = groups.get(rid) ?? [];
    arr.push(p);
    groups.set(rid, arr);
  }

  const roots: StoreRoot[] = [];
  for (const [rid, list] of groups) {
    const cat = byId.get(rid);
    if (!cat) continue;
    const children = cats
      .filter((c) => c.parent_id === rid)
      .map((c) => ({ id: c.id, name: c.name, img: c.img }));
    const prices = list.map((p) => p.finalPrice);
    roots.push({
      id: rid,
      name: cat.name,
      img: cat.img,
      children,
      products: list,
      stats: {
        total: list.length,
        onSale: list.length,
        minPrice: prices.length ? Math.min(...prices) : null,
        maxPrice: prices.length ? Math.max(...prices) : null,
      },
    });
  }
  roots.sort((a, b) => b.stats.onSale - a.stats.onSale || a.name.localeCompare(b.name, "zh"));
  return roots;
}

// ===== 分类树（后台拉取用） =====
export interface CategoryNode {
  key: string;
  id: number;
  title: string;
  productCount: number;
  children?: CategoryNode[];
}

export function getCategoryTree(): CategoryNode[] {
  const db = getDb();
  const cats = db
    .prepare("SELECT id, name, parent_id FROM categories ORDER BY name")
    .all() as { id: number; name: string; parent_id: number | null }[];
  const counts = new Map<number, number>();
  for (const r of db.prepare("SELECT dir_ids FROM products").all() as { dir_ids: string }[]) {
    try {
      for (const d of JSON.parse(r.dir_ids || "[]") as number[]) {
        counts.set(d, (counts.get(d) ?? 0) + 1);
      }
    } catch {
      /* ignore */
    }
  }
  const byParent = new Map<number | null, typeof cats>();
  for (const c of cats) {
    const arr = byParent.get(c.parent_id) ?? [];
    arr.push(c);
    byParent.set(c.parent_id, arr);
  }
  function build(parent: number | null): CategoryNode[] {
    return (byParent.get(parent) ?? []).map((c) => {
      const kids = build(c.id);
      return {
        key: String(c.id),
        id: c.id,
        title: c.name,
        productCount: counts.get(c.id) ?? 0,
        ...(kids.length ? { children: kids } : {}),
      };
    });
  }
  return build(null);
}

// ===== 板块卡片 + 按需加载商品 =====
export interface StoreBoard {
  id: number;
  name: string;
  img: string;
  total: number;
  minPrice: number | null;
  maxPrice: number | null;
  children: StoreSubCategory[];
}

export function getStoreBoards(): StoreBoard[] {
  const db = getDb();
  const rows = db
    .prepare(
      `SELECT c.id, c.name, c.img, COUNT(p.goods_id) AS total,
              MIN(p.final_price) AS minPrice, MAX(p.final_price) AS maxPrice
       FROM products p JOIN categories c ON c.id = p.root_category_id
       WHERE p.enabled = 1 AND p.status = 1
       GROUP BY c.id
       ORDER BY total DESC, c.name ASC`,
    )
    .all() as {
    id: number;
    name: string;
    img: string;
    total: number;
    minPrice: number | null;
    maxPrice: number | null;
  }[];

  const cats = db
    .prepare("SELECT id, name, img, parent_id FROM categories")
    .all() as { id: number; name: string; img: string; parent_id: number | null }[];
  // 收集有在售商品的分类目录，过滤掉没有商品的子分类
  const usedDirs = new Set<number>();
  const dirRows = db
    .prepare("SELECT dir_ids FROM products WHERE enabled = 1 AND status = 1")
    .all() as { dir_ids: string }[];
  for (const r of dirRows) {
    try {
      for (const d of JSON.parse(r.dir_ids || "[]") as number[]) usedDirs.add(d);
    } catch {
      /* ignore */
    }
  }

  const byParent = new Map<number, StoreSubCategory[]>();
  for (const c of cats) {
    if (c.parent_id == null) continue;
    if (!usedDirs.has(c.id)) continue;
    const arr = byParent.get(c.parent_id) ?? [];
    arr.push({ id: c.id, name: c.name, img: c.img });
    byParent.set(c.parent_id, arr);
  }

  return rows.map((r) => ({ ...r, children: byParent.get(r.id) ?? [] }));
}

export type StoreSort = "name-asc" | "price-asc" | "price-desc" | "stock-desc";

export interface StoreProductQuery {
  rootId?: number;
  dirId?: number;
  keyword?: string;
  sort?: StoreSort;
  page: number;
  pageSize: number;
}

export function listStoreProducts(q: StoreProductQuery): { items: Product[]; total: number } {
  const db = getDb();
  const where: string[] = ["enabled = 1", "status = 1"];
  const params: Record<string, unknown> = {};
  if (q.rootId) {
    where.push("root_category_id = @rootId");
    params.rootId = q.rootId;
  }
  if (q.dirId) {
    where.push("dir_ids LIKE @dir");
    params.dir = `%${q.dirId}%`;
  }
  if (q.keyword) {
    where.push("(name LIKE @kw OR custom_name LIKE @kw OR CAST(goods_id AS TEXT) = @kwExact)");
    params.kw = `%${q.keyword}%`;
    params.kwExact = q.keyword;
  }
  const sortExpr =
    q.sort === "price-asc"
      ? "final_price ASC"
      : q.sort === "price-desc"
        ? "final_price DESC"
        : q.sort === "stock-desc"
          ? "stock_count DESC"
          : "COALESCE(NULLIF(custom_name, ''), name) ASC";
  const priority = "CASE WHEN stock_count > 0 THEN 0 WHEN stock_count = 0 THEN 2 ELSE 1 END";
  const whereSql = "WHERE " + where.join(" AND ");
  const rows = db
    .prepare(`SELECT * FROM products ${whereSql} ORDER BY ${priority}, ${sortExpr} LIMIT @limit OFFSET @offset`)
    .all({ ...params, limit: q.pageSize, offset: (q.page - 1) * q.pageSize }) as ProductRow[];
  const total = (
    db.prepare(`SELECT COUNT(*) c FROM products ${whereSql}`).get(params) as { c: number }
  ).c;
  return { items: rows.map(mapProduct), total };
}

// ===== 后台商品筛选选项（仅返回已存在的板块/分类） =====
export interface ProductFilterOptions {
  roots: { id: number; name: string; count: number }[];
  subs: { id: number; name: string; rootId: number | null; count: number }[];
  status: { id: number; name: string; count: number }[];
}

export function getProductFilterOptions(): ProductFilterOptions {
  const db = getDb();
  const roots = db
    .prepare(
      `SELECT c.id, c.name, COUNT(*) AS count
       FROM products p JOIN categories c ON c.id = p.root_category_id
       GROUP BY c.id ORDER BY c.name`,
    )
    .all() as { id: number; name: string; count: number }[];

  const cats = db.prepare("SELECT id, name, parent_id FROM categories").all() as {
    id: number;
    name: string;
    parent_id: number | null;
  }[];
  const byId = new Map(cats.map((c) => [c.id, c]));

  const subCount = new Map<number, number>();
  for (const r of db.prepare("SELECT dir_ids FROM products").all() as { dir_ids: string }[]) {
    try {
      for (const d of JSON.parse(r.dir_ids || "[]") as number[]) {
        subCount.set(d, (subCount.get(d) ?? 0) + 1);
      }
    } catch {
      /* ignore */
    }
  }
  const subs = [...subCount.entries()]
    .map(([id, count]) => {
      const c = byId.get(id);
      // 只保留真正的子分类，排除板块（顶级分类）
      return c && c.parent_id != null ? { id, name: c.name, rootId: c.parent_id, count } : null;
    })
    .filter((x): x is { id: number; name: string; rootId: number; count: number } => Boolean(x))
    .sort((a, b) => a.name.localeCompare(b.name, "zh"));

  const status = (
    db.prepare("SELECT enabled, COUNT(*) AS count FROM products GROUP BY enabled ORDER BY enabled DESC").all() as {
      enabled: number;
      count: number;
    }[]
  ).map((r) => ({ id: r.enabled, name: r.enabled === 1 ? "上架" : "下架", count: r.count }));

  return { roots, subs, status };
}
