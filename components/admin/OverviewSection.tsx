"use client";

import { Button, Card, DatePicker, Grid, Message, Table, Tag } from "@arco-design/web-react";
import { IconRefresh } from "@arco-design/web-react/icon";
import dayjs from "dayjs";
import type { Dayjs } from "dayjs";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { formatPrice } from "@/lib/format";
import { ProductDistribution } from "./ProductDistribution";
import { UpstreamStatusCard } from "./UpstreamStatusCard";
import {
  CompactStatsRow,
  DAY_MS,
  SalesBreakdown,
  TodaySalesCards,
  cstDayEnd,
  cstDayStart,
  formatCstDay,
  formatCstTime,
  normalizeWindow,
  type SalesWindow,
} from "./OverviewStats";

const { Row, Col } = Grid;

interface RecentRecharge {
  key: string;
  id: number;
  orderNo: string;
  channel: "alipay" | "usdt";
  channelLabel: string;
  username: string;
  amountCny: number;
  status: string;
  createdAt: number;
  paidAt: number | null;
}

interface Stats {
  products: { total: number; enabled: number; disabled: number };
  users: { total: number; active: number; frozen: number; balance: number };
  today: SalesWindow;
  range: SalesWindow & { from: number; to: number };
  allTime: SalesWindow;
  /** 兼容旧接口：等价于 allTime，仅在缺字段时兜底 */
  orders?: { total: number; sale: number; cost: number };
  redeem: { batches: number; codes: number; used: number };
  byPlatform: { platform: string; c: number }[];
  recentRecharges: RecentRecharge[];
  generatedAt?: number;
}

/** 一次统计请求的时间窗口（epoch ms 闭区间；from=0 表示不限起始） */
interface Window {
  from: number;
  to: number;
}

const RECHARGE_STATUS: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  closed: { color: "gray", text: "已关闭" },
  cancelled: { color: "red", text: "已取消" },
  disputed: { color: "red", text: "支付争议" },
};

function rechargeStatus(status: string) {
  return RECHARGE_STATUS[status] ?? { color: "gray", text: status };
}

/** 今日窗口（北京时间 00:00 → 现在） */
function todayWindow(): Window {
  const now = Date.now();
  return { from: cstDayStart(now), to: now };
}

/** 最近 n 天（含今天，按北京时间的自然日对齐） */
function lastDaysWindow(n: number): Window {
  const now = Date.now();
  return { from: cstDayStart(now - (n - 1) * DAY_MS), to: cstDayEnd(now) };
}

/** 全部时间：from=0 表示不限起点，to 取当前时间（服务端也会再夹一次） */
function allTimeWindow(): Window {
  return { from: 0, to: Date.now() };
}

const SHORTCUTS = [
  {
    text: "今日",
    value: () => {
      const w = todayWindow();
      return [dayjs(w.from), dayjs(w.to)];
    },
  },
  {
    text: "近 7 天",
    value: () => {
      const w = lastDaysWindow(7);
      return [dayjs(w.from), dayjs(w.to)];
    },
  },
  {
    text: "近 30 天",
    value: () => {
      const w = lastDaysWindow(30);
      return [dayjs(w.from), dayjs(w.to)];
    },
  },
  // 起点用 1970-01-01，onChange 里识别为「全部时间」
  { text: "全部时间", value: () => [dayjs(0), dayjs(Date.now())] },
];

function isAllTime(w: Window): boolean {
  return w.from <= 0;
}

export function OverviewSection() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [win, setWin] = useState<Window>(todayWindow);
  // 同一个区间重复选时要能再次请求，用自增计数当触发器
  const [tick, setTick] = useState(0);
  // RangePicker 受控值，与请求窗口保持一致；「全部时间」时为空（输入框留空）
  const [picked, setPicked] = useState<Dayjs[] | undefined>(() => {
    const w = todayWindow();
    return [dayjs(w.from), dayjs(w.to)];
  });

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    const qs = new URLSearchParams({ from: String(win.from), to: String(win.to) });
    fetch(`/api/admin/stats?${qs.toString()}`)
      .then((r) => r.json())
      .then((j) => {
        if (cancelled) return;
        if (j?.ok) {
          setStats(j as Stats);
          setFailed(false);
        } else {
          setFailed(true);
          Message.error(j?.message ?? "加载统计失败");
        }
      })
      .catch(() => {
        if (cancelled) return;
        setFailed(true);
        Message.error("加载统计失败");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [win, tick]);

  const handlePick = useCallback((dates: (Dayjs | undefined)[] | undefined) => {
    if (!dates || !dates[0] || !dates[1]) {
      // 清空（allowClear）或非法选择：回到「今日」
      const w = todayWindow();
      setPicked([dayjs(w.from), dayjs(w.to)]);
      setWin(w);
      return;
    }
    const start = dates[0].valueOf();
    const end = dates[1].valueOf();
    if (start <= 0) {
      // 「全部时间」快捷项：输入框留空，用下方标签说明口径
      setPicked(undefined);
      setWin(allTimeWindow());
      return;
    }
    setPicked([dates[0], dates[1]]);
    setWin({ from: cstDayStart(start), to: cstDayEnd(end) });
  }, []);

  const allTime = isAllTime(win);
  const today = useMemo(() => normalizeWindow(stats?.today), [stats]);
  const rangeStat = useMemo(() => normalizeWindow(stats?.range), [stats]);
  const allStat = useMemo(() => normalizeWindow(stats?.allTime ?? stats?.orders), [stats]);
  // 以服务端回显的实际窗口为准（会被夹到 now），避免前后端口径不一致
  const effectiveFrom = stats?.range?.from ?? win.from;
  const effectiveTo = stats?.range?.to ?? win.to;

  const products = stats?.products ?? { total: 0, enabled: 0, disabled: 0 };
  const users = stats?.users ?? { total: 0, active: 0, frozen: 0, balance: 0 };
  const redeem = stats?.redeem ?? { batches: 0, codes: 0, used: 0 };

  if (!stats) {
    if (failed) {
      return (
        <Card bordered={false}>
          <div className="py-12 text-center text-[13px] text-[#86909c]">
            统计加载失败
            <Button className="ml-3" size="small" onClick={() => setTick((t) => t + 1)}>
              重试
            </Button>
          </div>
        </Card>
      );
    }
    return <div className="py-16 text-center text-[13px] text-[#86909c]">加载中…</div>;
  }

  return (
    <div className="flex flex-col gap-4">
      {/* 大卡片只统计当日，不受下面的时间筛选影响 */}
      <div style={{ opacity: loading ? 0.6 : 1, transition: "opacity .15s" }}>
        <TodaySalesCards data={today} />
      </div>

      {/* 时间筛选 + 区间统计 / 全部时间总额 */}
      <Card
        bordered={false}
        title="区间统计"
        extra={
          <div className="flex flex-wrap items-center justify-end gap-2">
            <DatePicker.RangePicker
              size="small"
              style={{ width: 250 }}
              format="YYYY-MM-DD"
              placeholder={["开始日期", "结束日期"]}
              allowClear
              value={picked}
              shortcuts={SHORTCUTS}
              // 未来没有销售数据，服务端也会夹到当前时间，这里直接禁掉避免前后端口径不一致
              disabledDate={(current) => current.isAfter(dayjs(), "day")}
              onChange={(_, dates) => handlePick(dates as (Dayjs | undefined)[])}
            />
            <Button
              size="small"
              icon={<IconRefresh />}
              loading={loading}
              onClick={() => setTick((t) => t + 1)}
            >
              刷新
            </Button>
          </div>
        }
      >
        <div className="mb-3 flex flex-wrap items-center gap-2 text-[11px] text-[#86909c]">
          {allTime ? (
            <Tag size="small" color="orange">
              全部时间
            </Tag>
          ) : (
            <Tag size="small" color="arcoblue">
              {formatCstDay(effectiveFrom)} ~ {formatCstDay(effectiveTo)}
            </Tag>
          )}
          <span>
            {loading
              ? "加载中…"
              : `订单 ${rangeStat.total} 笔 · 北京时间口径 · 今日大卡片不受此处筛选影响`}
          </span>
        </div>
        <Row gutter={[12, 12]}>
          <Col xs={24} md={12}>
            <SalesBreakdown
              title="所选时间段"
              caption={allTime ? "不限起始" : `订单 ${rangeStat.total} 笔`}
              data={rangeStat}
              accent="#f53f3f"
              emptyText="该时间段暂无成交订单"
            />
          </Col>
          <Col xs={24} md={12}>
            <SalesBreakdown
              title="全部时间总额"
              caption="不受筛选影响"
              data={allStat}
              accent="#1d2129"
              emptyText="暂无销售数据"
            />
          </Col>
        </Row>
      </Card>

      {/* 商品 / 用户 / 兑换码：保留，压缩成一行 */}
      <CompactStatsRow products={products} users={users} redeem={redeem} />

      <Row gutter={[12, 12]}>
        <Col span={24}>
          <UpstreamStatusCard />
        </Col>
      </Row>

      <Row gutter={[12, 12]}>
        <Col span={24}>
          <ProductDistribution
            total={products.total}
            enabled={products.enabled}
            disabled={products.disabled}
            byPlatform={stats.byPlatform}
          />
        </Col>
      </Row>

      <Row gutter={[12, 12]}>
        <Col span={24}>
          <Card
            title="最近充值订单"
            bordered={false}
            extra={<Link to="/admin/payment" className="text-[12px] text-[#165dff]">支付设置</Link>}
          >
            {stats.recentRecharges?.length ? (
              <Table
                rowKey="key"
                size="small"
                pagination={false}
                data={stats.recentRecharges}
                scroll={{ x: 620 }}
                columns={[
                  {
                    title: "渠道",
                    dataIndex: "channelLabel",
                    width: 90,
                    render: (_: unknown, item: RecentRecharge) => (
                      <Tag size="small" color={item.channel === "alipay" ? "arcoblue" : "orange"}>
                        {item.channelLabel}
                      </Tag>
                    ),
                  },
                  {
                    title: "用户",
                    dataIndex: "username",
                    width: 100,
                    render: (_: unknown, item: RecentRecharge) => item.username || "未知用户",
                  },
                  {
                    title: "金额",
                    dataIndex: "amountCny",
                    width: 100,
                    align: "right" as const,
                    render: (_: unknown, item: RecentRecharge) => (
                      <span className="font-medium text-[#f53f3f]">¥{formatPrice(item.amountCny)}</span>
                    ),
                  },
                  {
                    title: "状态",
                    dataIndex: "status",
                    width: 100,
                    render: (_: unknown, item: RecentRecharge) => {
                      const status = rechargeStatus(item.status);
                      return <Tag size="small" color={status.color}>{status.text}</Tag>;
                    },
                  },
                  {
                    title: "时间",
                    dataIndex: "paidAt",
                    width: 150,
                    render: (_: unknown, item: RecentRecharge) => (
                      <span className="text-[11px] text-[#86909c]">
                        {formatCstTime(item.paidAt ?? item.createdAt)}
                      </span>
                    ),
                  },
                  {
                    title: "订单号",
                    dataIndex: "orderNo",
                    width: 180,
                    render: (_: unknown, item: RecentRecharge) => (
                      <span className="font-mono text-[11px]">{item.orderNo}</span>
                    ),
                  },
                ]}
              />
            ) : (
              <div className="py-10 text-center text-[12px] text-[#86909c]">暂无充值订单</div>
            )}
            <div className="mt-3 flex gap-2">
              <Link to="/admin/payment?tab=alipay"><Button size="small">支付宝订单</Button></Link>
              <Link to="/admin/payment?tab=usdt"><Button size="small">USDT 订单</Button></Link>
            </div>
          </Card>
        </Col>
      </Row>
    </div>
  );
}
