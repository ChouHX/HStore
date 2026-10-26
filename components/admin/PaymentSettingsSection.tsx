"use client";

import { Button } from "@arco-design/web-react";
import { useSearchParams } from "react-router-dom";
import { AlipaySection } from "@/components/admin/AlipaySection";
import { CryptoSection } from "@/components/admin/CryptoSection";

export function PaymentSettingsSection() {
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === "usdt" ? "usdt" : "alipay";

  function changeTab(next: "alipay" | "usdt") {
    const query = new URLSearchParams(params);
    query.set("tab", next);
    setParams(query, { replace: true });
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2 rounded border border-[#e5e6eb] bg-white p-2">
        <Button type={tab === "alipay" ? "primary" : "secondary"} size="small" onClick={() => changeTab("alipay")}>
          支付宝经营码
        </Button>
        <Button type={tab === "usdt" ? "primary" : "secondary"} size="small" onClick={() => changeTab("usdt")}>
          USDT 充值
        </Button>
        <span className="ml-2 text-[11px] text-[#86909c]">
          这里只做渠道配置；支付订单在「订单管理」页统一查看，支付宝流水用弹窗查看。
        </span>
      </div>
      {tab === "alipay" ? <AlipaySection /> : <CryptoSection />}
    </div>
  );
}
