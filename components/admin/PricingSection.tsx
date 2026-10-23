"use client";

import {
  Button,
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
import type { PricingFormula, PricingRule, Rounding } from "@/lib/types";

const EMPTY = {
  id: 0,
  name: "",
  scope: "global" as "global" | "platform" | "category" | "product",
  scopeId: "",
  formulaType: "fixed_add" as PricingFormula,
  amount: 0,
  percent: 0,
  multiplier: 1,
  fixed: 0,
  rounding: "round4" as Rounding,
  priority: 0,
  enabled: 1,
};

const ROUNDINGS: { value: Rounding; label: string }[] = [
  { value: "round4", label: "四舍五入 4 位" },
  { value: "ceil4", label: "向上取整 4 位" },
  { value: "floor4", label: "向下取整 4 位" },
  { value: "round3", label: "四舍五入 3 位" },
  { value: "round2", label: "四舍五入 2 位" },
  { value: "ceil2", label: "向上取整 2 位" },
  { value: "floor2", label: "向下取整 2 位" },
  { value: "ceil1", label: "向上取整 1 位" },
  { value: "round0", label: "取整到元" },
];

const PLATFORMS = [
  { value: "microsoft", label: "微软邮箱" },
  { value: "google", label: "谷歌邮箱" },
];

function formulaText(r: PricingRule): string {
  switch (r.formulaType) {
    case "fixed_add":
      return `成本 + ${r.amount}`;
    case "fixed_sub":
      return `成本 - ${r.amount}`;
    case "ratio":
      return `成本 × (1 + ${r.percent}%)`;
    case "mixed":
      return `成本 × ${r.multiplier} + ${r.fixed}`;
  }
}

export function PricingSection() {
  const [rules, setRules] = useState<PricingRule[]>([]);
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ ...EMPTY });
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const j = await fetch("/api/admin/pricing").then((r) => r.json());
      if (j.ok) setRules(j.rules as PricingRule[]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function post(body: unknown, ok: string) {
    const j = await fetch("/api/admin/pricing", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success(ok);
      setOpen(false);
      load();
    } else Message.error(j.message ?? "操作失败");
  }

  const set = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) => setForm((f) => ({ ...f, [k]: v }));

  const columns = [
    { title: "名称", dataIndex: "name" },
    {
      title: "范围",
      dataIndex: "scope",
      render: (_: unknown, r: PricingRule) =>
        r.scope === "global"
          ? "全站"
          : r.scope === "platform"
            ? `平台 ${PLATFORMS.find((p) => p.value === r.scopeId)?.label ?? r.scopeId}`
            : r.scope === "category"
              ? `分类 ${r.scopeId}`
              : `商品 ${r.scopeId}`,
    },
    { title: "公式", dataIndex: "formula", render: (_: unknown, r: PricingRule) => <span className="font-mono">{formulaText(r)}</span> },
    {
      title: "舍入",
      dataIndex: "rounding",
      render: (_: unknown, r: PricingRule) => ROUNDINGS.find((o) => o.value === r.rounding)?.label,
    },
    { title: "优先级", dataIndex: "priority", width: 80, align: "right" as const },
    {
      title: "状态",
      dataIndex: "enabled",
      width: 90,
      render: (_: unknown, r: PricingRule) => (
        <Tag size="small" color={r.enabled ? "green" : "gray"}>
          {r.enabled ? "启用" : "停用"}
        </Tag>
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 220,
      render: (_: unknown, r: PricingRule) => (
        <Space>
          <Button size="small" onClick={() => { setForm({ ...EMPTY, ...r }); setOpen(true); }}>编辑</Button>
          <Button size="small" onClick={() => post({ action: "update", id: r.id, enabled: r.enabled ? 0 : 1 }, "状态已更新")}>
            {r.enabled ? "停用" : "启用"}
          </Button>
          <Popconfirm title="确认删除该规则？" onOk={() => post({ action: "delete", id: r.id }, "规则已删除")}>
            <Button size="small" status="danger">删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <span className="text-[12px] text-[#86909c]">越具体优先：单品 &gt; 分类 &gt; 全站；同范围按优先级。</span>
        <Button size="small" type="primary" onClick={() => { setForm({ ...EMPTY }); setOpen(true); }}>新建规则</Button>
      </div>

      <Table rowKey="id" loading={loading} columns={columns} data={rules} pagination={false} scroll={{ x: 900 }} />

      <Modal
        title={form.id ? `编辑规则 #${form.id}` : "新建定价规则"}
        visible={open}
        onCancel={() => setOpen(false)}
        onOk={() => post({ action: form.id ? "update" : "create", ...form }, form.id ? "规则已更新" : "规则已创建")}
        okText="保存并重算"
        style={{ width: 620 }}
        unmountOnExit
      >
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">规则名称</div>
            <Input value={form.name} onChange={(v) => set("name", v)} placeholder="如：全站加价 10%" />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">作用范围</div>
            <Select value={form.scope} onChange={(v) => set("scope", v as typeof form.scope)}>
              <Select.Option value="global">全站</Select.Option>
              <Select.Option value="platform">指定平台</Select.Option>
              <Select.Option value="category">指定分类</Select.Option>
              <Select.Option value="product">指定商品</Select.Option>
            </Select>
          </div>
          {form.scope !== "global" && (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">
                {form.scope === "platform" ? "平台" : form.scope === "category" ? "分类 ID" : "商品编号"}
              </div>
              {form.scope === "platform" ? (
                <Select value={form.scopeId || "microsoft"} onChange={(v) => set("scopeId", String(v))}>
                  {PLATFORMS.map((p) => (
                    <Select.Option key={p.value} value={p.value}>
                      {p.label}
                    </Select.Option>
                  ))}
                </Select>
              ) : (
                <Input value={form.scopeId} onChange={(v) => set("scopeId", v)} placeholder="填写 ID" />
              )}
            </div>
          )}
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">定价公式</div>
            <Select value={form.formulaType} onChange={(v) => set("formulaType", v as PricingFormula)}>
              <Select.Option value="fixed_add">固定加价：成本 + X</Select.Option>
              <Select.Option value="fixed_sub">固定减价：成本 - X</Select.Option>
              <Select.Option value="ratio">比例浮动：成本 × (1 + Y%)</Select.Option>
              <Select.Option value="mixed">混合：成本 × 倍率 + 固定</Select.Option>
            </Select>
          </div>
          {(form.formulaType === "fixed_add" || form.formulaType === "fixed_sub") && (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">固定金额 X</div>
              <InputNumber value={form.amount} min={0} step={0.01} style={{ width: "100%" }} onChange={(v) => set("amount", Number(v) || 0)} />
            </div>
          )}
          {form.formulaType === "ratio" && (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">百分比 Y (%)</div>
              <InputNumber value={form.percent} style={{ width: "100%" }} onChange={(v) => set("percent", Number(v) || 0)} />
            </div>
          )}
          {form.formulaType === "mixed" && (
            <>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">倍率</div>
                <InputNumber value={form.multiplier} step={0.1} style={{ width: "100%" }} onChange={(v) => set("multiplier", Number(v) || 0)} />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">固定金额 ±</div>
                <InputNumber value={form.fixed} step={0.01} style={{ width: "100%" }} onChange={(v) => set("fixed", Number(v) || 0)} />
              </div>
            </>
          )}
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">舍入规则</div>
            <Select value={form.rounding} onChange={(v) => set("rounding", v as Rounding)}>
              {ROUNDINGS.map((o) => (
                <Select.Option key={o.value} value={o.value}>{o.label}</Select.Option>
              ))}
            </Select>
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">优先级</div>
            <InputNumber value={form.priority} style={{ width: "100%" }} onChange={(v) => set("priority", Number(v) || 0)} />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-[12px] text-[#4e5969]">启用</span>
            <Switch checked={form.enabled === 1} onChange={(c) => set("enabled", c ? 1 : 0)} />
          </div>
        </div>
      </Modal>
    </div>
  );
}
