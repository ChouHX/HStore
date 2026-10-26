/** 金额展示：去掉多余的 0，例如 31.6000 -> 31.6，0.0300 -> 0.03 */
export function formatPrice(v: number | string | null | undefined): string {
  const n = Number(v ?? 0);
  if (!Number.isFinite(n)) return "0";
  return String(Number(n.toFixed(4)));
}

/**
 * 支付宝只能支付到「分」，不足 1 分向上取整：0.064 -> 0.07。
 * 先四舍五入再判断是否有余数，避免 0.06*100=6.000000000000001 这类浮点误差被误判成有余数。
 */
export function ceilToCents(v: number): number {
  const raw = v * 100;
  const cents = Math.round(raw);
  const fixed = raw - cents > 1e-6 ? cents + 1 : cents;
  return fixed / 100;
}

