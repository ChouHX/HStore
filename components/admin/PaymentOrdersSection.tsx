import { Button, Card, Input, Message, Select, Space, Table, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { formatPrice } from "@/lib/format";

interface AlipayOrder {
  id: number;
  uid: number;
  username: string;
  orderNo: string;
  requestedCny: number;
  payableCents: number;
  paymentBasis: string;
  receivedCents: number | null;
  alipayOrderNo: string;
  credited: number | null;
  status: string;
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
  status: string;
  txid: string | null;
  createdAt: number;
  paidAt: number | null;
}

/** 合并后的一行：支付宝与 USDT 共用同一套列。 */
interface Row {
  key: string;
  channel: "alipay" | "usdt";
  id: number;
  uid: number;
  username: string;
  orderNo: string;
  payText: string;
  paySub: string;
  receiveText: string;
  creditedText: string;
  status: string;
  statusText: string;
  extraLabel: string;
  extraValue: string;
  createdAt: number;
  paidAt: number | null;
  checkoutUrl: string;
}

const STATUS: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  closed: { color: "gray", text: "已关闭" },
  cancelled: { color: "red", text: "已取消" },
  disputed: { color: "red", text: "支付争议" },
};

const STATUS_OPTIONS = [
  { value: "all", label: "全部状态" },
  { value: "waiting", label: "等待支付" },
  { value: "paid", label: "已到账" },
  { value: "expired", label: "已过期" },
  { value: "closed", label: "已关闭" },
  { value: "cancelled", label: "已取消" },
  { value: "disputed", label: "支付争议" },
];

function cents(v: number | null | undefined) {
  return v === null || v === undefined ? "-" : `¥${formatPrice(v / 100)}`;
}

function time(ms: number | null) {
  return ms ? new Date(ms).toLocaleString("zh-CN") : "-";
}

/**
 * 支付订单管理：支付宝经营码订单与 USDT 链上订单合并成一张表，
 * 用渠道 / 状态 / 关键字筛选。
 */
export function PaymentOrdersSection() {
  const [rows, setRows] = useState<Row[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [channel, setChannel] = useState("all");
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [a, c] = await Promise.all([
        fetch("/api/admin/alipay").then((r) => r.json()).catch(() => null),
        fetch("/api/admin/crypto").then((r) => r.json()).catch(() => null),
      ]);
      const merged: Row[] = [];

      for (const o of (a?.ok ? (a.orders as AlipayOrder[]) : []) ?? []) {
        merged.push({
          key: `alipay-${o.id}`,
          channel: "alipay",
          id: o.id,
          uid: o.uid,
          username: o.username,
          orderNo: o.orderNo,
          payText: cents(o.payableCents),
          paySub: `申请 ¥${formatPrice(o.requestedCny)}`,
          receiveText: cents(o.receivedCents),
          creditedText: o.credited != null ? `¥${formatPrice(o.credited)}` : "-",
          status: o.status,
          statusText: STATUS[o.status]?.text ?? o.status,
          extraLabel: "匹配",
          extraValue: o.paymentBasis || "-",
          createdAt: o.createdAt,
          paidAt: o.paidAt,
          checkoutUrl: o.checkoutUrl,
        });
      }

      for (const o of (c?.ok ? (c.orders as CryptoOrder[]) : []) ?? []) {
        merged.push({
          key: `usdt-${o.id}`,
          channel: "usdt",
          id: o.id,
          uid: o.uid,
          username: "",
          orderNo: `#${o.id}`,
          payText: `${o.usdtAmount} USDT`,
          paySub: `申请 ¥${formatPrice(o.requestedCny)} · ${o.chain}`,
          receiveText: "-",
          creditedText: `¥${formatPrice(o.creditCny)}`,
          status: o.status,
          statusText: STATUS[o.status]?.text ?? o.status,
          extraLabel: "TXID",
          extraValue: o.txid || "-",
          createdAt: o.createdAt,
          paidAt: o.paidAt,
          checkoutUrl: "",
        });
      }

      merged.sort((x, y) => y.createdAt - x.createdAt);
      setRows(merged);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function act(path: string, body: unknown, okText: string) {
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

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    return rows.filter((r) => {
      if (channel !== "all" && r.channel !== channel) return false;
      if (status !== "all" && r.status !== status) return false;
      if (!kw) return true;
      return (
        r.orderNo.toLowerCase().includes(kw) ||
        String(r.uid) === kw ||
        (r.username || "").toLowerCase().includes(kw) ||
        r.extraValue.toLowerCase().includes(kw)
      );
    });
  }, [rows, channel, status, keyword]);

  const counts = useMemo(() => {
    let alipay = 0;
    let usdt = 0;
    for (const r of rows) {
      if (r.channel === "alipay") alipay++;
      else usdt++;
    }
    return { alipay, usdt };
  }, [rows]);

  const columns = [
    {
      title: "渠道",
      dataIndex: "channel",
      width: 90,
      render: (_: unknown, r: Row) => (
        <Tag size="small" color={r.channel === "alipay" ? "arcoblue" : "purple"}>
          {r.channel === "alipay" ? "支付宝" : "USDT"}
        </Tag>
      ),
    },
    {
      title: "订单号",
      dataIndex: "orderNo",
      width: 210,
      render: (_: unknown, r: Row) => <span className="font-mono text-[11px]">{r.orderNo}</span>,
    },
    {
      title: "用户",
      dataIndex: "uid",
      width: 110,
      render: (_: unknown, r: Row) => r.username || `UID ${r.uid}`,
    },
    {
      title: "应付",
      dataIndex: "payText",
      width: 150,
      render: (_: unknown, r: Row) => (
        <div className="text-[12px]">
          <div className="text-[#f53f3f]">{r.payText}</div>
          <div className="text-[11px] text-[#86909c]">{r.paySub}</div>
        </div>
      ),
    },
    {
      title: "实收 / 入账",
      dataIndex: "receiveText",
      width: 130,
      render: (_: unknown, r: Row) => (
        <div className="text-[12px]">
          <div>{r.receiveText}</div>
          <div className="text-[#00b42a]">{r.creditedText}</div>
        </div>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (_: unknown, r: Row) => (
        <Tag size="small" color={STATUS[r.status]?.color}>
          {r.statusText}
        </Tag>
      ),
    },
    {
      title: "匹配 / TXID",
      dataIndex: "extraValue",
      render: (_: unknown, r: Row) => (
        <div className="text-[11px] text-[#4e5969]">
          <div className="text-[#86909c]">{r.extraLabel}</div>
          <div className="line-clamp-1 max-w-[220px] break-all font-mono">{r.extraValue}</div>
        </div>
      ),
    },
    {
      title: "创建 / 确认",
      dataIndex: "createdAt",
      width: 170,
      render: (_: unknown, r: Row) => (
        <div className="text-[11px] text-[#4e5969]">
          <div>{time(r.createdAt)}</div>
          <div className="text-[#00b42a]">{time(r.paidAt)}</div>
        </div>
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 180,
      render: (_: unknown, r: Row) => (
        <Space>
          {r.channel === "alipay" && r.checkoutUrl && (
            <Button size="small" href={r.checkoutUrl} target="_blank" rel="noreferrer">
              收银台
            </Button>
          )}
          {r.status === "waiting" && r.channel === "alipay" && (
            <Button
              size="small"
              status="danger"
              disabled={busy}
              onClick={() => act("/api/admin/alipay", { action: "closeOrder", orderId: r.id }, "订单已关闭")}
            >
              关闭
            </Button>
          )}
          {r.status === "waiting" && r.channel === "usdt" && (
            <>
              <Button
                size="small"
                disabled={busy}
                onClick={() => act("/api/admin/crypto", { action: "checkOrder", orderId: r.id }, "已检查订单")}
              >
                检查
              </Button>
              <Button
                size="small"
                status="warning"
                disabled={busy}
                onClick={() => act("/api/admin/crypto", { action: "simulate", orderId: r.id }, "已模拟到账（测试）")}
              >
                模拟到账
              </Button>
            </>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2 rounded border border-[#e5e6eb] bg-white p-2">
        <Button size="small" loading={loading} onClick={load}>
          刷新
        </Button>
        <Select value={channel} onChange={setChannel} style={{ width: 130 }} size="small">
          <Select.Option value="all">全部渠道（{rows.length}）</Select.Option>
          <Select.Option value="alipay">支付宝（{counts.alipay}）</Select.Option>
          <Select.Option value="usdt">USDT（{counts.usdt}）</Select.Option>
        </Select>
        <Select value={status} onChange={setStatus} style={{ width: 130 }} size="small">
          {STATUS_OPTIONS.map((o) => (
            <Select.Option key={o.value} value={o.value}>
              {o.label}
            </Select.Option>
          ))}
        </Select>
        <Input.Search
          allowClear
          size="small"
          value={keyword}
          placeholder="订单号 / UID / 用户名 / TXID"
          style={{ width: 260 }}
          onChange={setKeyword}
        />
        <span className="ml-auto text-[11px] text-[#86909c]">
          支付宝与 USDT 订单合并展示；渠道配置在「支付设置」。
        </span>
      </div>

      <Card bordered={false} bodyStyle={{ padding: 12 }}>
        <Table
          // 筛选变化时重建表格，回到第 1 页，避免停在不存在的页码上
          key={`${channel}-${status}-${keyword}`}
          rowKey="key"
          columns={columns}
          data={filtered}
          loading={loading}
          pagination={{ pageSize: 10, showTotal: true, sizeCanChange: true }}
          scroll={{ x: 1300 }}
        />
      </Card>
    </div>
  );
}
