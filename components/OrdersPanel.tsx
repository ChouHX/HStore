"use client";
import { formatPrice } from "@/lib/format";

import {
  Button,
  Descriptions,
  Drawer,
  Input,
  Message,
  Select,
  Space,
  Table,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import type { LocalOrder } from "@/lib/types";
import { CARD_STATUS, ORDER_STATUS, arcoColor, cardStatusColor, orderStatusColor } from "@/lib/status";

const PAGE_SIZE = 10;

export function OrdersPanel() {
  const [items, setItems] = useState<LocalOrder[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [loading, setLoading] = useState(false);

  const [detail, setDetail] = useState<LocalOrder | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const sp = new URLSearchParams({ page: String(page), pageSize: String(pageSize), status });
      if (keyword) sp.set("keyword", keyword);
      const j = await fetch(`/api/order/list?${sp}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as LocalOrder[]);
        setTotal(j.total as number);
      } else Message.error(j.message);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, status, keyword]);

  useEffect(() => {
    load();
  }, [load]);

  function updateRow(order: LocalOrder) {
    setItems((prev) => prev.map((o) => (o.orderNumber === order.orderNumber ? order : o)));
  }

  async function openDetail(orderNumber: string) {
    setDrawerOpen(true);
    setDetailLoading(true);
    setDetail(items.find((o) => o.orderNumber === orderNumber) ?? null);
    try {
      const j = await fetch("/api/order/detail", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ orderNumber }),
      }).then((r) => r.json());
      if (j.order) {
        setDetail(j.order as LocalOrder);
        updateRow(j.order as LocalOrder);
      }
      if (!j.ok) Message.error(j.message ?? "刷新失败");
    } finally {
      setDetailLoading(false);
    }
  }

  async function refreshDetail() {
    if (!detail) return;
    setDetailLoading(true);
    try {
      const j = await fetch("/api/order/detail", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ orderNumber: detail.orderNumber }),
      }).then((r) => r.json());
      if (j.order) {
        setDetail(j.order as LocalOrder);
        updateRow(j.order as LocalOrder);
        Message.success("已刷新");
      } else Message.error(j.message ?? "刷新失败");
    } finally {
      setDetailLoading(false);
    }
  }

  async function voidCard(cardId: string) {
    const j = await fetch("/api/card/void", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ cardId }),
    }).then((r) => r.json());
    if (j.ok) {
      if (j.order) {
        setDetail(j.order as LocalOrder);
        updateRow(j.order as LocalOrder);
      }
      Message.success("卡券作废成功");
    } else Message.error(j.message ?? "作废失败");
  }

  function downloadTxt() {
    if (!detail) return;
    if (detail.cards.length === 0) {
      Message.warning("暂无卡券可导出");
      return;
    }
    // 一行一个数据：卡号----密码（无密码则只输出卡号）
    const lines = detail.cards.map((c) =>
      c.cardPwd ? `${c.cardNo ?? ""}----${c.cardPwd}` : `${c.cardNo ?? ""}`,
    );
    const blob = new Blob([lines.join("\n")], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `order-${detail.orderNumber}.txt`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    Message.success("已导出 TXT");
  }

  const columns = [
    {
      title: "订单号",
      dataIndex: "orderNumber",
      width: 170,
      render: (_: unknown, o: LocalOrder) => (
        <span className="font-mono text-[12px]">{o.orderNumber}</span>
      ),
    },
    {
      title: "商品",
      dataIndex: "goodsName",
      render: (_: unknown, o: LocalOrder) => (
        <span className="line-clamp-1 max-w-[260px]" title={o.goodsName}>
          {o.goodsName}
        </span>
      ),
    },
    { title: "数量", dataIndex: "count", width: 60, align: "right" as const },
    {
      title: "金额",
      dataIndex: "money",
      width: 90,
      align: "right" as const,
      render: (_: unknown, o: LocalOrder) => (
        <span className="font-semibold text-[#f53f3f]">¥{formatPrice(o.money)}</span>
      ),
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
      title: "下单时间",
      dataIndex: "createdAt",
      width: 130,
      render: (_: unknown, o: LocalOrder) =>
        new Date(o.createdAt).toLocaleString("zh-CN", {
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
        }),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 90,
      render: (_: unknown, o: LocalOrder) => (
        <Button size="small" type="primary" onClick={() => openDetail(o.orderNumber)}>
          查看详情
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space wrap>
        <Input.Search
          allowClear
          placeholder="订单号 / 商品名称"
          style={{ width: 240 }}
          onSearch={(v) => {
            setKeyword(v);
            setPage(1);
          }}
        />
        <Select
          value={status}
          style={{ width: 140 }}
          onChange={(v) => {
            setStatus(String(v));
            setPage(1);
          }}
        >
          <Select.Option value="all">全部状态</Select.Option>
          <Select.Option value="done">已完成</Select.Option>
          <Select.Option value="processing">进行中</Select.Option>
        </Select>
      </Space>

      <Table
        rowKey="orderNumber"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 900 }}
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

      <Drawer
        width={520}
        title="订单详情"
        visible={drawerOpen}
        onCancel={() => setDrawerOpen(false)}
        footer={null}
        unmountOnExit
      >
        {detail ? (
          <div className="flex flex-col gap-3">
            <div className="flex items-center justify-between">
              <Space>
                <Tag color={arcoColor(orderStatusColor(detail.status))}>
                  {ORDER_STATUS[detail.status] ?? detail.status}
                </Tag>
                <span className="text-[12px] text-[#86909c]">{detail.result || ""}</span>
              </Space>
              <Space>
                <Button size="small" onClick={downloadTxt} disabled={detail.cards.length === 0}>
                  下载 TXT
                </Button>
                <Button size="small" loading={detailLoading} onClick={refreshDetail}>
                  刷新
                </Button>
              </Space>
            </div>

            <Descriptions
              column={1}
              size="small"
              border
              data={[
                { label: "订单号", value: detail.orderNumber },
                { label: "商品", value: <span className="text-[12px]">{detail.goodsName}</span> },
                { label: "数量", value: detail.count },
                {
                  label: "金额",
                  value: <span className="font-semibold text-[#f53f3f]">¥{formatPrice(detail.money)}</span>,
                },
                { label: "下单时间", value: new Date(detail.createdAt).toLocaleString("zh-CN") },
              ]}
            />

            <div>
              <div className="mb-2 text-[13px] font-medium">卡券（{detail.cards.length}）</div>
              {detail.cards.length === 0 ? (
                <div className="rounded border border-[#e5e6eb] px-3 py-4 text-center text-[12px] text-[#86909c]">
                  暂无卡券，请点击「刷新」查看发货结果。
                </div>
              ) : (
                <div className="flex flex-col gap-2">
                  {detail.cards.map((c) => (
                    <div
                      key={c.cardId}
                      className="flex items-center justify-between gap-2 rounded border border-[#e5e6eb] px-3 py-2"
                    >
                      <div className="min-w-0 font-mono text-[12px]">
                        <div className="truncate">{c.cardNo || "(无卡号)"}</div>
                        {c.cardPwd ? <div className="truncate text-[#86909c]">{c.cardPwd}</div> : null}
                      </div>
                      <Space>
                        <Tag color={arcoColor(cardStatusColor(c.status))}>{CARD_STATUS[c.status] ?? c.status}</Tag>
                        <Button
                          size="small"
                          onClick={() => {
                            navigator.clipboard?.writeText([c.cardNo, c.cardPwd].filter(Boolean).join("----"));
                            Message.success("已复制");
                          }}
                        >
                          复制
                        </Button>
                        {c.cancelRequest === 1 && c.status !== 2 && (
                          <Button size="small" status="danger" onClick={() => voidCard(c.cardId)}>
                            作废
                          </Button>
                        )}
                      </Space>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        ) : (
          <div className="py-10 text-center text-[13px] text-[#86909c]">加载中…</div>
        )}
      </Drawer>
    </div>
  );
}
