import { Navigate, Outlet, useLocation } from "react-router-dom";
import { DashboardShell, type NavItem } from "@/components/layout/DashboardShell";
import { PageLoading } from "@/src/components/PageLoading";
import { useSession } from "@/src/hooks/useSession";

const NAV: NavItem[] = [
  { id: "overview", href: "/admin/overview", label: "数据概览" },
  { id: "products", href: "/admin/products", label: "商品与上下架" },
  { id: "catalog", href: "/admin/catalog", label: "商品同步" },
  { id: "pricing", href: "/admin/pricing", label: "定价规则" },
  { id: "users", href: "/admin/users", label: "用户与资产" },
  { id: "redeem", href: "/admin/redeem", label: "兑换码" },
  { id: "announcements", href: "/admin/announcements", label: "公告管理" },
  { id: "logs", href: "/admin/logs", label: "操作日志" },
  { id: "alipay", href: "/admin/alipay", label: "支付宝经营码" },
  { id: "crypto", href: "/admin/recharge", label: "USDT 充值" },
  { id: "settings", href: "/admin/settings", label: "系统设置" },
];

export function AdminLayout() {
  const user = useSession();
  const loc = useLocation();

  if (user === undefined) return <PageLoading />;
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname }} />;
  if (user.role !== "admin") return <Navigate to="/account/overview" replace />;

  return (
    <DashboardShell user={user} nav={NAV}>
      <Outlet />
    </DashboardShell>
  );
}
