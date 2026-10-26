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
  Radio,
  Select,
  Space,
  Table,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { RedeemCode } from "@/lib/types";

const PAGE_SIZE = 15;

const STATUS_TAG: Record<string, { color: string; label: string }> = {
  unused: { color: "green", label: "未使用" },
  redeeming: { color: "orange", label: "核销中" },
  used: { color: "gray", label: "已使用" },
  voided: { color: "red", label: "已作废" },
};

interface ProductOption {
  goodsId: number;
  name: string;
  finalPrice: number;
}

function validity(c: RedeemCode): string {
  if (!c.startAt && !c.endAt) return "永久";
  const s = c.startAt ? new Date(c.startAt).toLocaleDateString("zh-CN") : "立即";
  const e = c.endAt ? new Date(c.endAt).toLocaleDateString("zh-CN") : "永久";
  return `${s} ~ ${e}`;
}

/** 商品选择器：远程搜索后台商品 */
function ProductPicker({
  value,
  onChange,
}: {
  value: ProductOption | null;
  onChange: (v: ProductOption | null) => void;
}) {
  const [options, setOptions] = useState<ProductOption[]>([]);
  const [loading, setLoading] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const search = useCallback(async (kw: string) => {
    setLoading(true);
    try {
      const sp = new URLSearchParams({ page: "1", pageSize: "20" });
      if (kw) sp.set("keyword", kw);
      const j = await fetch(`/api/admin/products?${sp}`).then((r) => r.json());
      if (j.ok) {
        setOptions(
          (j.items as ProductOption[]).map((p) => ({
            goodsId: p.goodsId,
            name: p.name,
            finalPrice: p.finalPrice,
          })),
        );
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    search("");
  }, [search]);

  return (
    <Select
      showSearch
      allowClear
      filterOption={false}
      loading={loading}
      placeholder="搜索商品名称 / 编号"
      value={value?.goodsId}
      onChange={(v) => {
        const id = Number(v);
        onChange(options.find((o) => o.goodsId === id) ?? null);
      }}
      onSearch={(kw) => {
        if (timer.current) clearTimeout(timer.current);
        timer.current = setTimeout(() => search(kw), 300);
      }}
      style={{ width: "100%" }}
    >
      {options.map((o) => (
        <Select.Option key={o.goodsId} value={o.goodsId}>
          <span className="text-[12px]">
            <span className="font-mono text-[#86909c]">{o.goodsId}</span> {o.name}
            <span className="ml-2 text-[#f53f3f]">¥{formatPrice(o.finalPrice)}</span>
          </span>
        </Select.Option>
      ))}
    </Select>
  );
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
  const [saving, setSaving] = useState(false);

  const [kind, setKind] = useState<"balance" | "product">("balance");
  const [product, setProduct] = useState<ProductOption | null>(null);
  const [form, setForm] = useState({
    faceValue: 10,
    goodsCount: 1,
    count: 10,
    startAt: "",
    endAt: "",
  });

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

  function resetForm() {
    setKind("balance");
    setProduct(null);
    setForm({ faceValue: 10, goodsCount: 1, count: 10, startAt: "", endAt: "" });
  }

  async function create() {
    if (kind === "product" && !product) {
      Message.warning("请选择兑换码关联的商品");
      return;
    }
    if (kind === "balance" && form.faceValue <= 0) {
      Message.warning("面值必须大于 0");
      return;
    }
    setSaving(true);
    try {
      const j = await fetch("/api/admin/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          action: "create",
          kind,
          faceValue: kind === "balance" ? form.faceValue : 0,
          goodsId: kind === "product" ? product?.goodsId : null,
          goodsCount: kind === "product" ? form.goodsCount : 0,
          count: form.count,
          startAt: form.startAt ? new Date(form.startAt).getTime() : null,
          endAt: form.endAt ? new Date(form.endAt).getTime() : null,
        }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(`已生成 ${j.count} 张兑换码`);
        setOpen(false);
        resetForm();
        load();
      } else Message.error(j.message ?? "生成失败");
    } finally {
      setSaving(false);
    }
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
    {
      title: "兑换码",
      dataIndex: "code",
      render: (_: unknown, c: RedeemCode) => <span className="font-mono">{c.code}</span>,
    },
    {
      title: "类型",
      dataIndex: "kind",
      width: 150,
      render: (_: unknown, c: RedeemCode) =>
        c.goodsId ? (
          <Tag size="small" color="arcoblue">
            商品码
          </Tag>
        ) : (
          <Tag size="small">面值码</Tag>
        ),
    },
    {
      title: "面值 / 商品",
      dataIndex: "faceValue",
      width: 220,
      render: (_: unknown, c: RedeemCode) =>
        c.goodsId ? (
          <span className="text-[12px]">
            {c.goodsName || `#${c.goodsId}`}
            {c.goodsCount && c.goodsCount > 1 ? ` × ${c.goodsCount}` : ""}
          </span>
        ) : (
          <span className="text-[#f53f3f]">¥{formatPrice(c.faceValue)}</span>
        ),
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 90,
      render: (_: unknown, c: RedeemCode) => (
        <Tag size="small" color={STATUS_TAG[c.status]?.color}>
          {STATUS_TAG[c.status]?.label ?? c.status}
        </Tag>
      ),
    },
    {
      title: "关联订单",
      dataIndex: "orderNumber",
      width: 170,
      render: (_: unknown, c: RedeemCode) =>
        c.orderNumber ? (
          <span className="font-mono text-[12px]">{c.orderNumber}</span>
        ) : (
          <span className="text-[12px] text-[#86909c]">-</span>
        ),
    },
    {
      title: "有效期",
      dataIndex: "validity",
      width: 180,
      render: (_: unknown, c: RedeemCode) => validity(c),
    },
    {
      title: "核销用户",
      dataIndex: "usedBy",
      width: 90,
      render: (_: unknown, c: RedeemCode) => c.usedBy ?? "-",
    },
    {
      title: "核销时间",
      dataIndex: "usedAt",
      width: 160,
      render: (_: unknown, c: RedeemCode) =>
        c.usedAt ? new Date(c.usedAt).toLocaleString("zh-CN") : "-",
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 80,
      fixed: "right" as const,
      render: (_: unknown, c: RedeemCode) =>
        c.status === "unused" || c.status === "redeeming" ? (
          <Popconfirm title="确认作废该兑换码？" onOk={() => voidCode(c.id)}>
            <Button size="small" status="danger">
              作废
            </Button>
          </Popconfirm>
        ) : (
          <span className="text-[12px] text-[#86909c]">-</span>
        ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space wrap>
        <Input.Search
          allowClear
          placeholder="搜索兑换码"
          style={{ width: 220 }}
          onSearch={(v) => {
            setKeyword(v);
            setPage(1);
          }}
        />
        <Select
          value={status}
          onChange={(v) => {
            setStatus(String(v));
            setPage(1);
          }}
          style={{ width: 140 }}
        >
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
        <Button size="small" type="primary" onClick={() => setOpen(true)}>
          生成兑换码
        </Button>
      </Space>

      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 1180 }}
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
        title="生成兑换码"
        visible={open}
        onCancel={() => setOpen(false)}
        onOk={create}
        confirmLoading={saving}
        okText="生成"
        style={{ width: 520 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">兑换码类型</div>
            <Radio.Group
              type="button"
              value={kind}
              onChange={(v) => setKind(v as "balance" | "product")}
            >
              <Radio value="balance">面值码（充余额）</Radio>
              <Radio value="product">商品码（自动下单取件）</Radio>
            </Radio.Group>
          </div>

          {kind === "product" ? (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="sm:col-span-2">
                <div className="mb-1 text-[12px] text-[#4e5969]">关联商品</div>
                <ProductPicker value={product} onChange={setProduct} />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">每码兑换件数</div>
                <InputNumber
                  value={form.goodsCount}
                  min={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setForm({ ...form, goodsCount: Number(v) || 1 })}
                />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">生成数量</div>
                <InputNumber
                  value={form.count}
                  min={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setForm({ ...form, count: Number(v) || 1 })}
                />
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">面值（元）</div>
                <InputNumber
                  value={form.faceValue}
                  min={0.01}
                  step={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setForm({ ...form, faceValue: Number(v) || 0 })}
                />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">生成数量</div>
                <InputNumber
                  value={form.count}
                  min={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setForm({ ...form, count: Number(v) || 1 })}
                />
              </div>
            </div>
          )}

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">生效时间（空=立即）</div>
              <DatePicker
                showTime
                style={{ width: "100%" }}
                value={form.startAt}
                onChange={(s) => setForm({ ...form, startAt: s })}
              />
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">截止时间（空=永久）</div>
              <DatePicker
                showTime
                style={{ width: "100%" }}
                value={form.endAt}
                onChange={(s) => setForm({ ...form, endAt: s })}
              />
            </div>
          </div>

          {kind === "product" && (
            <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[11px] leading-relaxed text-[#86909c]">
              用户核销商品码时会自动调用上游下单接口取货，兑换码与生成的订单号绑定，
              用户可在「我的订单」中查看卡密，或直接打开邮箱取件。
            </div>
          )}
        </div>
      </Modal>
    </div>
  );
}
