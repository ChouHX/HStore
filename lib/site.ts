/** 站点品牌名：侧边栏、页头、公开页统一用这个。 */
export const SITE_NAME = "HShop";

export interface Banner {
  title: string;
  subtitle: string;
  bg: string;
}

export interface Banner {
  title: string;
  subtitle: string;
  bg: string;
  badge: string;
  icon: "mail" | "bolt" | "gift";
  highlights: string[];
}

export const BANNERS: Banner[] = [
  {
    title: "长效微软邮箱 · 全网低价",
    subtitle: "Outlook / Hotmail 双令牌，网页取件，下单秒发",
    bg: "linear-gradient(120deg,#0f2f8f 0%,#2563eb 55%,#60a5fa 100%)",
    badge: "热销爆款",
    icon: "mail",
    highlights: ["网页取件", "双令牌", "当天质保"],
  },
  {
    title: "谷歌邮箱 · 稳定收信",
    subtitle: "Gmail 老号 / 企业号，支持 API 取件，长期稳定",
    bg: "linear-gradient(120deg,#4c1d95 0%,#7c3aed 55%,#c084fc 100%)",
    badge: "稳定收信",
    icon: "bolt",
    highlights: ["API 取件", "长期可用", "高权重"],
  },
  {
    title: "新用户福利 · 兑换有礼",
    subtitle: "输入兑换码立即到账，会员等级再享专属折扣",
    bg: "linear-gradient(120deg,#9a3412 0%,#ea580c 55%,#fbbf24 100%)",
    badge: "新人专享",
    icon: "gift",
    highlights: ["兑换到账", "等级折扣", "余额支付"],
  },
];
