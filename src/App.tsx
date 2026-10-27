import { BrowserRouter, Link, Navigate, Route, Routes } from "react-router-dom";

import { AnnouncementsSection } from "@/components/admin/AnnouncementsSection";
import { CatalogSection } from "@/components/admin/CatalogSection";
import { LogsSection } from "@/components/admin/LogsSection";
import { OverviewSection as AdminOverview } from "@/components/admin/OverviewSection";
import { PaymentOrdersSection } from "@/components/admin/PaymentOrdersSection";
import { PaymentSettingsSection } from "@/components/admin/PaymentSettingsSection";
import { ProductsSection } from "@/components/admin/ProductsSection";
import { RedeemSection } from "@/components/admin/RedeemSection";
import { SettingsSection } from "@/components/admin/SettingsSection";
import { UsersSection } from "@/components/admin/UsersSection";
import { MyLedgerSection } from "@/components/account/MyLedgerSection";
import { MyOrdersSection } from "@/components/account/MyOrdersSection";
import { OverviewSection as AccountOverview } from "@/components/account/OverviewSection";
import { SecuritySection } from "@/components/account/SecuritySection";
import { AdminLayout } from "@/src/layouts/AdminLayout";
import { AccountLayout } from "@/src/layouts/AccountLayout";
import { HomePage } from "@/src/pages/HomePage";
import { CheckoutPage } from "@/src/pages/CheckoutPage";
import { LoginPage } from "@/src/pages/LoginPage";
import { OrderDetailPage } from "@/src/pages/OrderDetailPage";
import { OrderQueryPage } from "@/src/pages/OrderQueryPage";
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
        <Route path="/pay/:token" element={<CheckoutPage />} />
        {/* 订单查询：公开页，游客凭订单号或联系方式查 */}
        <Route path="/orders" element={<OrderQueryPage />} />
        {/* 订单详情：游客单凭 token，登录单凭订单号；两个入口共用同一页面 */}
        <Route path="/g/:token" element={<OrderDetailPage />} />
        <Route path="/order/:orderNumber" element={<OrderDetailPage />} />

        <Route path="/account" element={<AccountLayout />}>
          <Route index element={<Navigate to="/account/overview" replace />} />
          <Route path="overview" element={<AccountOverview />} />
          <Route path="recharge" element={<AccountOverview />} />
          <Route path="orders" element={<MyOrdersSection />} />
          <Route path="pickup" element={<Navigate to="/pickup" replace />} />
          <Route path="ledger" element={<MyLedgerSection />} />
          <Route path="security" element={<SecuritySection />} />
        </Route>

        <Route path="/admin" element={<AdminLayout />}>
          <Route index element={<Navigate to="/admin/overview" replace />} />
          <Route path="overview" element={<AdminOverview />} />
          <Route path="products" element={<ProductsSection />} />
          <Route path="catalog" element={<CatalogSection />} />
          <Route path="users" element={<UsersSection />} />
          <Route path="redeem" element={<RedeemSection />} />
          <Route path="announcements" element={<AnnouncementsSection />} />
          <Route path="logs" element={<LogsSection />} />
          <Route path="payment" element={<PaymentSettingsSection />} />
          <Route path="orders" element={<PaymentOrdersSection />} />
          <Route path="settings" element={<SettingsSection />} />
          {/* 旧路径兼容 */}
          <Route path="alipay" element={<Navigate to="/admin/payment?tab=alipay" replace />} />
          <Route path="recharge" element={<Navigate to="/admin/payment?tab=usdt" replace />} />
          <Route path="crypto" element={<Navigate to="/admin/payment?tab=usdt" replace />} />
          <Route path="levels" element={<Navigate to="/admin/users" replace />} />
        </Route>

        <Route path="*" element={<NotFound />} />
      </Routes>
    </BrowserRouter>
  );
}
