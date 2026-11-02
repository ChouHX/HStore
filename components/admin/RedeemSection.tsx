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
  Tooltip,
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

  // 「修改」弹窗：调整兑换码关联的商品 / 面值
  const [editing, setEditing] = useState<RedeemCode | null>(null);
  const [editProduct, setEditProduct] = useState<ProductOption | null>(null);
  const [editCount, setEditCount] = useState(1);
  const [editFace, setEditFace] = useState(10);

  // 已使用 / 核销中的兑换码只能查看明细，不允许改关联。
  const readOnly = editing !== null && editing.status !== "unused" && editing.status !== "voided";

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

  async function deleteCode(codeId: number) {
    const j = await fetch("/api/admin/redeem", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "deleteCode", codeId }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已删除");
      load();
    } else Message.error(j.message ?? "删除失败");
  }

  async function saveEdit() {
    if (!editing) return;
    if (readOnly) {
      setEditing(null);
      return;
    }
    if (!editProduct && editFace <= 0) {
      Message.warning("面值必须大于 0");
      return;
    }
    setSaving(true);
    try {
      const j = await fetch("/api/admin/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          action: "updateGoods",
          codeId: editing.id,
          newGoodsId: editProduct?.goodsId ?? null,
          goodsCount: editProduct ? editCount : 0,
          faceValue: editProduct ? 0 : editFace,
        }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("已保存");
        setEditing(null);
        load();
      } else Message.error(j.message ?? "保存失败");
    } finally {
      setSaving(false);
    }
  }

  const columns = [
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
    {
      title: "兑换码",
      dataIndex: "code",
      render: (_: unknown, c: RedeemCode) => (
        <span className="font-mono select-all">{c.code}</span>
      ),
    },
    {
      title: "商品ID",
      dataIndex: "goodsId",
      width: 140,
      render: (_: unknown, c: RedeemCode) =>
        c.goodsId ? (
          <Tooltip content={c.goodsName || `商品 ${c.goodsId}`}>
            <span className="cursor-help font-mono text-[12px] text-[#165dff] underline decoration-dotted">
              {c.goodsId}
              {c.goodsCount && c.goodsCount > 1 ? ` ×${c.goodsCount}` : ""}
            </span>
          </Tooltip>
        ) : (
          <Tooltip content="面值码：兑换后充入余额">
            <span className="cursor-help text-[12px] text-[#f53f3f]">
              ¥{formatPrice(c.faceValue)}
            </span>
          </Tooltip>
        ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 190,
      fixed: "right" as const,
      render: (_: unknown, c: RedeemCode) => {
        // 已使用/核销中的码不允许再改关联，但仍可删除（清理历史数据）。
        const editable = c.status === "unused" || c.status === "voided";
        return (
          <Space size="small">
            <Button
              size="mini"
              onClick={() => {
                setEditing(c);
                setEditProduct(
                  c.goodsId ? { goodsId: c.goodsId, name: c.goodsName || "", finalPrice: 0 } : null,
                );
                setEditCount(c.goodsCount || 1);
                setEditFace(c.faceValue || 10);
              }}
            >
              {editable ? "修改" : "详情"}
            </Button>
            <Popconfirm
              title={
                c.status === "used"
                  ? `该兑换码已使用（订单 ${c.orderNumber || "-"}），删除只影响兑换码记录，不影响已生成的订单。确认删除？`
                  : "确认删除该兑换码？删除后不可恢复。"
              }
              onOk={() => deleteCode(c.id)}
            >
              <Button size="mini" status="danger">
                删除
              </Button>
            </Popconfirm>
          </Space>
        );
      },
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

      <Modal
        title={editing ? `修改兑换码 ${editing.code}` : "修改兑换码"}
        visible={editing !== null}
        onCancel={() => setEditing(null)}
        onOk={saveEdit}
        confirmLoading={saving}
        okText="保存"
        okButtonProps={{ style: readOnly ? { display: "none" } : undefined }}
        cancelText={readOnly ? "关闭" : "取消"}
        style={{ width: 480 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          {editing && editing.status === "used" && (
            <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[12px] text-[#4e5969]">
              该兑换码已于
              {editing.usedAt ? ` ${new Date(editing.usedAt).toLocaleString("zh-CN")} ` : " "}
              被使用，关联订单 <span className="font-mono">{editing.orderNumber || "-"}</span>，
              已使用的兑换码不能修改关联关系。
            </div>
          )}
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">
              关联商品（清空则改为面值码；已是自营商品请到「自营商品」页导入卡密）
            </div>
            <ProductPicker value={editProduct} onChange={readOnly ? () => {} : setEditProduct} />
          </div>
          {editProduct ? (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">每码兑换件数</div>
              <InputNumber
                value={editCount}
                min={1}
                disabled={readOnly}
                style={{ width: "100%" }}
                onChange={(v) => setEditCount(Number(v) || 1)}
              />
            </div>
          ) : (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">面值（元）</div>
              <InputNumber
                value={editFace}
                min={0.01}
                step={1}
                disabled={readOnly}
                style={{ width: "100%" }}
                onChange={(v) => setEditFace(Number(v) || 0)}
              />
            </div>
          )}
        </div>
      </Modal>
    </div>
  );
}
