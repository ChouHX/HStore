import { Button, Card, Message, Space, Table, Tabs, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { formatPrice } from "@/lib/format";

interface AlipayOrder {
  id: number;
  uid: number;
  username: string;
  orderNo: string;
  requestedCny: number;
  payableCents: number;
  paymentStatus: string;
  checkoutStatus: string;
  paymentBasis: string;
  receivedCents: number | null;
  alipayOrderNo: string;
  credited: number | null;
  status: "waiting" | "paid" | "expired" | "closed" | "disputed";
  checkoutUrl: string;
  createdAt: number;
  paidAt: number | null;
}

interface CryptoOrder {
  id: number;
  uid: number;
  chain: string;
  requestedCny: number;
  usdtAmount: string;
  creditCny: number;
  status: "waiting" | "paid" | "expired" | "cancelled";
  txid: string | null;
  createdAt: number;
  paidAt: number | null;
}

const ALIPAY_STATUS: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  closed: { color: "gray", text: "已关闭" },
  disputed: { color: "red", text: "支付争议" },
};

const USDT_STATUS: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  cancelled: { color: "red", text: "已取消" },
};

function cents(v: number | null | undefined) {
  return v === null || v === undefined ? "-" : `¥${formatPrice(v / 100)}`;
}

function time(ms: number | null) {
  return ms ? new Date(ms).toLocaleString("zh-CN") : "-";
}

/**
 * 支付订单管理：支付宝经营码订单 + USDT 链上订单统一在这里看。
 * 支付设置页只留配置，流水在那边用弹窗查看。
 */
export function PaymentOrdersSection() {
  const [tab, setTab] = useState<"alipay" | "usdt">("alipay");
  const [alipay, setAlipay] = useState<AlipayOrder[]>([]);
  const [usdt, setUsdt] = useState<CryptoOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [a, c] = await Promise.all([
        fetch("/api/admin/alipay").then((r) => r.json()).catch(() => null),
        fetch("/api/admin/crypto").then((r) => r.json()).catch(() => null),
      ]);
      if (a?.ok) setAlipay(a.orders ?? []);
      if (c?.ok) setUsdt(c.orders ?? []);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function post(path: string, body: unknown, okText: string) {
    setBusy(true);
    try {
      const j = await fetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      if (j.ok) Message.success(okText);
      else Message.error(j.message ?? "操作失败");
      await load();
    } catch {
      Message.error("操作失败");
    } finally {
      setBusy(false);
    }
  }

  const alipayColumns = [
    { title: "ID", dataIndex: "id", width: 60 },
    {
      title: "用户",
      dataIndex: "username",
      width: 100,
      render: (_: unknown, r: AlipayOrder) => r.username || `UID ${r.uid}`,
    },
    {
      title: "订单号",
      dataIndex: "orderNo",
      width: 210,
      render: (_: unknown, r: AlipayOrder) => <span className="font-mono text-[11px]">{r.orderNo}</span>,
    },
    {
      title: "申请 / 应付",
      dataIndex: "requestedCny",
      width: 130,
      render: (_: unknown, r: AlipayOrder) => (
        <div className="text-[12px]">
          <div>¥{formatPrice(r.requestedCny)}</div>
          <div className="text-[#f53f3f]">{cents(r.payableCents)}</div>
        </div>
      ),
    },
    {
      title: "实收 / 入账",
      dataIndex: "credited",
      width: 130,
      render: (_: unknown, r: AlipayOrder) => (
        <div className="text-[12px]">
          <div>{cents(r.receivedCents)}</div>
          <div className="text-[#00b42a]">{r.credited != null ? `¥${formatPrice(r.credited)}` : "-"}</div>
        </div>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (_: unknown, r: AlipayOrder) => (
        <Tag size="small" color={ALIPAY_STATUS[r.status]?.color}>
          {ALIPAY_STATUS[r.status]?.text ?? r.status}
        </Tag>
      ),
    },
    {
      title: "匹配方式",
      dataIndex: "paymentBasis",
      width: 90,
      render: (_: unknown, r: AlipayOrder) => r.paymentBasis,
    },
    {
      title: "支付宝订单号",
      dataIndex: "alipayOrderNo",
      width: 190,
      render: (_: unknown, r: AlipayOrder) => (
        <span className="font-mono text-[11px]">{r.alipayOrderNo || "-"}</span>
      ),
    },
    {
      title: "创建 / 确认",
      dataIndex: "createdAt",
      width: 170,
      render: (_: unknown, r: AlipayOrder) => (
        <div className="text-[11px] text-[#4e5969]">
          <div>{time(r.createdAt)}</div>
          <div className="text-[#00b42a]">{time(r.paidAt)}</div>
        </div>
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 170,
      render: (_: unknown, r: AlipayOrder) => (
        <Space>
          {r.checkoutUrl && (
            <Button size="small" href={r.checkoutUrl} target="_blank" rel="noreferrer">
              收银台
            </Button>
          )}
          {r.status === "waiting" && (
            <Button
              size="small"
              status="danger"
              disabled={busy}
              onClick={() => post("/api/admin/alipay", { action: "closeOrder", orderId: r.id }, "订单已关闭")}
            >
              关闭
            </Button>
          )}
        </Space>
      ),
    },
  ];

  const usdtColumns = [
    { title: "ID", dataIndex: "id", width: 60 },
    { title: "用户", dataIndex: "uid", width: 70 },
    { title: "链", dataIndex: "chain", width: 70 },
    {
      title: "申请 (CNY)",
      dataIndex: "requestedCny",
      width: 100,
      render: (_: unknown, r: CryptoOrder) => `¥${formatPrice(r.requestedCny)}`,
    },
    {
      title: "应付金额",
      dataIndex: "usdtAmount",
      width: 130,
      render: (_: unknown, r: CryptoOrder) => <span className="font-mono text-[12px]">{r.usdtAmount} USDT</span>,
    },
    {
      title: "预计到账",
      dataIndex: "creditCny",
      width: 110,
      render: (_: unknown, r: CryptoOrder) => `¥${formatPrice(r.creditCny)}`,
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (_: unknown, r: CryptoOrder) => (
        <Tag size="small" color={USDT_STATUS[r.status]?.color}>
          {USDT_STATUS[r.status]?.text ?? r.status}
        </Tag>
      ),
    },
    {
      title: "TXID",
      dataIndex: "txid",
      render: (_: unknown, r: CryptoOrder) => (
        <span className="line-clamp-1 max-w-[200px] font-mono text-[11px] text-[#86909c]">{r.txid ?? "-"}</span>
      ),
    },
    {
      title: "创建时间",
      dataIndex: "createdAt",
      width: 170,
      render: (_: unknown, r: CryptoOrder) => time(r.createdAt),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 170,
      render: (_: unknown, r: CryptoOrder) =>
        r.status === "waiting" ? (
          <Space>
            <Button
              size="small"
              disabled={busy}
              onClick={() => post("/api/admin/crypto", { action: "checkOrder", orderId: r.id }, "已检查订单")}
            >
              检查
            </Button>
            <Button
              size="small"
              status="warning"
              disabled={busy}
              onClick={() => post("/api/admin/crypto", { action: "simulate", orderId: r.id }, "已模拟到账（测试）")}
            >
              模拟到账
            </Button>
          </Space>
        ) : (
          <span className="text-[12px] text-[#86909c]">-</span>
        ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2 rounded border border-[#e5e6eb] bg-white p-2">
        <Button size="small" loading={loading} onClick={load}>
          刷新
        </Button>
        {tab === "alipay" ? (
          <>
            <Button
              size="small"
              disabled={busy}
              onClick={() => post("/api/admin/alipay", { action: "expire" }, "已处理过期订单")}
            >
              处理过期订单
            </Button>
            <Button
              size="small"
              disabled={busy}
              onClick={() => post("/api/admin/alipay", { action: "reconcilePending" }, "已重新匹配待处理流水")}
            >
              重新匹配流水
            </Button>
          </>
        ) : (
          <Button
            size="small"
            disabled={busy}
            onClick={() => post("/api/admin/crypto", { action: "checkOrders" }, "已检查进行中订单")}
          >
            检查进行中订单
          </Button>
        )}
        <span className="ml-2 text-[11px] text-[#86909c]">
          支付宝与 USDT 的支付订单都在这里管理；渠道配置在「支付设置」。
        </span>
      </div>

      <Card bordered={false} bodyStyle={{ padding: 12 }}>
        <Tabs activeTab={tab} onChange={(k) => setTab(k as "alipay" | "usdt")} lazyload={false}>
          <Tabs.TabPane key="alipay" title={`支付宝订单（${alipay.length}）`}>
            <Table
              rowKey="id"
              columns={alipayColumns}
              data={alipay}
              loading={loading}
              pagination={{ pageSize: 10 }}
              scroll={{ x: 1300 }}
            />
          </Tabs.TabPane>
          <Tabs.TabPane key="usdt" title={`USDT 订单（${usdt.length}）`}>
            <Table
              rowKey="id"
              columns={usdtColumns}
              data={usdt}
              loading={loading}
              pagination={{ pageSize: 10 }}
              scroll={{ x: 1200 }}
            />
          </Tabs.TabPane>
        </Tabs>
      </Card>
    </div>
  );
}
