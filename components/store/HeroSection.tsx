"use client";

import { Button, Card, Carousel, Tag } from "@arco-design/web-react";
import { IconArrowRight, IconNotification } from "@arco-design/web-react/icon";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { BANNERS, type Banner } from "@/lib/site";
import type { Announcement } from "@/lib/api-types";
import { contentToPlainText } from "@/lib/rich-text";

interface Props {
  announcements: Announcement[];
  /** 有关键弹窗公告时可从公告卡片右上角重新打开 */
  hasPopup?: boolean;
  isLoggedIn?: boolean;
  onOpenNotice?: () => void;
  onSelectBoardKeyword?: (keyword: string) => void;
}

export function HeroSection({
  announcements,
  hasPopup = false,
  isLoggedIn = false,
  onOpenNotice,
  onSelectBoardKeyword,
}: Props) {
  const navigate = useNavigate();
  const [current, setCurrent] = useState(0);

  function handleAction(b: Banner) {
    if (b.action.type === "route") {
      navigate(b.action.href);
      return;
    }
    if (b.action.type === "recharge") {
      navigate(isLoggedIn ? "/account/overview?focus=recharge" : "/login");
      return;
    }
    if (b.action.type === "board") {
      onSelectBoardKeyword?.(b.action.keyword);
      document.getElementById("store-products")?.scrollIntoView({ behavior: "smooth", block: "start" });
    }
  }

  return (
    <section className="mb-4 grid gap-3 lg:grid-cols-3">
      <div className="store-hero relative overflow-hidden rounded-xl shadow-[0_8px_24px_rgba(29,33,41,0.08)] ring-1 ring-black/5 lg:col-span-2">
        <Carousel
          className="h-[188px] sm:h-[252px] lg:h-[268px]"
          autoPlay={{ interval: 5400, hoverToPause: true }}
          animation="slide"
          moveSpeed={520}
          showArrow="hover"
          indicatorType="slider"
          indicatorClassName="store-hero-indicator"
          onChange={(index) => setCurrent(index)}
        >
          {BANNERS.map((b, idx) => (
            <div
              key={b.title}
              className="store-hero-slide relative h-[188px] overflow-hidden sm:h-[252px] lg:h-[268px]"
              style={{ background: b.bg }}
            >
              {/* 细点阵纹理 + 柔光层次 */}
              <div
                className="pointer-events-none absolute inset-0 opacity-[0.12]"
                style={{
                  backgroundImage: "radial-gradient(rgba(255,255,255,0.9) 1px, transparent 1px)",
                  backgroundSize: "20px 20px",
                }}
              />
              <div className="pointer-events-none absolute -right-14 -top-20 h-60 w-60 rounded-full bg-white/15 blur-3xl" />
              <div className="pointer-events-none absolute -bottom-24 right-20 h-52 w-52 rounded-full bg-black/20 blur-3xl" />
              <div className="pointer-events-none absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-black/25 to-transparent" />

              {/* 低资源矢量场景插画：移动端缩放呈现，桌面端完整展示 */}
              <div className="store-hero-art pointer-events-none absolute right-1 top-1/2 -translate-y-1/2 sm:right-5 lg:right-8">
                <img
                  src={b.image}
                  alt=""
                  width={280}
                  height={220}
                  decoding="async"
                  loading={idx === 0 ? "eager" : "lazy"}
                  className="h-auto w-[124px] opacity-90 drop-shadow-[0_12px_24px_rgba(0,0,0,0.22)] sm:w-[216px] sm:opacity-100 lg:w-[244px]"
                />
              </div>

              <div className="store-hero-copy relative z-10 flex h-full max-w-[70%] flex-col justify-center px-5 pb-6 text-white sm:max-w-[62%] sm:px-8 sm:pb-7">
                <span className="w-fit rounded-full border border-white/30 bg-white/15 px-2.5 py-[3px] text-[11px] font-medium tracking-wide backdrop-blur-sm">
                  {b.badge}
                </span>
                <div className="mt-2 text-[18px] font-semibold leading-snug sm:text-[25px]">{b.title}</div>
                <div className="mt-1 line-clamp-2 text-[12px] leading-relaxed text-white/85 sm:text-[13px]">
                  {b.subtitle}
                </div>

                <div className="mt-2.5 hidden flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-white/85 sm:flex">
                  {b.highlights.map((h) => (
                    <span key={h} className="flex items-center gap-1">
                      <span className="h-1.5 w-1.5 rounded-full bg-white/85" />
                      {h}
                    </span>
                  ))}
                </div>

                <div className="mt-3 flex items-center">
                  <Button
                    size="small"
                    className="store-hero-btn"
                    style={{
                      background: "#fff",
                      color: "#1d2129",
                      border: "none",
                      fontWeight: 600,
                      borderRadius: 999,
                      padding: "0 14px",
                    }}
                    onClick={() => handleAction(b)}
                  >
                    {b.ctaLabel}
                    <IconArrowRight style={{ marginLeft: 4 }} />
                  </Button>
                </div>
              </div>
            </div>
          ))}
        </Carousel>

        <div className="pointer-events-none absolute bottom-3 right-4 z-10 rounded-full bg-black/25 px-2 py-[2px] text-[11px] font-medium tabular-nums text-white/90 backdrop-blur-sm">
          {current + 1} / {BANNERS.length}
        </div>
      </div>

      {/* 外层原生 div 控制响应式显隐，避免 Arco 无层 .arco-card 覆盖 Tailwind hidden */}
      <div className="hidden lg:block lg:h-[268px]">
        <Card
          style={{ height: "100%", display: "flex", flexDirection: "column", overflow: "hidden" }}
          title={
            <div className="flex w-full items-center justify-between gap-2">
              <span className="flex items-center gap-2">
                公告
                {announcements.length > 0 && <Tag size="small">{announcements.length}</Tag>}
              </span>
              {hasPopup && onOpenNotice && (
                <Button
                  size="mini"
                  type="text"
                  icon={<IconNotification />}
                  aria-label="查看弹窗公告"
                  title="查看弹窗公告"
                  data-notice-anchor
                  onClick={onOpenNotice}
                />
              )}
            </div>
          }
          bordered={false}
          headerStyle={{ flexShrink: 0 }}
          bodyStyle={{ padding: 12, flex: 1, minHeight: 0, overflowY: "auto", overflowX: "hidden" }}
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
                    {a.content && (
                      <div className="line-clamp-2 text-[11px] text-[#86909c]">
                        {contentToPlainText(a.content)}
                      </div>
                    )}
                  </div>
                </div>
              ))
            )}
          </div>
        </Card>
      </div>
    </section>
  );
}
