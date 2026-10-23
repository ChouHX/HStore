/** 金额展示：去掉多余的 0，例如 31.6000 -> 31.6，0.0300 -> 0.03 */
export function formatPrice(v: number | string | null | undefined): string {
  const n = Number(v ?? 0);
  if (!Number.isFinite(n)) return "0";
  return String(Number(n.toFixed(4)));
}
