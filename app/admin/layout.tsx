import { redirect } from "next/navigation";
import { getSessionUser } from "@/lib/session";
import { DashboardShell, type NavItem } from "@/components/layout/DashboardShell";

export const dynamic = "force-dynamic";

const NAV: NavItem[] = [
  { id: "overview", href: "/admin/overview", label: "数据概览" },
  { id: "products", href: "/admin/products", label: "商品与上下架" },
  { id: "catalog", href: "/admin/catalog", label: "商品同步" },
  { id: "pricing", href: "/admin/pricing", label: "定价规则" },
  { id: "users", href: "/admin/users", label: "用户与资产" },
  { id: "redeem", href: "/admin/redeem", label: "兑换码" },
  { id: "announcements", href: "/admin/announcements", label: "公告管理" },
  { id: "logs", href: "/admin/logs", label: "操作日志" },
  { id: "crypto", href: "/admin/recharge", label: "充值设置" },
  { id: "settings", href: "/admin/settings", label: "系统设置" },
];

export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  const user = await getSessionUser();
  if (!user) redirect("/login");
  if (user.role !== "admin") redirect("/account/overview");
  return (
    <DashboardShell user={user} nav={NAV}>
      {children}
    </DashboardShell>
  );
}
