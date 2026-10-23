"use client";

import { Button, Input, InputNumber, Message, Popconfirm, Space, Table, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import type { Level } from "@/lib/types";

const EMPTY = { id: 0, name: "", threshold: 0, discountPercent: 100, sort: 0 };

export function LevelsPanel({ onChanged }: { onChanged?: () => void }) {
  const [levels, setLevels] = useState<Level[]>([]);
  const [form, setForm] = useState({ ...EMPTY });
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const j = await fetch("/api/admin/levels").then((r) => r.json());
    if (j.ok) setLevels(j.levels as Level[]);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function post(body: unknown, ok: string) {
    setBusy(true);
    try {
      const j = await fetch("/api/admin/levels", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(ok);
        setForm({ ...EMPTY });
        await load();
        onChanged?.();
      } else Message.error(j.message ?? "操作失败");
    } finally {
      setBusy(false);
    }
  }

  const columns = [
    {
      title: "等级名称",
      dataIndex: "name",
      render: (_: unknown, l: Level) => <Tag color="arcoblue">{l.name}</Tag>,
    },
    {
      title: "升级门槛",
      dataIndex: "threshold",
      width: 110,
      align: "right" as const,
      render: (_: unknown, l: Level) => `¥${l.threshold}`,
    },
    {
      title: "折扣率",
      dataIndex: "discount",
      width: 90,
      align: "right" as const,
      render: (_: unknown, l: Level) => `${Math.round(l.discount * 100)}%`,
    },
    { title: "排序", dataIndex: "sort", width: 70, align: "right" as const },
    {
      title: "操作",
      dataIndex: "op",
      width: 140,
      render: (_: unknown, l: Level) => (
        <Space>
          <Button
            size="small"
            onClick={() =>
              setForm({
                id: l.id,
                name: l.name,
                threshold: l.threshold,
                discountPercent: Math.round(l.discount * 100),
                sort: l.sort,
              })
            }
          >
            编辑
          </Button>
          <Popconfirm title="删除后相关用户将重新判定等级，确认？" onOk={() => post({ action: "delete", id: l.id }, "等级已删除")}>
            <Button size="small" status="danger">
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="rounded border border-[#e5e6eb] p-3">
        <div className="mb-2 text-[13px] font-medium">{form.id ? `编辑等级 #${form.id}` : "新增会员等级"}</div>
        <div className="grid gap-2 sm:grid-cols-4">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">名称</div>
            <Input value={form.name} onChange={(v) => setForm({ ...form, name: v })} placeholder="如：黄金会员" />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">门槛（累计消费）</div>
            <InputNumber
              value={form.threshold}
              min={0}
              style={{ width: "100%" }}
              onChange={(v) => setForm({ ...form, threshold: Number(v) || 0 })}
            />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">折扣率（%）</div>
            <InputNumber
              value={form.discountPercent}
              min={1}
              max={100}
              style={{ width: "100%" }}
              onChange={(v) => setForm({ ...form, discountPercent: Number(v) || 100 })}
            />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">排序</div>
            <InputNumber
              value={form.sort}
              style={{ width: "100%" }}
              onChange={(v) => setForm({ ...form, sort: Number(v) || 0 })}
            />
          </div>
        </div>
        <div className="mt-2 flex gap-2">
          <Button
            size="small"
            type="primary"
            disabled={!form.name}
            loading={busy}
            onClick={() =>
              post(
                {
                  action: form.id ? "update" : "create",
                  id: form.id || undefined,
                  name: form.name,
                  threshold: form.threshold,
                  discount: form.discountPercent / 100,
                  sort: form.sort,
                },
                form.id ? "等级已更新" : "等级已创建",
              )
            }
          >
            {form.id ? "保存修改" : "新增等级"}
          </Button>
          {form.id > 0 && (
            <Button size="small" onClick={() => setForm({ ...EMPTY })}>
              取消编辑
            </Button>
          )}
        </div>
      </div>

      <Table rowKey="id" size="small" columns={columns} data={levels} pagination={false} scroll={{ x: 640 }} />
      <div className="text-[11px] text-[#86909c]">
        升级依据为「累计实际已消费金额」，订单结算完成才累加；充值/兑换码仅入余额，不计入成长。
      </div>
    </div>
  );
}
