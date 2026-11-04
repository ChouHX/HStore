"use client";

import { Button, Card, Carousel, Tag } from "@arco-design/web-react";
import { IconArrowRight, IconEmail, IconGift, IconNotification, IconThunderbolt } from "@arco-design/web-react/icon";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { BANNERS } from "@/lib/site";
import type { Announcement } from "@/lib/api-types";
import { contentToPlainText } from "@/lib/rich-text";

const ICONS = { mail: IconEmail, bolt: IconThunderbolt, gift: IconGift };

interface Props {
  announcements: Announcement[];
  /** 有关键弹窗公告时可从公告卡片右上角重新打开 */
  hasPopup?: boolean;
  onOpenNotice?: () => void;
}

export function HeroSection({ announcements, hasPopup = false, onOpenNotice }: Props) {
  const navigate = useNavigate();
  // 右下角「第几张 / 共几张」，跟 Arco 内部索引保持同步
  const [current, setCurrent] = useState(0);

  function scrollToProducts() {
    document.getElementById("store-products")?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  return (
    <section className="mb-4 grid gap-3 lg:grid-cols-3">
      <div className="store-hero relative overflow-hidden rounded-xl shadow-[0_8px_24px_rgba(29,33,41,0.08)] ring-1 ring-black/5 lg:col-span-2">
        <Carousel
          className="h-[188px] sm:h-[252px] lg:h-[268px]"
          autoPlay={{ interval: 5200, hoverToPause: true }}
          animation="slide"
          moveSpeed={520}
          showArrow="hover"
          indicatorType="slider"
          indicatorClassName="store-hero-indicator"
          onChange={(index) => setCurrent(index)}
        >
          {BANNERS.map((b) => {
            const Icon = ICONS[b.icon] ?? IconEmail;
            return (
              <div
                key={b.title}
                className="store-hero-slide relative h-[188px] overflow-hidden sm:h-[252px] lg:h-[268px]"
                style={{ background: b.bg }}
              >
                {/* 细网格 + 两道柔光，让纯渐变背景不显得平 */}
                <div
                  className="pointer-events-none absolute inset-0 opacity-[0.13]"
                  style={{
                    backgroundImage:
                      "linear-gradient(#fff 1px, transparent 1px), linear-gradient(90deg, #fff 1px, transparent 1px)",
                    backgroundSize: "30px 30px",
                  }}
                />
                <div className="pointer-events-none absolute -right-16 -top-24 h-64 w-64 rounded-full bg-white/20 blur-3xl" />
                <div className="pointer-events-none absolute -bottom-28 right-24 h-56 w-56 rounded-full bg-black/20 blur-3xl" />
                {/* 底部压暗：浅色渐变尾部也能托住指示器和张数角标 */}
                <div className="pointer-events-none absolute inset-x-0 bottom-0 h-24 bg-gradient-to-t from-black/25 to-transparent" />

                <div className="pointer-events-none absolute right-6 top-1/2 hidden -translate-y-1/2 md:block lg:right-8">
                  <div className="relative flex h-[84px] w-[84px] rotate-3 items-center justify-center rounded-3xl border border-white/25 bg-white/10 backdrop-blur-sm lg:h-[104px] lg:w-[104px]">
                    <Icon style={{ fontSize: 44, color: "#fff" }} />
                    <span className="absolute -right-2 -top-2 h-3 w-3 rounded-full bg-white/70" />
                  </div>
                </div>

                <div className="store-hero-copy relative flex h-full flex-col justify-center px-5 pb-6 text-white sm:px-8 sm:pb-7">
                  <span className="w-fit rounded-full border border-white/30 bg-white/15 px-2.5 py-[3px] text-[11px] font-medium tracking-wide backdrop-blur-sm">
                    {b.badge}
                  </span>
                  <div className="mt-2 text-[19px] font-semibold leading-snug sm:text-[26px]">{b.title}</div>
                  <div className="mt-1 max-w-[92%] text-[12px] leading-relaxed text-white/85 sm:max-w-[80%] sm:text-[13px]">
                    {b.subtitle}
                  </div>

                  <div className="mt-2.5 hidden items-center gap-3 text-[11px] text-white/80 sm:flex">
                    {b.highlights.map((h) => (
                      <span key={h} className="flex items-center gap-1">
                        <span className="h-1 w-1 rounded-full bg-white/80" />
                        {h}
                      </span>
                    ))}
                  </div>

                  <div className="mt-3 flex items-center gap-2">
                    <Button
                      size="small"
                      className="store-hero-btn"
                      style={{ background: "#fff", color: "#1d2129", border: "none", fontWeight: 500 }}
                      onClick={scrollToProducts}
                    >
                      立即选购
                      <IconArrowRight style={{ marginLeft: 4 }} />
                    </Button>
                    <Button
                      size="small"
                      className="store-hero-btn-ghost"
                      style={{ background: "transparent", color: "#fff", borderColor: "rgba(255,255,255,0.55)" }}
                      onClick={() => navigate("/pickup")}
                    >
                      微软取件
                    </Button>
                  </div>
                </div>
              </div>
            );
          })}
        </Carousel>
        {/* 张数角标：指示器在底部居中，这里放右下角，两者互不遮挡 */}
        <div className="pointer-events-none absolute bottom-3 right-4 z-10 rounded-full bg-black/25 px-2 py-[2px] text-[11px] font-medium tabular-nums text-white/90 backdrop-blur-sm">
          {current + 1} / {BANNERS.length}
        </div>
      </div>

      <Card
        className="hidden overflow-hidden lg:flex lg:h-full lg:flex-col"
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
    </section>
  );
}
