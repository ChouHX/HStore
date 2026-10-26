"use client";

import { Button, Card, Grid, Message, Progress, Statistic, Tag } from "@arco-design/web-react";
import { Link } from "react-router-dom";
import { useEffect, useState } from "react";
import type { AuditLog } from "@/lib/api-types";

const { Row, Col } = Grid;

interface Stats {
  products: { total: number; enabled: number; disabled: number };
  users: { total: number; active: number; frozen: number; balance: number };
  orders: { total: number; sale: number; cost: number };
  redeem: { batches: number; codes: number; used: number };
  byPlatform: { platform: string; c: number }[];
}

const PLATFORM_LABEL: Record<string, string> = { microsoft: "微软", google: "谷歌", other: "其它" };

export function OverviewSection() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [logs, setLogs] = useState<AuditLog[]>([]);

  useEffect(() => {
    fetch("/api/admin/stats")
      .then((r) => r.json())
      .then((j) => { if (j.ok) setStats(j as Stats); })
      .catch(() => Message.error("加载统计失败"));
    fetch("/api/admin/audit?page=1&pageSize=6")
      .then((r) => r.json())
      .then((j) => { if (j.ok) setLogs(j.items as AuditLog[]); })
      .catch(() => {});
  }, []);

  if (!stats) {
    return <div className="py-16 text-center text-[13px] text-[#86909c]">加载中…</div>;
  }

  const totalProducts = stats.products.total || 1;

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
          <Card title="平台商品分布" bordered={false}>
            {stats.byPlatform.map((p) => (
              <div key={p.platform} className="mb-3">
                <div className="mb-1 flex justify-between text-[12px]">
                  <span>{PLATFORM_LABEL[p.platform] ?? p.platform}</span>
                  <span className="text-[#86909c]">
                    {p.c} · {Math.round((p.c / totalProducts) * 100)}%
                  </span>
                </div>
                <Progress percent={Math.round((p.c / totalProducts) * 100)} size="small" showText={false} />
              </div>
            ))}
            <div className="mt-2 flex gap-2">
              <Link to="/admin/products"><Button size="small" type="primary">商品管理</Button></Link>
              <Link to="/admin/pricing"><Button size="small">定价规则</Button></Link>
              <Link to="/admin/redeem"><Button size="small">兑换码</Button></Link>
            </div>
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="最近操作" bordered={false}>
            <div className="flex flex-col gap-2">
              {logs.map((l) => (
                <div key={l.id} className="flex items-center justify-between gap-2 rounded border border-[#e5e6eb] px-2.5 py-1.5 text-[12px]">
                  <div className="flex min-w-0 items-center gap-2">
                    <Tag size="small" color="arcoblue">{l.action}</Tag>
                    <span className="truncate text-[#86909c]">
                      {l.targetType}{l.targetId ? `#${l.targetId}` : ""} {l.remark}
                    </span>
                  </div>
                  <span className="shrink-0 text-[11px] text-[#86909c]">
                    {new Date(l.createdAt).toLocaleTimeString("zh-CN")}
                  </span>
                </div>
              ))}
              {logs.length === 0 && <div className="text-[12px] text-[#86909c]">暂无操作记录</div>}
            </div>
            <div className="mt-3 flex gap-2">
              <Link to="/admin/logs"><Button size="small">全部日志</Button></Link>
              <Link to="/admin/users"><Button size="small">用户与资产</Button></Link>
            </div>
          </Card>
        </Col>
      </Row>
    </div>
  );
}
