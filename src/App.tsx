import { BrowserRouter, Link, Navigate, Route, Routes } from "react-router-dom";
import { Suspense, lazy } from "react";

import { HomePage } from "@/src/pages/HomePage";
import { LoginPage } from "@/src/pages/LoginPage";
import { RegisterPage } from "@/src/pages/RegisterPage";
import { OrderQueryPage } from "@/src/pages/OrderQueryPage";
const PickupPage = lazy(() => import("@/src/pages/PickupPage").then((m) => ({ default: m.PickupPage })));
const CheckoutPage = lazy(() => import("@/src/pages/CheckoutPage").then((m) => ({ default: m.CheckoutPage })));
const OrderDetailPage = lazy(() => import("@/src/pages/OrderDetailPage").then((m) => ({ default: m.OrderDetailPage })));

const AdminLayout = lazy(() => import("@/src/layouts/AdminLayout").then((m) => ({ default: m.AdminLayout })));
const AccountLayout = lazy(() => import("@/src/layouts/AccountLayout").then((m) => ({ default: m.AccountLayout })));
const AdminOverview = lazy(() =>
  import("@/components/admin/OverviewSection").then((m) => ({ default: m.OverviewSection })),
);
const ProductsSection = lazy(() =>
  import("@/components/admin/ProductsSection").then((m) => ({ default: m.ProductsSection })),
);
const CatalogSection = lazy(() =>
  import("@/components/admin/CatalogSection").then((m) => ({ default: m.CatalogSection })),
);
const UsersSection = lazy(() =>
  import("@/components/admin/UsersSection").then((m) => ({ default: m.UsersSection })),
);
const RedeemSection = lazy(() =>
  import("@/components/admin/RedeemSection").then((m) => ({ default: m.RedeemSection })),
);
const AnnouncementsSection = lazy(() =>
  import("@/components/admin/AnnouncementsSection").then((m) => ({ default: m.AnnouncementsSection })),
);
const LogsSection = lazy(() => import("@/components/admin/LogsSection").then((m) => ({ default: m.LogsSection })));
const PaymentSettingsSection = lazy(() =>
  import("@/components/admin/PaymentSettingsSection").then((m) => ({ default: m.PaymentSettingsSection })),
);
const PaymentOrdersSection = lazy(() =>
  import("@/components/admin/PaymentOrdersSection").then((m) => ({ default: m.PaymentOrdersSection })),
);
const SettingsSection = lazy(() =>
  import("@/components/admin/SettingsSection").then((m) => ({ default: m.SettingsSection })),
);

const AccountOverview = lazy(() =>
  import("@/components/account/OverviewSection").then((m) => ({ default: m.OverviewSection })),
);
const MyOrdersSection = lazy(() =>
  import("@/components/account/MyOrdersSection").then((m) => ({ default: m.MyOrdersSection })),
);
const MyLedgerSection = lazy(() =>
  import("@/components/account/MyLedgerSection").then((m) => ({ default: m.MyLedgerSection })),
);
const SecuritySection = lazy(() =>
  import("@/components/account/SecuritySection").then((m) => ({ default: m.SecuritySection })),
);

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
      <Suspense
        fallback={
          <div className="flex min-h-screen items-center justify-center text-[13px] text-[#86909c]">加载中…</div>
        }
      >
        <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
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
      </Suspense>
    </BrowserRouter>
  );
}
