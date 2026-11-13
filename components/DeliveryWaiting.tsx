import { Spin } from "@arco-design/web-react";
import { useEffect, useState } from "react";

export function DeliveryWaiting({ since, partial = false }: { since: number; partial?: boolean }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [since]);
  const seconds = Math.max(0, Math.floor((now - (since || now)) / 1000));
  const minutes = Math.floor(seconds / 60);
  return (
    <div className="rounded border border-[#bedaff] bg-[#e8f3ff] px-3 py-3 text-[12px] text-[#165dff]">
      <div className="flex flex-wrap items-center gap-2" role="status">
        <Spin size={16} />
        <span className="font-medium">{partial ? "部分商品已发出，剩余商品正在发货" : "正在发货"}</span>
        <span className="font-mono tabular-nums">已等待 {minutes} 分 {String(seconds % 60).padStart(2, "0")} 秒</span>
      </div>
      <div className="mt-2 leading-relaxed text-[#4e5969]">
        {seconds >= 180 ? "发货耗时较长，订单仍在处理中。" : "部分商品发货需要几分钟，请耐心等待。"}
        完成后将自动显示结果，也可稍后通过订单查询查看。请勿重复下单或支付。
      </div>
    </div>
  );
}
