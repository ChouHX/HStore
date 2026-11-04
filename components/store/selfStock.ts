import type { StoreProduct, GoodsDetail } from "@/lib/types";

/**
 * 商品库存展示口径。
 *
 * 后端下发的 stockCount 就是真实库存条数（自营商品是 self_stock 里 available 的条数，
 * 上游商品是定时同步下来的值），前端直接展示数字：
 * - stockCount < 0                → 库存未知（上游商品还没同步到）
 * - stockCount === 0              → 缺货；自营商品若 hasFallback 为真仍可下单（走上游兜底）
 * - stockCount > 0                → 有货，显示具体条数
 */
export function isSelfFulfillment(p: Pick<StoreProduct, "fulfillment"> | null | undefined): boolean {
  return p?.fulfillment === "self";
}

/** 商品是否缺货（不可下单）。自营商品有上游兜底时不算缺货。 */
export function isSoldOut(
  p: Pick<StoreProduct, "stockCount" | "fulfillment" | "hasFallback"> | null | undefined,
): boolean {
  if (!p) return false;
  if (p.stockCount !== 0) return false;
  return !(isSelfFulfillment(p) && p.hasFallback);
}

/** 详情弹窗里的库存文案：统一显示真实条数。 */
export function detailStockLabel(
  detail: Pick<GoodsDetail, "stockCount"> | null | undefined,
  product: Pick<StoreProduct, "fulfillment" | "hasFallback"> | null | undefined,
): string {
  const stock = detail?.stockCount;
  if (stock === undefined || stock === null || stock < 0) return "库存 -";
  // 自营有兜底商品时本地 0 条仍可下单，这里只如实报数字，缺货由按钮状态表达
  if (stock === 0) return isSelfFulfillment(product) && product?.hasFallback ? "库存 0（可下单）" : "缺货";
  return `库存 ${stock}`;
}

/**
 * 详情弹窗里的下单按钮是否可用。
 * 上游商品保持原有行为（只要能拿到详情就可下单，库存由后端下单时校验）；
 * 自营商品本地无货且没有兜底商品时前端直接置灰。
 */
export function detailPurchasable(
  detail: Pick<GoodsDetail, "stockCount"> | null | undefined,
  product: Pick<StoreProduct, "fulfillment" | "hasFallback"> | null | undefined,
): boolean {
  if (!detail) return false;
  if (!isSelfFulfillment(product)) return true;
  if ((detail.stockCount ?? 0) > 0) return true;
  return Boolean(product?.hasFallback);
}
