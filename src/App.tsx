import { BrowserRouter, Link, Navigate, Route, Routes } from "react-router-dom";

import { AnnouncementsSection } from "@/components/admin/AnnouncementsSection";
import { CatalogSection } from "@/components/admin/CatalogSection";
import { CryptoSection } from "@/components/admin/CryptoSection";
import { LogsSection } from "@/components/admin/LogsSection";
import { OverviewSection as AdminOverview } from "@/components/admin/OverviewSection";
import { PerPaySection } from "@/components/admin/PerPaySection";
import { PricingSection } from "@/components/admin/PricingSection";
import { ProductsSection } from "@/components/admin/ProductsSection";
import { RedeemSection } from "@/components/admin/RedeemSection";
import { SettingsSection } from "@/components/admin/SettingsSection";
import { UsersSection } from "@/components/admin/UsersSection";
import { MyLedgerSection } from "@/components/account/MyLedgerSection";
import { MyOrdersSection } from "@/components/account/MyOrdersSection";
import { OverviewSection as AccountOverview } from "@/components/account/OverviewSection";
import { RechargeSection } from "@/components/account/RechargeSection";
import { AdminLayout } from "@/src/layouts/AdminLayout";
import { AccountLayout } from "@/src/layouts/AccountLayout";
import { HomePage } from "@/src/pages/HomePage";
import { LoginPage } from "@/src/pages/LoginPage";
import { PickupPage } from "@/src/pages/PickupPage";

function NotFound() {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-3">
      <div className="text-[15px] font-medium text-[#1d2129]">页面不存在</div>
      <Link to="/" className="text-[13px] text-[#165dff]">
        返回商城
      </Link>
    </div>
  );
}

export function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/login" element={<LoginPage />} />
        {/* 取件是公开页面，不放在 /account 下，无需登录 */}
        <Route path="/pickup" element={<PickupPage />} />

        <Route path="/account" element={<AccountLayout />}>
          <Route index element={<Navigate to="/account/overview" replace />} />
          <Route path="overview" element={<AccountOverview />} />
          <Route path="recharge" element={<RechargeSection />} />
          <Route path="orders" element={<MyOrdersSection />} />
          <Route path="pickup" element={<Navigate to="/pickup" replace />} />
          <Route path="ledger" element={<MyLedgerSection />} />
        </Route>

        <Route path="/admin" element={<AdminLayout />}>
          <Route index element={<Navigate to="/admin/overview" replace />} />
          <Route path="overview" element={<AdminOverview />} />
          <Route path="products" element={<ProductsSection />} />
          <Route path="catalog" element={<CatalogSection />} />
          <Route path="pricing" element={<PricingSection />} />
          <Route path="users" element={<UsersSection />} />
          <Route path="redeem" element={<RedeemSection />} />
          <Route path="announcements" element={<AnnouncementsSection />} />
          <Route path="logs" element={<LogsSection />} />
          <Route path="perpay" element={<PerPaySection />} />
          <Route path="recharge" element={<CryptoSection />} />
          <Route path="settings" element={<SettingsSection />} />
          {/* 旧路径兼容 */}
          <Route path="crypto" element={<Navigate to="/admin/recharge" replace />} />
          <Route path="levels" element={<Navigate to="/admin/users" replace />} />
        </Route>

        <Route path="*" element={<NotFound />} />
      </Routes>
    </BrowserRouter>
  );
}
