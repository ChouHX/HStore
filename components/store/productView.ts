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

/** 站点 LOGO（public/favicon.svg）。 */
export const SITE_LOGO = "/favicon.svg";

/** 自营板块（分类）ID，与后端 store.SelfCategoryID 保持一致。 */
export const SELF_BOARD_ID = 900001;

/**
 * 板块图标：自营板块没有自己的分类图，统一显示站点 LOGO；
 * 其他板块沿用后台同步进来的分类图。商品图片不受影响，仍用各自的自定义图。
 */
export function boardImg(id: number, img: string | undefined | null): string | null {
  if (id === SELF_BOARD_ID) return SITE_LOGO;
  return catImg(img);
}
