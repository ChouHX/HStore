import { Radio, Space } from "@arco-design/web-react";
import { useSearchParams } from "react-router-dom";
import { AlipaySection } from "@/components/admin/AlipaySection";
import { CryptoSection } from "@/components/admin/CryptoSection";

/**
 * 支付设置：只有渠道配置。支付订单在「订单管理」页，支付宝流水在配置卡片里弹窗查看。
 */
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
      <div className="flex flex-wrap items-center gap-3 rounded border border-[#e5e6eb] bg-white px-3 py-2">
        <Radio.Group type="button" value={tab} onChange={(v) => changeTab(v as "alipay" | "usdt")}>
          <Radio value="alipay">支付宝设置</Radio>
          <Radio value="usdt">USDT 设置</Radio>
        </Radio.Group>
        <Space>
          <span className="text-[11px] text-[#86909c]">
            这里只做渠道配置；支付订单在「订单管理」页统一查看。
          </span>
        </Space>
      </div>
      {tab === "alipay" ? <AlipaySection /> : <CryptoSection />}
    </div>
  );
}
