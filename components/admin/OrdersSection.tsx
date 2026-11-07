"use client";

import {
  Button,
  Descriptions,
  Input,
  Message,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { formatPrice } from "@/lib/format";
import { ORDER_STATUS, arcoColor, orderStatusColor } from "@/lib/status";
import type { LocalOrder } from "@/lib/types";

const PAGE_SIZE = 15;

/** 订单来源的展示名。兑换码核销单在库里 source=user，靠是否关联兑换码来区分。 */
const SOURCE_TAG: Record<string, { color: string; text: string }> = {
  admin: { color: "gray", text: "后台" },
  user: { color: "arcoblue", text: "用户" },
  guest: { color: "purple", text: "游客" },
};

const KIND_OPTIONS = [
  { value: "all", label: "全部订单" },
  { value: "redeem", label: "兑换码订单" },
  { value: "normal", label: "非兑换码订单" },
];

const STATUS_OPTIONS = [
  { value: "all", label: "全部状态" },
  { value: "processing", label: "处理中" },
  { value: "done", label: "已完成" },
  { value: "closed", label: "已关闭" },
];

function time(ms: number | null | undefined) {
  return ms ? new Date(ms).toLocaleString("zh-CN") : "-";
}

function Ellipsis({ text, width = 120 }: { text: string; width?: number }) {
  return (
    <div className="truncate" style={{ maxWidth: width }} title={text}>
      {text || "-"}
    </div>
  );
}

interface DetailState {
  order: LocalOrder;
  redeemCode: string;
}

/**
 * 业务订单管理：orders 表里的全部订单。
 *
 * 与「支付单」不是一回事：
 *   - 支付单（支付宝 / USDT）记录的是「钱怎么进来的」，在 PaymentOrdersSection；
 *   - 业务订单记录的是「货怎么出去的」，包含兑换码核销单、自营单、余额购买单、游客直购单。
 * 兑换码核销单不会写 contact，也不扣余额，以前后台完全看不到，这里补上。
 */
export function OrdersSection() {
  const [items, setItems] = useState<LocalOrder[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [kind, setKind] = useState("all");
  const [status, setStatus] = useState("all");
  const [source, setSource] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [loading, setLoading] = useState(false);
  const [detail, setDetail] = useState<DetailState | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const sp = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
      if (kind !== "all") sp.set("kind", kind);
      if (status !== "all") sp.set("status", status);
      if (source !== "all") sp.set("source", source);
      if (keyword) sp.set("keyword", keyword);
      const j = await fetch(`/api/admin/orders?${sp}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as LocalOrder[]);
        setTotal(j.total as number);
      } else Message.error(j.message ?? "加载失败");
    } catch {
      Message.error("加载失败");
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, kind, status, source, keyword]);

  useEffect(() => {
    load();
  }, [load]);

  async function openDetail(orderNumber: string) {
    setDetailLoading(true);
    try {
      const sp = new URLSearchParams({ orderNumber });
      const j = await fetch(`/api/admin/orders/detail?${sp}`).then((r) => r.json());
      if (j.ok) setDetail({ order: j.order as LocalOrder, redeemCode: (j.redeemCode as string) ?? "" });
      else Message.error(j.message ?? "加载详情失败");
    } finally {
      setDetailLoading(false);
    }
  }

  function resetFilters() {
    setKind("all");
    setStatus("all");
    setSource("all");
    setKeyword("");
    setPage(1);
  }

  const columns = [
    {
      title: "订单号",
      dataIndex: "orderNumber",
      width: 200,
      render: (_: unknown, o: LocalOrder) => (
        <div>
          <div className="font-mono text-[12px]">{o.orderNumber}</div>
          {o.upstreamOrderNo && (
            <div className="mt-0.5 truncate text-[11px] text-[#86909c]" title={o.upstreamOrderNo}>
              上游 {o.upstreamOrderNo}
            </div>
          )}
        </div>
      ),
    },
    {
      title: "商品",
      dataIndex: "goodsName",
      width: 200,
      render: (_: unknown, o: LocalOrder) => (
        <div className="text-[12px]">
          <Ellipsis text={o.goodsName} width={190} />
          <div className="mt-0.5 font-mono text-[11px] text-[#86909c]">#{o.goodsId}</div>
        </div>
      ),
    },
    {
      title: "数量",
      dataIndex: "count",
      width: 70,
      align: "right" as const,
    },
    {
      title: "金额 / 成本",
      dataIndex: "saleMoney",
      width: 120,
      align: "right" as const,
      render: (_: unknown, o: LocalOrder) => (
        <div className="text-[12px]">
          <div className="font-semibold text-[#f53f3f]">¥{formatPrice(o.saleMoney || o.money)}</div>
          <div className="text-[11px] text-[#86909c]">成本 ¥{formatPrice(o.costMoney)}</div>
        </div>
      ),
    },
    {
      title: "用户",
      dataIndex: "username",
      width: 120,
      render: (_: unknown, o: LocalOrder) => (
        <div className="text-[12px]">
          <Ellipsis text={o.username || (o.userUid ? `UID ${o.userUid}` : "—")} width={110} />
          {o.userUid ? <div className="mt-0.5 text-[11px] text-[#86909c]">UID {o.userUid}</div> : null}
        </div>
      ),
    },
    {
      title: "来源",
      dataIndex: "source",
      width: 90,
      render: (_: unknown, o: LocalOrder) => {
        const t = SOURCE_TAG[o.source] ?? { color: "gray", text: o.source || "-" };
        return (
          <Space size="mini" direction="vertical">
            <Tag size="small" color={t.color}>
              {t.text}
            </Tag>
            {o.contact ? (
              <Tooltip content={o.contact}>
                <span className="text-[11px] text-[#86909c]">有联系方式</span>
              </Tooltip>
            ) : null}
          </Space>
        );
      },
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 90,
      render: (_: unknown, o: LocalOrder) => (
        <Tag size="small" color={arcoColor(orderStatusColor(o.status))}>
          {ORDER_STATUS[o.status] ?? o.status}
        </Tag>
      ),
    },
    {
      title: "备注",
      dataIndex: "result",
      width: 130,
      render: (_: unknown, o: LocalOrder) => <Ellipsis text={o.result} width={120} />,
    },
    {
      title: "下单时间",
      dataIndex: "createdAt",
      width: 160,
      render: (_: unknown, o: LocalOrder) => (
        <span className="text-[11px] text-[#4e5969]">{time(o.createdAt)}</span>
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 90,
      fixed: "right" as const,
      render: (_: unknown, o: LocalOrder) => (
        <Button size="mini" loading={detailLoading} onClick={() => void openDetail(o.orderNumber)}>
          详情
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space wrap>
        <Input.Search
          allowClear
          value={keyword}
          placeholder="订单号 / 商品 / 用户名 / 联系方式"
          style={{ width: 260 }}
          autoComplete="off"
          onChange={(v) => {
            setKeyword(v);
            if (!v) setPage(1);
          }}
          onSearch={(v) => {
            setKeyword(v);
            setPage(1);
          }}
        />
        <Select
          value={kind}
          style={{ width: 150 }}
          onChange={(v) => {
            setKind(String(v));
            setPage(1);
          }}
        >
          {KIND_OPTIONS.map((o) => (
            <Select.Option key={o.value} value={o.value}>
              {o.label}
            </Select.Option>
          ))}
        </Select>
        <Select
          value={status}
          style={{ width: 130 }}
          onChange={(v) => {
            setStatus(String(v));
            setPage(1);
          }}
        >
          {STATUS_OPTIONS.map((o) => (
            <Select.Option key={o.value} value={o.value}>
              {o.label}
            </Select.Option>
          ))}
        </Select>
        <Select
          value={source}
          style={{ width: 120 }}
          onChange={(v) => {
            setSource(String(v));
            setPage(1);
          }}
        >
          <Select.Option value="all">全部来源</Select.Option>
          <Select.Option value="user">用户</Select.Option>
          <Select.Option value="guest">游客</Select.Option>
          <Select.Option value="admin">后台</Select.Option>
        </Select>
        <Button size="small" onClick={resetFilters}>
          重置筛选
        </Button>
        <Button size="small" loading={loading} onClick={load}>
          刷新
        </Button>
        <span className="ml-auto text-[11px] text-[#86909c]">
          共 {total} 单。兑换码核销单在这里；支付单（支付宝 / USDT）见「支付设置 → 支付订单」。
        </span>
      </Space>

      <Table
        rowKey="orderNumber"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 1300 }}
        pagination={{
          current: page,
          pageSize,
          total,
          showTotal: true,
          sizeCanChange: true,
          onChange: (p, ps) => {
            setPage(p);
            setPageSize(ps);
          },
        }}
      />

      <Modal
        title={detail ? `订单详情 ${detail.order.orderNumber}` : "订单详情"}
        visible={detail !== null}
        onCancel={() => setDetail(null)}
        footer={<Button onClick={() => setDetail(null)}>关闭</Button>}
        style={{ width: 720 }}
        unmountOnExit
      >
        {detail && (
          <div className="flex flex-col gap-3">
            <Descriptions
              column={2}
              colon="："
              labelStyle={{ width: 96, color: "#86909c" }}
              data={[
                { label: "商品编号", value: <span className="font-mono">{detail.order.goodsId}</span> },
                { label: "数量", value: detail.order.count },
                {
                  label: "金额",
                  value: <span className="text-[#f53f3f]">¥{formatPrice(detail.order.saleMoney || detail.order.money)}</span>,
                },
                { label: "成本", value: `¥${formatPrice(detail.order.costMoney)}` },
                { label: "用户", value: detail.order.username || "-" },
                { label: "来源", value: SOURCE_TAG[detail.order.source]?.text ?? detail.order.source },
                { label: "状态", value: ORDER_STATUS[detail.order.status] ?? detail.order.status },
                { label: "联系方式", value: detail.order.contact || "-" },
                { label: "外部单号", value: <span className="font-mono">{detail.order.outerNumber || "-"}</span> },
                {
                  label: "上游单号",
                  value: <span className="break-all font-mono">{detail.order.upstreamOrderNo || "-"}</span>,
                },
                { label: "备注", value: detail.order.result || "-" },
                { label: "兑换码", value: <span className="font-mono">{detail.redeemCode || "-"}</span> },
                { label: "下单时间", value: time(detail.order.createdAt) },
                { label: "更新时间", value: time(detail.order.updatedAt) },
              ]}
            />
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">
                商品名称：{detail.order.goodsName || "-"}
              </div>
              <div className="mb-1 text-[12px] text-[#4e5969]">
                卡密（{detail.order.cards?.length ?? 0} 条）
              </div>
              {detail.order.cards?.length ? (
                <div className="max-h-[260px] overflow-auto rounded border border-[#e5e6eb] bg-[#f7f8fa] p-2 font-mono text-[12px] leading-relaxed select-all">
                  {detail.order.cards.map((c) => (
                    <div key={c.cardId} className="break-all">
                      {[c.cardNo, c.cardPwd].filter(Boolean).join("----")}
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[12px] text-[#86909c]">
                  该订单没有卡密记录（可能上游还在发货，或商品本身不发卡）。
                </div>
              )}
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
