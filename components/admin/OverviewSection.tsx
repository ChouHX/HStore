"use client";

import { Button, Card, Grid, Message, Statistic, Tag } from "@arco-design/web-react";
import { Link } from "react-router-dom";
import { useEffect, useState } from "react";
import { formatPrice } from "@/lib/format";

const { Row, Col } = Grid;

interface RecentRecharge {
  id: number;
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
  orders: { total: number; sale: number; cost: number };
  redeem: { batches: number; codes: number; used: number };
  byPlatform: { platform: string; c: number }[];
  recentRecharges: RecentRecharge[];
}

const PLATFORM_LABEL: Record<string, string> = { microsoft: "微软", google: "谷歌", other: "其它" };
const PLATFORM_COLOR: Record<string, string> = { microsoft: "#165dff", google: "#00b42a", other: "#ff7d00" };

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

export function OverviewSection() {
  const [stats, setStats] = useState<Stats | null>(null);

  useEffect(() => {
    fetch("/api/admin/stats")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setStats(j as Stats);
        else Message.error(j.message ?? "加载统计失败");
      })
      .catch(() => Message.error("加载统计失败"));
  }, []);

  if (!stats) {
    return <div className="py-16 text-center text-[13px] text-[#86909c]">加载中…</div>;
  }

  const totalProducts = stats.products.total || 0;
  const platforms = [...stats.byPlatform]
    .filter((p) => p.c > 0)
    .sort((a, b) => b.c - a.c);
  const platformTotal = platforms.reduce((sum, p) => sum + p.c, 0) || totalProducts || 1;

  return (
    <div className="flex flex-col gap-4">
      <Row gutter={[12, 12]}>
        <Col xs={12} md={6}>
          <Card bordered={false}>
            <Statistic title="商品总数" value={stats.products.total} suffix="件" />
            <div className="mt-1 text-[12px] text-[#86909c]">
              在售 {stats.products.enabled} · 下架 {stats.products.disabled}
            </div>
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card bordered={false}>
            <Statistic title="用户数" value={stats.users.total} suffix="人" />
            <div className="mt-1 text-[12px] text-[#86909c]">
              正常 {stats.users.active} · 冻结 {stats.users.frozen}
            </div>
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card bordered={false}>
            <Statistic
              title="销售额"
              value={stats.orders.sale ?? 0}
              precision={2}
              prefix="¥"
              styleValue={{ color: "#f53f3f" }}
            />
            <div className="mt-1 text-[12px] text-[#86909c]">订单 {stats.orders.total} 笔</div>
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card bordered={false}>
            <Statistic title="兑换码已核销" value={stats.redeem.used ?? 0} suffix="张" />
            <div className="mt-1 text-[12px] text-[#86909c]">共 {stats.redeem.codes} 张</div>
          </Card>
        </Col>
      </Row>

      <Row gutter={[12, 12]}>
        <Col xs={24} lg={12}>
          <Card
            title="商品分布"
            bordered={false}
            extra={<span className="text-[12px] text-[#86909c]">共 {totalProducts} 件</span>}
          >
            {totalProducts === 0 ? (
              <div className="py-10 text-center text-[12px] text-[#86909c]">暂无商品数据</div>
            ) : (
              <>
                <div className="flex h-3 w-full overflow-hidden rounded-full bg-[#f2f3f5]">
                  {platforms.map((p) => (
                    <div
                      key={p.platform}
                      title={`${PLATFORM_LABEL[p.platform] ?? p.platform} ${p.c}`}
                      style={{
                        width: `${Math.max(2, (p.c / platformTotal) * 100)}%`,
                        background: PLATFORM_COLOR[p.platform] ?? "#86909c",
                      }}
                    />
                  ))}
                </div>

                <div className="mt-4 flex flex-col gap-2">
                  {platforms.map((p) => {
                    const percent = Math.round((p.c / platformTotal) * 100);
                    return (
                      <div
                        key={p.platform}
                        className="flex items-center justify-between gap-3 rounded border border-[#f2f3f5] px-3 py-2"
                      >
                        <div className="flex min-w-0 items-center gap-2 text-[13px]">
                          <span
                            className="h-2.5 w-2.5 shrink-0 rounded-full"
                            style={{ background: PLATFORM_COLOR[p.platform] ?? "#86909c" }}
                          />
                          <span className="truncate">{PLATFORM_LABEL[p.platform] ?? p.platform}</span>
                        </div>
                        <div className="flex shrink-0 items-center gap-3 text-[12px]">
                          <span className="text-[#4e5969]">{p.c} 件</span>
                          <span className="w-10 text-right text-[#86909c]">{percent}%</span>
                        </div>
                      </div>
                    );
                  })}
                  {platforms.length === 0 && (
                    <div className="text-[12px] text-[#86909c]">暂无平台分布数据</div>
                  )}
                </div>

                <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px] text-[#86909c]">
                  <Tag size="small" color="green">在售 {stats.products.enabled}</Tag>
                  <Tag size="small" color="gray">下架 {stats.products.disabled}</Tag>
                </div>
              </>
            )}
            <div className="mt-4 flex gap-2">
              <Link to="/admin/products"><Button size="small" type="primary">商品管理</Button></Link>
              <Link to="/admin/pricing"><Button size="small">定价规则</Button></Link>
              <Link to="/admin/redeem"><Button size="small">兑换码</Button></Link>
            </div>
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card
            title="最近充值订单"
            bordered={false}
            extra={<Link to="/admin/payment" className="text-[12px] text-[#165dff]">支付设置</Link>}
          >
            <div className="flex flex-col gap-2">
              {stats.recentRecharges?.map((item) => {
                const status = rechargeStatus(item.status);
                const time = item.paidAt ?? item.createdAt;
                return (
                  <div
                    key={`${item.channel}-${item.id}`}
                    className="flex items-center justify-between gap-2 rounded border border-[#e5e6eb] px-2.5 py-2 text-[12px]"
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      <Tag size="small" color={item.channel === "alipay" ? "arcoblue" : "orange"}>
                        {item.channelLabel}
                      </Tag>
                      <span className="truncate text-[#4e5969]">
                        {item.username || "未知用户"}
                      </span>
                      <span className="shrink-0 font-medium text-[#f53f3f]">
                        ¥{formatPrice(item.amountCny)}
                      </span>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      <Tag size="small" color={status.color}>{status.text}</Tag>
                      <span className="text-[11px] text-[#86909c]">
                        {new Date(time).toLocaleString("zh-CN", {
                          month: "2-digit",
                          day: "2-digit",
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </span>
                    </div>
                  </div>
                );
              })}
              {(!stats.recentRecharges || stats.recentRecharges.length === 0) && (
                <div className="py-8 text-center text-[12px] text-[#86909c]">暂无充值订单</div>
              )}
            </div>
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
