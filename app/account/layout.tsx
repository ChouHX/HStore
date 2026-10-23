import { redirect } from "next/navigation";
import { getSessionUser } from "@/lib/session";
import { DashboardShell, type NavItem } from "@/components/layout/DashboardShell";

export const dynamic = "force-dynamic";

const NAV: NavItem[] = [
  { id: "overview", href: "/account/overview", label: "概览" },
  { id: "recharge", href: "/account/recharge", label: "充值 / 兑换" },
  { id: "orders", href: "/account/orders", label: "我的订单" },
  { id: "ledger", href: "/account/ledger", label: "余额明细" },
];

export default async function AccountLayout({ children }: { children: React.ReactNode }) {
  const user = await getSessionUser();
  if (!user) redirect("/login");
  return (
    <DashboardShell user={user} nav={NAV}>
      {children}
    </DashboardShell>
  );
}
