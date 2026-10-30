"use client";

import {
  Button,
  Card,
  Input,
  InputNumber,
  Message,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import type { Announcement } from "@/lib/api-types";
import { RichTextEditor } from "@/components/common/RichTextEditor";
import { contentToPlainText, sanitizeRichText } from "@/lib/rich-text";

const EMPTY = { id: 0, type: "normal" as "normal" | "popup", title: "", content: "", enabled: true, sort: 0 };

export function AnnouncementsSection() {
  const [items, setItems] = useState<Announcement[]>([]);
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ ...EMPTY });
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    const j = await fetch("/api/admin/announcements").then((r) => r.json());
    if (j.ok) setItems(j.items as Announcement[]);
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function post(body: unknown, ok: string) {
    setBusy(true);
    try {
      const j = await fetch("/api/admin/announcements", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(ok);
        setOpen(false);
        await load();
      } else Message.error(j.message ?? "操作失败");
    } finally {
      setBusy(false);
    }
  }

  const columns = [
    { title: "ID", dataIndex: "id", width: 60 },
    {
      title: "类型",
      dataIndex: "type",
      width: 90,
      render: (_: unknown, r: Announcement) => (
        <Tag size="small" color={r.type === "popup" ? "orange" : "arcoblue"}>
          {r.type === "popup" ? "弹窗" : "普通"}
        </Tag>
      ),
    },
    { title: "标题", dataIndex: "title" },
    {
      title: "内容",
      dataIndex: "content",
      render: (_: unknown, r: Announcement) => (
        <span className="line-clamp-1 max-w-[360px] text-[#86909c]">
          {contentToPlainText(r.content) || "-"}
        </span>
      ),
    },
    {
      title: "状态",
      dataIndex: "enabled",
      width: 90,
      render: (_: unknown, r: Announcement) => (
        <Tag size="small" color={r.enabled ? "green" : "gray"}>
          {r.enabled ? "启用" : "停用"}
        </Tag>
      ),
    },
    { title: "排序", dataIndex: "sort", width: 70, align: "right" as const },
    {
      title: "更新时间",
      dataIndex: "updatedAt",
      width: 160,
      render: (_: unknown, r: Announcement) => new Date(r.updatedAt).toLocaleString("zh-CN"),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 200,
      render: (_: unknown, r: Announcement) => (
        <Space>
          <Button
            size="small"
            onClick={() => {
              setForm({
                id: r.id,
                type: r.type,
                title: r.title,
                content: r.content,
                enabled: r.enabled === 1,
                sort: r.sort,
              });
              setOpen(true);
            }}
          >
            编辑
          </Button>
          <Button
            size="small"
            onClick={() => post({ action: "toggle", id: r.id, enabled: r.enabled !== 1 }, r.enabled ? "已停用" : "已启用")}
          >
            {r.enabled ? "停用" : "启用"}
          </Button>
          <Popconfirm title="确认删除该公告？" onOk={() => post({ action: "delete", id: r.id }, "已删除")}>
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
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-[12px] text-[#86909c]">
          普通公告展示在商城 Hero 区与「公告」抽屉；弹窗公告仅一个生效，用户首次加载时弹窗（按公告 ID 记住已读）。
        </div>
        <Button
          type="primary"
          size="small"
          onClick={() => {
            setForm({ ...EMPTY });
            setOpen(true);
          }}
        >
          新增公告
        </Button>
      </div>

      <Table rowKey="id" columns={columns} data={items} pagination={false} scroll={{ x: 900 }} />

      <Modal
        title={form.id ? `编辑公告 #${form.id}` : "新增公告"}
        visible={open}
        onCancel={() => setOpen(false)}
        onOk={() =>
          post(
            {
              action: "save",
              id: form.id || undefined,
              type: form.type,
              title: form.title,
              content: sanitizeRichText(form.content),
              enabled: form.enabled,
              sort: form.sort,
            },
            form.id ? "公告已更新" : "公告已创建",
          )
        }
        confirmLoading={busy}
        style={{ width: 720 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">类型</div>
            <Select value={form.type} onChange={(v) => setForm({ ...form, type: v as "normal" | "popup" })} style={{ width: "100%" }}>
              <Select.Option value="normal">普通公告（Hero 区 / 公告抽屉）</Select.Option>
              <Select.Option value="popup">弹窗公告（仅一个生效）</Select.Option>
            </Select>
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">标题</div>
            <Input value={form.title} onChange={(v) => setForm({ ...form, title: v })} placeholder="公告标题" />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">内容</div>
            <RichTextEditor
              value={form.content}
              onChange={(html) => setForm({ ...form, content: html })}
              placeholder="公告内容，支持标题、加粗、列表、链接等格式"
              minHeight={200}
            />
          </div>
          <div className="flex items-center gap-6">
            <span className="flex items-center gap-2 text-[12px] text-[#4e5969]">
              启用
              <Switch checked={form.enabled} onChange={(c) => setForm({ ...form, enabled: c })} />
            </span>
            <span className="flex items-center gap-2 text-[12px] text-[#4e5969]">
              排序
              <InputNumber value={form.sort} style={{ width: 100 }} onChange={(v) => setForm({ ...form, sort: Number(v) || 0 })} />
            </span>
          </div>
        </div>
      </Modal>
    </div>
  );
}
