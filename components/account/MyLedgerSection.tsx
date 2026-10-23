"use client";
import { formatPrice } from "@/lib/format";

import { Message, Table, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import type { LedgerEntry } from "@/lib/types";

const PAGE_SIZE = 12;

const TYPE_LABEL: Record<string, { label: string; color: string }> = {
  recharge: { label: "充值", color: "arcoblue" },
  consume: { label: "消费", color: "green" },
  redeem: { label: "兑换码", color: "arcoblue" },
  admin_adjust: { label: "管理员调整", color: "orange" },
};

export function MyLedgerSection() {
  const [items, setItems] = useState<LedgerEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const j = await fetch(`/api/me/ledger?page=${page}&pageSize=${PAGE_SIZE}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as LedgerEntry[]);
        setTotal(j.total as number);
      } else Message.error(j.message);
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    load();
  }, [load]);

  const columns = [
    { title: "时间", dataIndex: "createdAt", render: (_: unknown, l: LedgerEntry) => new Date(l.createdAt).toLocaleString("zh-CN") },
    {
      title: "类型",
      dataIndex: "type",
      render: (_: unknown, l: LedgerEntry) => (
        <Tag size="small" color={TYPE_LABEL[l.type]?.color ?? "gray"}>{TYPE_LABEL[l.type]?.label ?? l.type}</Tag>
      ),
    },
    {
      title: "变动",
      dataIndex: "amount",
      align: "right" as const,
      render: (_: unknown, l: LedgerEntry) => (
        <span style={{ color: l.amount >= 0 ? "#00b42a" : "#f53f3f" }}>
          {l.amount >= 0 ? "+" : ""}{formatPrice(l.amount)}
        </span>
      ),
    },
    { title: "调整前", dataIndex: "balanceBefore", align: "right" as const, render: (_: unknown, l: LedgerEntry) => formatPrice(l.balanceBefore) },
    { title: "调整后", dataIndex: "balanceAfter", align: "right" as const, render: (_: unknown, l: LedgerEntry) => formatPrice(l.balanceAfter) },
    { title: "备注", dataIndex: "remark" },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="text-[12px] text-[#86909c]">余额明细不可篡改，仅可查询。</div>
      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 700 }}
        pagination={{ current: page, pageSize: PAGE_SIZE, total, showTotal: true, onChange: (p) => setPage(p) }}
      />
    </div>
  );
}
