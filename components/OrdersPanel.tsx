"use client";
import { formatPrice } from "@/lib/format";

import { Button, Input, Message, Select, Space, Table, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import type { LocalOrder } from "@/lib/types";
import { ORDER_STATUS, arcoColor, orderStatusColor } from "@/lib/status";

const PAGE_SIZE = 10;

export function OrdersPanel() {
  const navigate = useNavigate();
  const [items, setItems] = useState<LocalOrder[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [loading, setLoading] = useState(false);

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

  function openDetail(orderNumber: string) {
    navigate(`/order/${encodeURIComponent(orderNumber)}`);
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
    </div>
  );
}
