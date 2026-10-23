"use client";

import { Button, Card, Carousel, Tag } from "@arco-design/web-react";
import { BANNERS } from "@/lib/site";
import type { Announcement } from "@/lib/announcements";

export function HeroSection({ announcements }: { announcements: Announcement[] }) {
  return (
    <section className="mb-4 grid gap-3 lg:grid-cols-3">
      <div className="overflow-hidden rounded lg:col-span-2">
        <Carousel
          className="h-[140px] sm:h-[200px]"
          autoPlay={{ interval: 4500 }}
          animation="slide"
          showArrow="hover"
          indicatorType="dot"
        >
          {BANNERS.map((b) => (
            <div key={b.title} className="h-[140px] sm:h-[200px]" style={{ background: b.bg }}>
              {/* 内层负责垂直居中，避免 Arco 切换动画把 display 改成 block 导致文字跳动 */}
              <div className="flex h-full flex-col justify-center px-5 text-white sm:px-8">
                <div className="text-[20px] font-semibold leading-tight sm:text-[22px]">{b.title}</div>
                <div className="mt-0.5 text-[12px] opacity-90 sm:mt-1 sm:text-[13px]">{b.subtitle}</div>
                <div className="mt-2 sm:mt-3">
                  <Button size="small" type="primary" style={{ background: "#fff", color: "#1d2129", border: "none" }}>
                    立即选购
                  </Button>
                </div>
              </div>
            </div>
          ))}
        </Carousel>
      </div>

      <Card
        className="hidden overflow-hidden lg:flex lg:flex-col"
        title="公告"
        bordered={false}
        headerStyle={{ flexShrink: 0 }}
        bodyStyle={{ padding: 12, flex: 1, minHeight: 0, overflowY: "auto", overflowX: "hidden" }}
        style={{ height: 200 }}
      >
        <div className="flex flex-col gap-2.5">
          {announcements.length === 0 ? (
            <div className="text-[12px] text-[#86909c]">暂无公告</div>
          ) : (
            announcements.map((a) => (
              <div key={a.id} className="flex items-start gap-2 text-[12px]">
                <Tag size="small" color="arcoblue" style={{ flexShrink: 0 }}>
                  {new Date(a.createdAt).toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" })}
                </Tag>
                <div className="min-w-0">
                  <div className="text-[#1d2129]">{a.title}</div>
                  {a.content && <div className="text-[11px] text-[#86909c]">{a.content}</div>}
                </div>
              </div>
            ))
          )}
        </div>
      </Card>
    </section>
  );
}
