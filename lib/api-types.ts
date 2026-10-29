/**
 * 前端展示用类型：与 Go 后端 JSON 一一对应，纯类型声明（无运行时依赖）。
 * 迁移到 Vite 后，Node 端的 lib/queries.ts、lib/users.ts、lib/announcements.ts 已删除，
 * 组件统一从这里取类型。
 */
import type { Product } from "./types";

export type AnnouncementType = "normal" | "popup";

export interface Announcement {
  id: number;
  type: AnnouncementType;
  title: string;
  content: string;
  enabled: number;
  sort: number;
  createdAt: number;
  updatedAt: number;
}

export interface AuditLog {
  id: number;
  operatorId: number | null;
  action: string;
  targetType: string;
  targetId: string;
  before: string;
  after: string;
  remark: string;
  createdAt: number;
}

export interface PlatformStats {
  total: number;
  onSale: number;
  minPrice: number | null;
  maxPrice: number | null;
}

export interface StoreSubCategory {
  id: number;
  name: string;
  img: string;
}

export interface StoreBoard {
  id: number;
  name: string;
  img: string;
  total: number;
  minPrice: number | null;
  maxPrice: number | null;
  children: StoreSubCategory[];
}

export interface StoreRoot {
  id: number;
  name: string;
  img: string;
  children: StoreSubCategory[];
  products: Product[];
  stats: PlatformStats;
}

export interface CategoryNode {
  key: string;
  id: number;
  title: string;
  productCount: number;
  children?: CategoryNode[];
}

export interface AdminProduct extends Product {
  rootName: string;
  subName: string;
}

export interface ProductFilterOptions {
  roots: { id: number; name: string; count: number }[];
  subs: { id: number; name: string; rootId: number | null; count: number }[];
  status: { id: number; name: string; count: number }[];
}

/** GET /api/store/home 的响应 */
export interface StoreHome {
  boards: StoreBoard[];
  productCount: number;
  configured: boolean;
  lastSync: number;
  discount: number;
  announcements: Announcement[];
  /** 后端只返回一个当前生效的弹窗公告（可能为 null），不是数组 */
  popup: Announcement | null;
  user: import("./types").User | null;
}
