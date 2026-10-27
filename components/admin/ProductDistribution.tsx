"use client";

import { Card } from "@arco-design/web-react";

export interface PlatformCount {
  platform: string;
  c: number;
}

interface Props {
  total: number;
  enabled: number;
  disabled: number;
  byPlatform: PlatformCount[];
}

const PLATFORM_LABEL: Record<string, string> = { microsoft: "微软", google: "谷歌", other: "其它" };
const PLATFORM_COLOR: Record<string, string> = {
  microsoft: "#165dff",
  google: "#00b42a",
  other: "#ff7d00",
};

/** 紧凑版商品分布：一条堆叠条 + 单行图例，不再占半屏高度。 */
export function ProductDistribution({ total, enabled, disabled, byPlatform }: Props) {
  const platforms = [...byPlatform].filter((p) => p.c > 0).sort((a, b) => b.c - a.c);
  const platformTotal = platforms.reduce((sum, p) => sum + p.c, 0) || total || 1;

  return (
    <Card
      bordered={false}
      title="商品分布"
      extra={
        <span className="text-[12px] text-[#86909c]">
          共 {total} 件 · 在售 {enabled} · 下架 {disabled}
        </span>
      }
      bodyStyle={{ padding: "12px 16px 14px" }}
    >
      {total === 0 ? (
        <div className="py-2 text-center text-[12px] text-[#86909c]">暂无商品数据</div>
      ) : (
        <>
          <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-[#f2f3f5]">
            {platforms.map((p) => (
              <div
                key={p.platform}
                title={`${PLATFORM_LABEL[p.platform] ?? p.platform} ${p.c} 件`}
                style={{
                  width: `${Math.max(2, (p.c / platformTotal) * 100)}%`,
                  background: PLATFORM_COLOR[p.platform] ?? "#86909c",
                }}
              />
            ))}
          </div>

          <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-2">
            {platforms.map((p) => {
              const percent = Math.round((p.c / platformTotal) * 100);
              return (
                <div key={p.platform} className="flex items-center gap-1.5 text-[12px] leading-none">
                  <span
                    className="h-2 w-2 shrink-0 rounded-full"
                    style={{ background: PLATFORM_COLOR[p.platform] ?? "#86909c" }}
                  />
                  <span className="text-[#4e5969]">{PLATFORM_LABEL[p.platform] ?? p.platform}</span>
                  <span className="font-medium text-[#1d2129]">{p.c}</span>
                  <span className="text-[#86909c]">{percent}%</span>
                </div>
              );
            })}
            {platforms.length === 0 ? (
              <span className="text-[12px] text-[#86909c]">暂无平台分布数据</span>
            ) : null}
          </div>
        </>
      )}
    </Card>
  );
}
