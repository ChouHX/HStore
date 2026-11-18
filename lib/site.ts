/** 站点品牌名：侧边栏、页头、公开页统一用这个。 */
export const SITE_NAME = "HShop";

export type BannerAction =
  | { type: "board"; keyword: string }
  | { type: "route"; href: string }
  | { type: "recharge" };

/** 首页轮播单张 banner 的数据结构。 */
export interface Banner {
  title: string;
  subtitle: string;
  /** CSS background，直接贴到幻灯片容器上 */
  bg: string;
  badge: string;
  /** 低资源矢量场景插画（public/banners/*.svg，约 2~3KB，长效缓存） */
  image: string;
  highlights: string[];
  /** 每张幻灯片配备唯一、不重复的场景按钮 */
  ctaLabel: string;
  action: BannerAction;
}

export const BANNERS: Banner[] = [
  {
    title: "长效微软邮箱 · 双令牌秒发",
    subtitle: "Outlook / Hotmail 精选号源，支持网页一键取件与 OAuth2 稳定收信",
    bg: "radial-gradient(circle at 82% 28%, rgba(147,197,253,0.36), transparent 48%), linear-gradient(125deg, #0b2265 0%, #1d4ed8 54%, #38bdf8 100%)",
    badge: "热销爆款",
    image: "/banners/hero-microsoft.svg",
    highlights: ["OAuth2 双令牌", "网页秒收验证码", "当天售后质保"],
    ctaLabel: "浏览微软专区",
    action: { type: "board", keyword: "微软" },
  },
  {
    title: "谷歌 Gmail · 纯净稳定直登",
    subtitle: "老号 / 耐用号 / 企业域多规格可选，适配海外业务注册与长期收信",
    bg: "radial-gradient(circle at 80% 24%, rgba(196,181,253,0.34), transparent 46%), linear-gradient(125deg, #1e1b4b 0%, #4f46e5 54%, #0ea5e9 100%)",
    badge: "高权重号源",
    image: "/banners/hero-google.svg",
    highlights: ["多地区纯净号源", "长期稳定可用", "支持批量阶梯价"],
    ctaLabel: "选购谷歌专区",
    action: { type: "board", keyword: "谷歌" },
  },
  {
    title: "兑换中心 · 输码即刻出卡",
    subtitle: "支持余额充值码与商品兑换码免登录极速核销，自动发卡零等待",
    bg: "radial-gradient(circle at 82% 25%, rgba(253,230,138,0.32), transparent 46%), linear-gradient(125deg, #7c2d12 0%, #ea580c 54%, #f59e0b 100%)",
    badge: "极速核销",
    image: "/banners/hero-redeem.svg",
    highlights: ["免登录直接兑换", "余额实时到账", "一键导出 TXT"],
    ctaLabel: "前往兑换中心",
    action: { type: "route", href: "/redeem" },
  },
  {
    title: "多渠道秒结 · 会员享专属折扣",
    subtitle: "支持支付宝扫码、USDT 多链转账与余额结算，累计消费自动晋升会员等级",
    bg: "radial-gradient(circle at 80% 25%, rgba(110,231,183,0.3), transparent 48%), linear-gradient(125deg, #064e3b 0%, #059669 52%, #14b8a6 100%)",
    badge: "7×24h 自动发卡",
    image: "/banners/hero-delivery.svg",
    highlights: ["支付宝 / USDT", "等级折上折", "随时查单补提"],
    ctaLabel: "充值享会员折扣",
    action: { type: "recharge" },
  },
];
