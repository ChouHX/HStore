import { Button, Card, Descriptions, Input, Message, Modal, Select, Space, Table, Tag } from "@arco-design/web-react";
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
  expiresAt: number;
  createdAt: number;
  paidAt: number | null;
  kind?: string;
  relatedOrderNo?: string;
  lastError?: string;
}

interface CryptoOrder {
  id: number;
  /** 系统生成的支付单号（US 开头） */
  orderNo: string;
  uid: number;
  chain: string;
  address: string;
  requestedCny: number;
  usdtAmount: string;
  creditCny: number;
  status: string;
  txid: string | null;
  receivedUsdt: string | null;
  expiresAt: number;
  createdAt: number;
  paidAt: number | null;
  kind?: string;
  relatedOrderNo?: string;
}

/** 合并后的一行：支付宝与 USDT 共用同一套列。 */
interface Row {
  key: string;
  kind: "recharge" | "purchase";
  channel: "alipay" | "usdt";
  id: number;
  uid: number;
  username: string;
  orderNo: string;
  /** 游客购物对应的业务订单号（用户端显示的订单号） */
  relatedOrderNo?: string;
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
  raw: AlipayOrder | CryptoOrder;
}

const STATUS: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  closed: { color: "gray", text: "已关闭" },
  cancelled: { color: "red", text: "已取消" },
  disputed: { color: "red", text: "支付争议" },
};

const KIND: Record<string, { color: string; text: string }> = {
  recharge: { color: "arcoblue", text: "充值" },
  purchase: { color: "green", text: "购物" },
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

function time(ms: number | null | undefined) {
  return ms ? new Date(ms).toLocaleString("zh-CN") : "-";
}

/** 超长内容截断，鼠标悬停看全量，避免表格换行。 */
function Ellipsis({ text, width = 110 }: { text: string; width?: number }) {
  return (
    <div className="truncate" style={{ maxWidth: width }} title={text}>
      {text}
    </div>
  );
}

/**
 * 支付订单管理：支付宝经营码订单与 USDT 链上订单合并成一张表，
 * 用类型 / 渠道 / 状态 / 关键字筛选。
 */
export function PaymentOrdersSection() {
  const [rows, setRows] = useState<Row[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [kind, setKind] = useState("all");
  const [channel, setChannel] = useState("all");
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [detail, setDetail] = useState<Row | null>(null);

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
          kind: o.kind === "purchase" ? "purchase" : "recharge",
          channel: "alipay",
          id: o.id,
          uid: o.uid,
          username: o.username,
          orderNo: o.orderNo,
          relatedOrderNo: o.relatedOrderNo,
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
          raw: o,
        });
      }

      for (const o of (c?.ok ? (c.orders as CryptoOrder[]) : []) ?? []) {
        merged.push({
          key: `usdt-${o.id}`,
          kind: o.kind === "purchase" ? "purchase" : "recharge",
          channel: "usdt",
          id: o.id,
          uid: o.uid,
          username: "",
          orderNo: o.orderNo || `#${o.id}`,
          relatedOrderNo: o.relatedOrderNo,
          payText: `${o.usdtAmount} USDT`,
          paySub: `申请 ¥${formatPrice(o.requestedCny)} · ${o.chain}`,
          receiveText: o.receivedUsdt ? `${o.receivedUsdt} USDT` : "-",
          creditedText: `¥${formatPrice(o.creditCny)}`,
          status: o.status,
          statusText: STATUS[o.status]?.text ?? o.status,
          extraLabel: "TXID",
          extraValue: o.txid || "-",
          createdAt: o.createdAt,
          paidAt: o.paidAt,
          checkoutUrl: "",
          raw: o,
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
      if (kind !== "all" && r.kind !== kind) return false;
      if (channel !== "all" && r.channel !== channel) return false;
      if (status !== "all" && r.status !== status) return false;
      if (!kw) return true;
      return (
        r.orderNo.toLowerCase().includes(kw) ||
        (r.relatedOrderNo || "").toLowerCase().includes(kw) ||
        String(r.uid) === kw ||
        (r.username || "").toLowerCase().includes(kw) ||
        r.extraValue.toLowerCase().includes(kw)
      );
    });
  }, [rows, kind, channel, status, keyword]);

  const counts = useMemo(() => {
    const c = { recharge: 0, purchase: 0, alipay: 0, usdt: 0 };
    for (const r of rows) {
      c[r.kind]++;
      c[r.channel]++;
    }
    return c;
  }, [rows]);

  const columns = [
    {
      title: "类型",
      dataIndex: "kind",
      width: 80,
      render: (_: unknown, r: Row) => (
        <Tag size="small" color={KIND[r.kind]?.color}>
          {KIND[r.kind]?.text}
        </Tag>
      ),
    },
    {
      title: "渠道",
      dataIndex: "channel",
      width: 86,
      render: (_: unknown, r: Row) => (
        <Tag size="small" color={r.channel === "alipay" ? "arcoblue" : "purple"}>
          {r.channel === "alipay" ? "支付宝" : "USDT"}
        </Tag>
      ),
    },
    {
      title: "订单号",
      dataIndex: "orderNo",
      width: 220,
      render: (_: unknown, r: Row) =>
        r.relatedOrderNo ? (
          <div>
            <Ellipsis text={r.relatedOrderNo} width={205} />
            <div className="mt-0.5 truncate text-[11px] text-[#86909c]" title={r.orderNo} style={{ maxWidth: 205 }}>
              支付单 {r.orderNo}
            </div>
          </div>
        ) : (
          <Ellipsis text={r.orderNo} width={190} />
        ),
    },
    {
      title: "用户",
      dataIndex: "uid",
      width: 110,
      render: (_: unknown, r: Row) => <Ellipsis text={r.username || `UID ${r.uid}`} width={96} />,
    },
    {
      title: "应付",
      dataIndex: "payText",
      width: 140,
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
      width: 150,
      render: (_: unknown, r: Row) => (
        <div className="text-[11px] text-[#4e5969]">
          <div className="text-[#86909c]">{r.extraLabel}</div>
          <Ellipsis text={r.extraValue} width={140} />
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
      width: 210,
      render: (_: unknown, r: Row) => (
        <Space>
          <Button size="small" onClick={() => setDetail(r)}>
            详情
          </Button>
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

  const alipayDetail = detail?.channel === "alipay" ? (detail.raw as AlipayOrder) : null;
  const usdtDetail = detail?.channel === "usdt" ? (detail.raw as CryptoOrder) : null;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2 rounded border border-[#e5e6eb] bg-white p-2">
        <Button size="small" loading={loading} onClick={load}>
          刷新
        </Button>
        <Select value={kind} onChange={setKind} style={{ width: 130 }} size="small">
          <Select.Option value="all">全部类型（{rows.length}）</Select.Option>
          <Select.Option value="recharge">充值（{counts.recharge}）</Select.Option>
          <Select.Option value="purchase">购物（{counts.purchase}）</Select.Option>
        </Select>
        <Select value={channel} onChange={setChannel} style={{ width: 130 }} size="small">
          <Select.Option value="all">全部渠道</Select.Option>
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
          style={{ width: 240 }}
          onChange={setKeyword}
        />
        <span className="ml-auto text-[11px] text-[#86909c]">
          充值 = 余额充值单；购物 = 游客直购单。渠道配置在「支付设置」。
        </span>
      </div>

      <Card bordered={false} bodyStyle={{ padding: 12 }}>
        <Table
          // 筛选变化时重建表格，回到第 1 页，避免停在不存在的页码上
          key={`${kind}-${channel}-${status}-${keyword}`}
          rowKey="key"
          columns={columns}
          data={filtered}
          loading={loading}
          pagination={{ pageSize: 10, showTotal: true, sizeCanChange: true }}
          scroll={{ x: 1420 }}
        />
      </Card>

      <Modal
        title={detail ? `${KIND[detail.kind].text}订单详情 · ${detail.channel === "alipay" ? "支付宝" : "USDT"}` : ""}
        visible={detail !== null}
        onCancel={() => setDetail(null)}
        footer={<Button onClick={() => setDetail(null)}>关闭</Button>}
        style={{ width: 640 }}
        unmountOnExit
      >
        {detail && (
          <Descriptions
            column={1}
            colon="："
            labelStyle={{ width: 120, color: "#86909c" }}
            data={[
              { label: "类型", value: KIND[detail.kind].text },
              { label: "渠道", value: detail.channel === "alipay" ? "支付宝经营码" : "USDT 链上转账" },
              ...(detail.relatedOrderNo
                ? [{ label: "业务订单号", value: <span className="font-mono">{detail.relatedOrderNo}</span> }]
                : []),
              { label: "支付单号", value: <span className="font-mono">{detail.orderNo}</span> },
              { label: "用户", value: detail.username || `UID ${detail.uid}` },
              {
                label: "金额",
                value: (
                  <span>
                    {detail.payText}
                    <span className="ml-2 text-[12px] text-[#86909c]">{detail.paySub}</span>
                  </span>
                ),
              },
              { label: "实收 / 入账", value: `${detail.receiveText} / ${detail.creditedText}` },
              { label: "状态", value: <Tag size="small" color={STATUS[detail.status]?.color}>{detail.statusText}</Tag> },
              ...(alipayDetail
                ? [
                    { label: "支付宝订单号", value: <span className="font-mono">{alipayDetail.alipayOrderNo || "-"}</span> },
                    { label: "匹配方式", value: alipayDetail.paymentBasis || "-" },
                    {
                      label: "收银台",
                      value: alipayDetail.checkoutUrl ? (
                        <a href={alipayDetail.checkoutUrl} target="_blank" rel="noreferrer" className="break-all">
                          {alipayDetail.checkoutUrl}
                        </a>
                      ) : (
                        "-"
                      ),
                    },
                  ]
                : []),
              ...(usdtDetail
                ? [
                    { label: "链", value: usdtDetail.chain },
                    { label: "收款地址", value: <span className="break-all font-mono">{usdtDetail.address}</span> },
                    { label: "TXID", value: <span className="break-all font-mono">{usdtDetail.txid || "-"}</span> },
                  ]
                : []),
              { label: "创建时间", value: time(detail.createdAt) },
              { label: "确认时间", value: time(detail.paidAt) },
              {
                label: "过期时间",
                value: time(alipayDetail ? alipayDetail.expiresAt : usdtDetail?.expiresAt),
              },
            ]}
          />
        )}
      </Modal>
    </div>
  );
}
