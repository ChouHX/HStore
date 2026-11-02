import type { Product, GoodsDetail } from "@/lib/types";

/**
 * 自营商品的库存展示口径。
 *
 * 自营商品的真实库存条数属于后台信息，前台一律不展示数字，只展示「有货 / 缺货」：
 * - 本地可用库存 > 0                → 有货
 * - 本地为 0 且关联了上游兜底商品    → 有货（下单时走上游采购）
 * - 本地为 0 且没有兜底商品          → 缺货（按钮置灰，不可下单）
 *
 * 后端已经把自营商品的 stockCount 脱敏成「可售标记」（>0 有货，0 缺货），
 * 前端只需按 stockCount 是否为 0 判断，不要依赖具体数值。
 */
export function isSelfFulfillment(p: Pick<Product, "fulfillment"> | null | undefined): boolean {
  return p?.fulfillment === "self";
}

/** 商品是否缺货（不可下单）。上游商品沿用 stockCount === 0 即缺货的口径。 */
export function isSoldOut(p: Pick<Product, "stockCount" | "fulfillment"> | null | undefined): boolean {
  if (!p) return false;
  return p.stockCount === 0;
}

/** 自营商品的可售标记：true 有货 / false 缺货。 */
export function selfStockAvailable(p: Pick<Product, "stockCount" | "fulfillment"> | null | undefined): boolean {
  return !isSoldOut(p);
}

/**
 * 商品卡片上的库存标签文案。
 * 自营商品不返回数字（返回 null），由调用方渲染「有货 / 缺货」标签。
 */
export function stockLabel(p: Pick<Product, "stockCount" | "fulfillment"> | null | undefined): string | null {
  if (!p) return null;
  if (isSelfFulfillment(p)) {
    return isSoldOut(p) ? "缺货" : "有货";
  }
  if (p.stockCount < 0) return "库存 -";
  if (p.stockCount === 0) return "缺货";
  return `库存 ${p.stockCount}`;
}

/** 详情弹窗里的库存文案：自营只给状态，上游沿用数字。 */
export function detailStockLabel(
  detail: Pick<GoodsDetail, "stockCount"> | null | undefined,
  product: Pick<Product, "fulfillment"> | null | undefined,
): string {
  if (isSelfFulfillment(product)) {
    return detail && (detail.stockCount ?? 0) === 0 ? "缺货" : "有货";
  }
  return detail ? `库存 ${detail.stockCount}` : "库存 -";
}

/**
 * 详情弹窗里的下单按钮是否可用。
 * 上游商品保持原有行为（只要能拿到详情就可下单，库存由后端下单时校验）；
 * 自营商品缺货时前端直接置灰。
 */
export function detailPurchasable(
  detail: Pick<GoodsDetail, "stockCount"> | null | undefined,
  product: Pick<Product, "fulfillment"> | null | undefined,
): boolean {
  if (!detail) return false;
  if (!isSelfFulfillment(product)) return true;
  return (detail.stockCount ?? 0) > 0;
}
