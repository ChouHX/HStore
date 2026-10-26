import { Navigate, Outlet, useLocation } from "react-router-dom";
import { DashboardShell, type NavItem } from "@/components/layout/DashboardShell";
import { PageLoading } from "@/src/components/PageLoading";
import { useSession } from "@/src/hooks/useSession";

const NAV: NavItem[] = [
  { id: "overview", href: "/account/overview", label: "概览" },
  { id: "orders", href: "/account/orders", label: "我的订单" },
  { id: "pickup", href: "/pickup", label: "取件" },
  { id: "ledger", href: "/account/ledger", label: "余额明细" },
];

export function AccountLayout() {
  const user = useSession();
  const loc = useLocation();

  if (user === undefined) return <PageLoading />;
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />;

  return (
    <DashboardShell user={user} nav={NAV}>
      <Outlet />
    </DashboardShell>
  );
}
