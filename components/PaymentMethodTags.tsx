"use client";

import { Tag } from "@arco-design/web-react";

export type PaymentChannel = "balance" | "alipay" | "usdt";

interface Props {
  value: PaymentChannel;
  onChange: (value: PaymentChannel) => void;
  alipayReady: boolean;
  usdtReady: boolean;
  /** 登录用户才显示余额支付 */
  showBalance?: boolean;
  balanceReady?: boolean;
}

const OPTIONS: {
  code: PaymentChannel;
  label: string;
  logo?: string;
  color: string;
}[] = [
  { code: "balance", label: "余额支付", color: "green" },
  { code: "alipay", label: "支付宝", logo: "/pay-alipay.svg", color: "blue" },
  { code: "usdt", label: "USDT", logo: "/pay-usdt.svg", color: "purple" },
];

export function PaymentMethodTags({
  value,
  onChange,
  alipayReady,
  usdtReady,
  showBalance = false,
  balanceReady = true,
}: Props) {
  const readyOf = (code: PaymentChannel) => {
    if (code === "balance") return showBalance && balanceReady;
    return code === "alipay" ? alipayReady : usdtReady;
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      {OPTIONS.filter((option) => option.code !== "balance" || showBalance).map((option) => {
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
            icon={
              option.logo ? (
                <img src={option.logo} alt="" className="h-5 w-5" />
              ) : (
                <span className="flex h-5 w-5 items-center justify-center rounded-full bg-[#00b42a] text-[12px] font-semibold text-white">
                  ¥
                </span>
              )
            }
            style={{
              height: 38,
              padding: "0 12px",
              fontSize: 13,
              opacity: ready ? 1 : 0.45,
              cursor: ready ? "pointer" : "not-allowed",
            }}
            title={ready ? option.label : `${option.label} 不可用`}
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
