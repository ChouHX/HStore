import type { StoreProduct, GoodsDetail } from "@/lib/types";

/**
 * 商品库存展示口径。
 *
 * 后端下发的 stockCount 就是真实库存条数（自营商品是 self_stock 里 available 的条数，
 * 上游商品是定时同步下来的值），前端直接展示数字：
 * - stockCount < 0                → 库存未知（上游商品还没同步到）
 * - stockCount === 0              → 缺货
 * - stockCount > 0                → 有货，显示具体条数
 *
 * 注：自营商品已与上游完全分离，不再有「绑定兜底商品」的概念，
 * 因此自营商品库存为 0 一律视为缺货。
 */
export function isSelfFulfillment(p: Pick<StoreProduct, "fulfillment"> | null | undefined): boolean {
  return p?.fulfillment === "self";
}

/** 商品是否缺货（不可下单）。 */
export function isSoldOut(
  p: Pick<StoreProduct, "stockCount" | "fulfillment"> | null | undefined,
): boolean {
  if (!p) return false;
  return p.stockCount === 0;
}

/** 详情弹窗里的库存文案：统一显示真实条数。 */
export function detailStockLabel(
  detail: Pick<GoodsDetail, "stockCount"> | null | undefined,
): string {
  const stock = detail?.stockCount;
  if (stock === undefined || stock === null || stock < 0) return "库存 -";
  if (stock === 0) return "缺货";
  return `库存 ${stock}`;
}

/**
 * 详情弹窗里的下单按钮是否可用。
 * 上游商品保持原有行为（只要能拿到详情就可下单，库存由后端下单时校验）；
 * 自营商品本地无货时前端直接置灰。
 */
export function detailPurchasable(
  detail: Pick<GoodsDetail, "stockCount"> | null | undefined,
  product: Pick<StoreProduct, "fulfillment"> | null | undefined,
): boolean {
  if (!detail) return false;
  if (!isSelfFulfillment(product)) return true;
  return (detail.stockCount ?? 0) > 0;
}
