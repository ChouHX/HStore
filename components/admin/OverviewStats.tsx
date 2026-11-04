"use client";

import { Card, Grid, Statistic } from "@arco-design/web-react";
import { formatPrice } from "@/lib/format";

const { Row, Col } = Grid;

/** 与后端一致的统计口径：订单数 / 销售额 / 成本 / 毛利 */
export interface SalesWindow {
  total: number;
  sale: number;
  cost: number;
  profit: number;
}

export const CST_OFFSET_MS = 8 * 60 * 60 * 1000;
export const DAY_MS = 24 * 60 * 60 * 1000;

/** 北京时间当天 00:00:00.000 的时间戳（不依赖浏览器所在时区） */
export function cstDayStart(ms: number): number {
  return Math.floor((ms + CST_OFFSET_MS) / DAY_MS) * DAY_MS - CST_OFFSET_MS;
}

/** 北京时间当天 23:59:59.999 的时间戳 */
export function cstDayEnd(ms: number): number {
  return cstDayStart(ms) + DAY_MS - 1;
}

const DAY_FMT = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

const TIME_FMT = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

/** 2026-11-03（北京时间） */
export function formatCstDay(ms: number): string {
  return DAY_FMT.format(new Date(ms)).replace(/\//g, "-");
}

/** 11-03 08:30（北京时间） */
export function formatCstTime(ms: number): string {
  return TIME_FMT.format(new Date(ms)).replace(/\//g, "-");
}

function num(v: unknown): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

/** 兼容旧版接口（只有 orders 字段）并补齐 profit */
export function normalizeWindow(w: Partial<SalesWindow> | null | undefined): SalesWindow {
  const sale = num(w?.sale);
  const cost = num(w?.cost);
  const total = num(w?.total);
  return {
    total,
    sale,
    cost,
    profit: w?.profit === undefined || w?.profit === null ? Math.round((sale - cost) * 100) / 100 : num(w.profit),
  };
}

function pct(part: number, base: number): string {
  if (!base) return "-";
  return `${((part / base) * 100).toFixed(1)}%`;
}

function avg(sale: number, total: number): string {
  if (!total) return "-";
  return `¥${formatPrice(sale / total)}`;
}

/** 今日四张大卡：销售额 / 订单数 / 成本 / 毛利 */
export function TodaySalesCards({ data }: { data: SalesWindow }) {
  return (
    <Row gutter={[12, 12]}>
      <Col xs={12} md={6}>
        <Card bordered={false}>
          <Statistic
            title="今日销售额"
            value={formatPrice(data.sale)}
            prefix="¥"
            styleValue={{ color: "#f53f3f" }}
          />
          <div className="mt-1 text-[12px] text-[#86909c]">
            订单 {data.total} 笔 · 客单价 {avg(data.sale, data.total)}
          </div>
        </Card>
      </Col>
      <Col xs={12} md={6}>
        <Card bordered={false}>
          <Statistic title="今日订单数" value={data.total} suffix="笔" />
          <div className="mt-1 text-[12px] text-[#86909c]">
            {data.total ? `成本 ${pct(data.cost, data.sale)}` : "暂无成交"}
          </div>
        </Card>
      </Col>
      <Col xs={12} md={6}>
        <Card bordered={false}>
          <Statistic
            title="今日成本"
            value={formatPrice(data.cost)}
            prefix="¥"
            styleValue={{ color: "#4e5969" }}
          />
          <div className="mt-1 text-[12px] text-[#86909c]">占销售额 {pct(data.cost, data.sale)}</div>
        </Card>
      </Col>
      <Col xs={12} md={6}>
        <Card bordered={false}>
          <Statistic
            title="今日毛利"
            value={formatPrice(data.profit)}
            prefix="¥"
            styleValue={{ color: data.profit >= 0 ? "#00b42a" : "#f53f3f" }}
          />
          <div className="mt-1 text-[12px] text-[#86909c]">毛利率 {pct(data.profit, data.sale)}</div>
        </Card>
      </Col>
    </Row>
  );
}

interface BreakdownProps {
  title: string;
  caption?: string;
  data: SalesWindow;
  /** 销售额用红色，全部时间用默认深色 */
  accent?: string;
  /** 无订单时的提示文案 */
  emptyText?: string;
}

/** 一个时间口径下的 4 项指标（用于「所选时间段」「全部时间总额」） */
export function SalesBreakdown({
  title,
  caption,
  data,
  accent = "#1d2129",
  emptyText = "暂无成交订单",
}: BreakdownProps) {
  if (!data.total) {
    return (
      <div className="flex h-full min-h-[92px] flex-col items-center justify-center rounded border border-dashed border-[#e5e6eb] bg-[#fafbfc] px-3 py-2.5">
        <span className="text-[13px] font-medium text-[#4e5969]">{title}</span>
        <span className="mt-1 text-[12px] text-[#86909c]">{emptyText}</span>
      </div>
    );
  }
  return (
    <div className="h-full rounded border border-[#f2f3f5] bg-[#fafbfc] px-3 py-2.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-[13px] font-medium text-[#1d2129]">{title}</span>
        {caption ? <span className="truncate text-[11px] text-[#86909c]">{caption}</span> : null}
      </div>
      <div className="mt-2 grid grid-cols-2 gap-x-3 gap-y-2 sm:grid-cols-4">
        <div>
          <div className="text-[11px] text-[#86909c]">销售额</div>
          <div className="mt-0.5 text-[15px] font-semibold" style={{ color: accent }}>
            ¥{formatPrice(data.sale)}
          </div>
        </div>
        <div>
          <div className="text-[11px] text-[#86909c]">订单数</div>
          <div className="mt-0.5 text-[15px] font-semibold text-[#1d2129]">{data.total}</div>
        </div>
        <div>
          <div className="text-[11px] text-[#86909c]">成本</div>
          <div className="mt-0.5 text-[15px] font-semibold text-[#4e5969]">¥{formatPrice(data.cost)}</div>
        </div>
        <div>
          <div className="text-[11px] text-[#86909c]">毛利</div>
          <div
            className="mt-0.5 text-[15px] font-semibold"
            style={{ color: data.profit >= 0 ? "#00b42a" : "#f53f3f" }}
          >
            ¥{formatPrice(data.profit)}
          </div>
        </div>
      </div>
    </div>
  );
}

interface CompactStatsProps {
  products: { total: number; enabled: number; disabled: number };
  users: { total: number; active: number; frozen: number };
  redeem: { batches: number; codes: number; used: number };
}

/** 商品 / 用户 / 兑换码的存量指标，压缩成一行 */
export function CompactStatsRow({ products, users, redeem }: CompactStatsProps) {
  return (
    <Row gutter={[12, 12]}>
      <Col xs={12} md={8}>
        <Card bordered={false} bodyStyle={{ padding: "12px 16px" }}>
          <div className="text-[12px] text-[#86909c]">商品总数</div>
          <div className="mt-0.5 text-[18px] font-semibold text-[#1d2129]">
            {products.total}
            <span className="ml-1 text-[12px] font-normal text-[#86909c]">件</span>
          </div>
          <div className="mt-1 text-[11px] text-[#86909c]">
            在售 {products.enabled} · 下架 {products.disabled}
          </div>
        </Card>
      </Col>
      <Col xs={12} md={8}>
        <Card bordered={false} bodyStyle={{ padding: "12px 16px" }}>
          <div className="text-[12px] text-[#86909c]">用户数</div>
          <div className="mt-0.5 text-[18px] font-semibold text-[#1d2129]">
            {users.total}
            <span className="ml-1 text-[12px] font-normal text-[#86909c]">人</span>
          </div>
          <div className="mt-1 text-[11px] text-[#86909c]">
            正常 {users.active} · 冻结 {users.frozen}
          </div>
        </Card>
      </Col>
      <Col xs={12} md={8}>
        <Card bordered={false} bodyStyle={{ padding: "12px 16px" }}>
          <div className="text-[12px] text-[#86909c]">兑换码已核销</div>
          <div className="mt-0.5 text-[18px] font-semibold text-[#1d2129]">
            {redeem.used}
            <span className="ml-1 text-[12px] font-normal text-[#86909c]">张</span>
          </div>
          <div className="mt-1 text-[11px] text-[#86909c]">共 {redeem.codes} 张</div>
        </Card>
      </Col>
    </Row>
  );
}
