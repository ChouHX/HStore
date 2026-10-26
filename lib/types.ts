export type Platform = "microsoft" | "google" | "other";

export interface RemoteCategory {
  id: number;
  name: string;
  img?: string;
  children?: RemoteCategory[];
  brands?: { id: number; name: string }[];
}

export interface RemoteProduct {
  goodsId: number;
  name: string;
  faceValue: number;
  salesPrice: number;
  goodsType: number;
  warrantyDays?: number;
  warrantyTime?: number;
  warrantyUnit?: number;
  status: number;
  multiple: number;
  isRepeat: number;
  isRefOrder: number;
  isRefMoney: number;
  skuType: number;
  imgUrl: string;
  dirIds?: number[] | null;
  stockCount?: number;
}

export interface Category {
  id: number;
  name: string;
  img: string;
  parentId: number | null;
  platform: Platform;
  updatedAt: number;
}

export interface Product {
  goodsId: number;
  name: string;
  faceValue: number;
  /** 上游成本/原价 */
  costPrice: number;
  /** 定价规则计算后的销售价 */
  finalPrice: number;
  /** 上游销售价（兼容保留） */
  salesPrice: number;
  goodsType: number;
  warrantyDays: number;
  status: number;
  multiple: number;
  isRepeat: number;
  isRefOrder: number;
  isRefMoney: number;
  skuType: number;
  imgUrl: string;
  dirIds: number[];
  platform: Platform;
  /** 本站上架状态：1 上架，0 下架 */
  enabled: number;
  /** 手动锁定价（null 表示未覆盖） */
  manualPrice: number | null;
  priceLocked: number;
  /** 库存数量，-1 表示未知 */
  stockCount: number;
  /** 板块（顶级分类）ID */
  rootCategoryId: number | null;
  /** 自定义信息（按商品编号） */
  customName: string | null;
  customDesc: string | null;
  customImg: string | null;
  updatedAt: number;
}

export interface SyncResult {
  ok: boolean;
  source: "remote" | "demo";
  categories: number;
  products: number;
  microsoft: number;
  google: number;
  message: string;
}

export interface RechargeTemplateOption {
  name: string;
  value: string;
}

export interface RechargeTemplate {
  type: number;
  title: string;
  placeholder?: string;
  required: number;
  regex?: string;
  options?: RechargeTemplateOption[];
}

export interface GoodsDetail extends RemoteProduct {
  minQuantity: number;
  maxQuantity: number;
  stockCount: number;
  goodsDescribe?: string;
  goodsDetail?: string;
  buyNotice?: string;
  limitBalance?: number;
  rechargeTemplates?: RechargeTemplate[];
  goodsTags?: { name: string; color: string };
  serviceTags?: { name: string; image?: string; color?: string }[];
}

export interface Card {
  cardId: string;
  cardNo?: string;
  cardPwd?: string;
  status: number;
  cancelRequest: number;
  result?: string;
}

export interface OrderDetail {
  orderNumber: string;
  outerNumber?: string;
  status: number;
  count: number;
  money: number;
  retMoney: number;
  result: string;
  startCount?: string;
  nowCount?: string;
  endCount?: string;
  cards?: Card[];
}

export interface CreateOrderResult {
  orderNumber: string;
  cards: Card[];
}

/** 本地订单（含卡券） */
export interface LocalOrder {
  orderNumber: string;
  outerNumber: string;
  goodsId: number;
  goodsName: string;
  count: number;
  money: number;
  status: number;
  result: string;
  platform: Platform;
  createdAt: number;
  updatedAt: number;
  userUid: number | null;
  username: string;
  saleMoney: number;
  costMoney: number;
  source: "admin" | "user" | "redeem";
  settled: number;
  cards: Card[];
}

// ===== 用户 / 资产 =====
export interface User {
  uid: number;
  username: string;
  nickname: string;
  phone: string;
  email: string;
  role: "user" | "admin";
  status: "active" | "frozen";
  balance: number;
  totalSpent: number;
  levelId: number | null;
  levelName: string;
  levelLocked: number;
  createdAt: number;
  updatedAt: number;
}

export interface Level {
  id: number;
  name: string;
  threshold: number;
  discount: number;
  sort: number;
}

export interface LedgerEntry {
  id: number;
  uid: number;
  username: string;
  type: "recharge" | "consume" | "redeem" | "admin_adjust";
  amount: number;
  balanceBefore: number;
  balanceAfter: number;
  orderNumber: string;
  code: string;
  operatorId: number | null;
  remark: string;
  createdAt: number;
}

// ===== 定价 =====
export type PricingFormula = "fixed_add" | "fixed_sub" | "ratio" | "mixed";
export type Rounding =
  | "round2"
  | "ceil2"
  | "floor2"
  | "round3"
  | "round4"
  | "ceil4"
  | "floor4"
  | "ceil1"
  | "round0";

export interface PricingRule {
  id: number;
  name: string;
  scope: "global" | "platform" | "category" | "product";
  scopeId: string;
  formulaType: PricingFormula;
  amount: number;
  percent: number;
  multiplier: number;
  fixed: number;
  rounding: Rounding;
  priority: number;
  enabled: number;
  createdAt: number;
  updatedAt: number;
}

// ===== 兑换码 =====
export interface RedeemCode {
  id: number;
  code: string;
  /** 面值码金额（商品码为 0） */
  faceValue: number;
  /** 非空 = 商品码，核销后自动下单取件 */
  goodsId: number | null;
  goodsName: string;
  goodsCount: number;
  /** 核销后关联的订单号 */
  orderNumber: string;
  status: "unused" | "redeeming" | "used" | "voided";
  usedBy: number | null;
  usedAt: number | null;
  startAt: number | null;
  endAt: number | null;
  createdAt: number;
}
