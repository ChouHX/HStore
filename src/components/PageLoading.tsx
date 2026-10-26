import { Spin } from "@arco-design/web-react";

/** 全屏加载占位 */
export function PageLoading({ tip = "加载中…" }: { tip?: string }) {
  return (
    <div className="flex min-h-[60vh] flex-col items-center justify-center gap-3">
      <Spin />
      <div className="text-[12px] text-[#86909c]">{tip}</div>
    </div>
  );
}
