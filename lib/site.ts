/** 站点品牌名：侧边栏、页头、公开页统一用这个。 */
export const SITE_NAME = "HShop";

export interface Banner {
  title: string;
  subtitle: string;
  bg: string;
}

export const BANNERS: Banner[] = [
  {
    title: "长效微软邮箱 · 全网低价",
    subtitle: "Outlook / Hotmail 双令牌，网页取件，下单秒发",
    bg: "linear-gradient(120deg,#1d4ed8 0%,#3b82f6 100%)",
  },
  {
    title: "谷歌邮箱 · 稳定收信",
    subtitle: "Gmail 老号 / 企业号，支持 API 取件，长期稳定",
    bg: "linear-gradient(120deg,#6d28d9 0%,#a855f7 100%)",
  },
  {
    title: "新用户福利 · 兑换有礼",
    subtitle: "输入兑换码立即到账，会员等级再享专属折扣",
    bg: "linear-gradient(120deg,#ea580c 0%,#f59e0b 100%)",
  },
];
