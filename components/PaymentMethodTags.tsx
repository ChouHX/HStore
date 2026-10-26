"use client";

import { Tag } from "@arco-design/web-react";

export type PaymentChannel = "alipay" | "usdt";

interface Props {
  value: PaymentChannel;
  onChange: (value: PaymentChannel) => void;
  alipayReady: boolean;
  usdtReady: boolean;
}

const OPTIONS: { code: PaymentChannel; label: string; logo: string; color: string }[] = [
  { code: "alipay", label: "支付宝", logo: "/pay-alipay.svg", color: "blue" },
  { code: "usdt", label: "USDT", logo: "/pay-usdt.svg", color: "purple" },
];

export function PaymentMethodTags({ value, onChange, alipayReady, usdtReady }: Props) {
  const readyOf = (code: PaymentChannel) => (code === "alipay" ? alipayReady : usdtReady);
  return (
    <div className="flex flex-wrap items-center gap-2">
      {OPTIONS.map((option) => {
        const ready = readyOf(option.code);
        const checked = value === option.code;
        return (
          <Tag
            key={option.code}
            checkable
            checked={checked}
            size="large"
            bordered
            color={checked ? option.color : undefined}
            icon={<img src={option.logo} alt="" className="h-5 w-5" />}
            style={{
              height: 38,
              padding: "0 12px",
              fontSize: 13,
              opacity: ready ? 1 : 0.45,
              cursor: ready ? "pointer" : "not-allowed",
            }}
            title={ready ? option.label : `${option.label} 未配置`}
            onCheck={() => {
              if (ready) onChange(option.code);
            }}
          >
            {option.label}
          </Tag>
        );
      })}
    </div>
  );
}
