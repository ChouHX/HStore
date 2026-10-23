"use client";
import { formatPrice } from "@/lib/format";

import {
  Button,
  DatePicker,
  Input,
  InputNumber,
  Message,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import type { RedeemCode } from "@/lib/types";

const PAGE_SIZE = 15;

const STATUS_TAG: Record<string, { color: string; label: string }> = {
  unused: { color: "green", label: "未使用" },
  used: { color: "gray", label: "已使用" },
  voided: { color: "red", label: "已作废" },
};

function validity(c: RedeemCode): string {
  if (!c.startAt && !c.endAt) return "永久";
  const s = c.startAt ? new Date(c.startAt).toLocaleDateString("zh-CN") : "立即";
  const e = c.endAt ? new Date(c.endAt).toLocaleDateString("zh-CN") : "永久";
  return `${s} ~ ${e}`;
}

export function RedeemSection() {
  const [items, setItems] = useState<RedeemCode[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ faceValue: 10, count: 10, startAt: "", endAt: "" });

  const query = useCallback(() => {
    const sp = new URLSearchParams();
    if (status !== "all") sp.set("status", status);
    if (keyword) sp.set("keyword", keyword);
    return sp;
  }, [status, keyword]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const sp = query();
      sp.set("page", String(page));
      sp.set("pageSize", String(pageSize));
      const j = await fetch(`/api/admin/redeem?${sp}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as RedeemCode[]);
        setTotal(j.total as number);
      } else Message.error(j.message);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, query]);

  useEffect(() => {
    load();
  }, [load]);

  async function create() {
    const j = await fetch("/api/admin/redeem", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        action: "create",
        faceValue: form.faceValue,
        count: form.count,
        startAt: form.startAt ? new Date(form.startAt).getTime() : null,
        endAt: form.endAt ? new Date(form.endAt).getTime() : null,
      }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success(`已生成 ${j.count} 张兑换码`);
      setOpen(false);
      load();
    } else Message.error(j.message ?? "生成失败");
  }

  async function voidCode(codeId: number) {
    const j = await fetch("/api/admin/redeem", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "voidCode", codeId }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已作废");
      load();
    } else Message.error(j.message ?? "作废失败");
  }

  const columns = [
    { title: "兑换码", dataIndex: "code", render: (_: unknown, c: RedeemCode) => <span className="font-mono">{c.code}</span> },
    { title: "面值", dataIndex: "faceValue", width: 100, align: "right" as const, render: (_: unknown, c: RedeemCode) => `¥${formatPrice(c.faceValue)}` },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (_: unknown, c: RedeemCode) => (
        <Tag size="small" color={STATUS_TAG[c.status]?.color}>
          {STATUS_TAG[c.status]?.label ?? c.status}
        </Tag>
      ),
    },
    { title: "有效期", dataIndex: "validity", width: 190, render: (_: unknown, c: RedeemCode) => validity(c) },
    { title: "核销用户", dataIndex: "usedBy", width: 100, render: (_: unknown, c: RedeemCode) => c.usedBy ?? "-" },
    {
      title: "核销时间",
      dataIndex: "usedAt",
      width: 170,
      render: (_: unknown, c: RedeemCode) => (c.usedAt ? new Date(c.usedAt).toLocaleString("zh-CN") : "-"),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 90,
      render: (_: unknown, c: RedeemCode) =>
        c.status === "unused" ? (
          <Popconfirm title="确认作废该兑换码？" onOk={() => voidCode(c.id)}>
            <Button size="small" status="danger">作废</Button>
          </Popconfirm>
        ) : (
          <span className="text-[12px] text-[#86909c]">-</span>
        ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space wrap>
        <Input.Search allowClear placeholder="搜索兑换码" style={{ width: 220 }} onSearch={(v) => { setKeyword(v); setPage(1); }} />
        <Select value={status} onChange={(v) => { setStatus(String(v)); setPage(1); }} style={{ width: 140 }}>
          <Select.Option value="all">全部状态</Select.Option>
          <Select.Option value="unused">未使用</Select.Option>
          <Select.Option value="used">已使用</Select.Option>
          <Select.Option value="voided">已作废</Select.Option>
        </Select>
        <Button
          size="small"
          onClick={() => window.open(`/api/admin/redeem?export=1&${query().toString()}`, "_blank")}
        >
          导出 CSV
        </Button>
        <Button size="small" type="primary" onClick={() => setOpen(true)}>生成兑换码</Button>
      </Space>

      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 960 }}
        pagination={{ current: page, pageSize, total, showTotal: true, sizeCanChange: true, onChange: (p, ps) => { setPage(p); setPageSize(ps); } }}
      />

      <Modal
        title="生成兑换码"
        visible={open}
        onCancel={() => setOpen(false)}
        onOk={create}
        okText="生成"
        style={{ width: 480 }}
        unmountOnExit
      >
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">面值（元）</div>
            <InputNumber value={form.faceValue} min={0.01} step={1} style={{ width: "100%" }} onChange={(v) => setForm({ ...form, faceValue: Number(v) || 0 })} />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">生成数量</div>
            <InputNumber value={form.count} min={1} style={{ width: "100%" }} onChange={(v) => setForm({ ...form, count: Number(v) || 1 })} />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">生效时间（空=立即）</div>
            <DatePicker showTime style={{ width: "100%" }} value={form.startAt} onChange={(s) => setForm({ ...form, startAt: s })} />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">截止时间（空=永久）</div>
            <DatePicker showTime style={{ width: "100%" }} value={form.endAt} onChange={(s) => setForm({ ...form, endAt: s })} />
          </div>
        </div>
      </Modal>
    </div>
  );
}
