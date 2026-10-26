"use client";

import { Button, Message, Table, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import type { AuditLog } from "@/lib/api-types";

const PAGE_SIZE = 20;

export function LogsSection() {
  const [items, setItems] = useState<AuditLog[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const j = await fetch(`/api/admin/audit?page=${page}&pageSize=${PAGE_SIZE}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as AuditLog[]);
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
    { title: "时间", dataIndex: "createdAt", width: 180, render: (_: unknown, l: AuditLog) => new Date(l.createdAt).toLocaleString("zh-CN") },
    { title: "操作人", dataIndex: "operatorId", width: 90, render: (_: unknown, l: AuditLog) => l.operatorId ?? "-" },
    { title: "动作", dataIndex: "action", render: (_: unknown, l: AuditLog) => <Tag size="small" color="arcoblue">{l.action}</Tag> },
    {
      title: "对象",
      dataIndex: "target",
      render: (_: unknown, l: AuditLog) => `${l.targetType}${l.targetId ? `#${l.targetId}` : ""}`,
    },
    { title: "备注", dataIndex: "remark", render: (_: unknown, l: AuditLog) => <span className="text-[#86909c]">{l.remark}</span> },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <span className="text-[12px] text-[#86909c]">记录余额调整、等级变更、定价与上下架等操作。</span>
        <Button size="small" onClick={load}>刷新</Button>
      </div>
      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 820 }}
        pagination={{ current: page, pageSize: PAGE_SIZE, total, showTotal: true, onChange: (p) => setPage(p) }}
      />
    </div>
  );
}
