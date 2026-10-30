"use client";

import { Button, Card, Grid, Message, Statistic, Table, Tag } from "@arco-design/web-react";
import { Link } from "react-router-dom";
import { useEffect, useState } from "react";
import { formatPrice } from "@/lib/format";
import { ProductDistribution } from "./ProductDistribution";
import { UpstreamStatusCard } from "./UpstreamStatusCard";

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
  orders: { total: number; sale: number; cost: number };
  redeem: { batches: number; codes: number; used: number };
  byPlatform: { platform: string; c: number }[];
  recentRecharges: RecentRecharge[];
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
        <Col span={24}>
          <UpstreamStatusCard />
        </Col>
      </Row>

      <Row gutter={[12, 12]}>
        <Col span={24}>
          <ProductDistribution
            total={totalProducts}
            enabled={stats.products.enabled}
            disabled={stats.products.disabled}
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
                        {new Date(item.paidAt ?? item.createdAt).toLocaleString("zh-CN", {
                          month: "2-digit",
                          day: "2-digit",
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
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
