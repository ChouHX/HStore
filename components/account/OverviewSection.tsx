"use client";
import { formatPrice } from "@/lib/format";

import { Button, Card, Grid, Message, Progress, Space, Statistic, Tag } from "@arco-design/web-react";
import { Link } from "react-router-dom";
import { useEffect, useState } from "react";
import type { Level, User } from "@/lib/types";

const { Row, Col } = Grid;

interface Data {
  user: User;
  levels: Level[];
  orderCount: number;
}

export function OverviewSection() {
  const [data, setData] = useState<Data | null>(null);

  useEffect(() => {
    fetch("/api/me")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setData({ user: j.user, levels: j.levels, orderCount: j.orderCount });
      })
      .catch(() => Message.error("加载失败"));
  }, []);

  if (!data) return <div className="py-16 text-center text-[13px] text-[#86909c]">加载中…</div>;

  const { user, levels, orderCount } = data;
  const next = levels.find((l) => l.threshold > user.totalSpent);
  const remaining = next ? next.threshold - user.totalSpent : 0;
  const percent = next && next.threshold > 0 ? Math.min(100, Math.round((user.totalSpent / next.threshold) * 100)) : 100;

  return (
    <div className="flex flex-col gap-4">
      <Row gutter={[12, 12]} align="stretch">
        <Col xs={12} md={6}>
          <Card bordered={false} style={{ height: "100%" }}>
            <Statistic title="账户余额" value={user.balance} precision={2} prefix="¥" styleValue={{ color: "#f53f3f" }} />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card bordered={false} style={{ height: "100%" }}>
            <div className="text-[13px] text-[#86909c]">会员等级</div>
            <div className="mt-2 flex items-center gap-2">
              <Tag color="arcoblue">{user.levelName || "普通会员"}</Tag>
              {user.levelLocked === 1 && <Tag color="orange">锁定</Tag>}
            </div>
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card bordered={false} style={{ height: "100%" }}>
            <Statistic title="累计消费" value={user.totalSpent} precision={2} prefix="¥" />
          </Card>
        </Col>
        <Col xs={12} md={6}>
          <Card bordered={false} style={{ height: "100%" }}>
            <Statistic title="订单数量" value={orderCount} suffix="笔" />
          </Card>
        </Col>
      </Row>

      <Card title="会员成长" bordered={false}>
        <div className="text-[13px] text-[#4e5969]">
          {next ? `再消费 ¥${formatPrice(remaining)} 即可升级为「${next.name}」` : "已达最高等级"}
        </div>
        <Progress percent={percent} style={{ marginTop: 8 }} />
        <div className="mt-3 flex flex-wrap gap-2">
          {levels.map((l) => (
            <Tag key={l.id} color={l.id === user.levelId ? "arcoblue" : "gray"}>
              {l.name} · 满 ¥{l.threshold}
            </Tag>
          ))}
        </div>
        <Space style={{ marginTop: 16 }}>
          <Link to="/account/recharge"><Button size="small" type="primary">充值 / 兑换</Button></Link>
          <Link to="/account/orders"><Button size="small">查看订单</Button></Link>
          <Link to="/"><Button size="small">返回商城</Button></Link>
        </Space>
      </Card>
    </div>
  );
}
