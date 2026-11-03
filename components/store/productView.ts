import type { Product } from "@/lib/types";

/**
 * 商品展示层的公共取值逻辑（商城列表、兑换中心等页面共用）。
 *
 * 后台已把上游外链图片换成本地 `/api/img/` 代理地址，因此这里只放行非 http 开头的地址，
 * 避免浏览器直接去请求上游域名。
 */
export function catImg(img: string | undefined | null): string | null {
  if (!img) return null;
  return img.startsWith("http") ? null : img;
}

export function cleanBoardName(name: string): string {
  const cleaned = name.replace(/[（(][^（）()]*[）)]\s*$/, "").trim();
  return cleaned || name;
}

export function productTitle(p: Product): string {
  return p.customName || p.name;
}

export function productImg(p: Product): string | null {
  return catImg(p.customImg || p.imgUrl);
}
