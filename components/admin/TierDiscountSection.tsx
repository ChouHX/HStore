"use client";

import {
  Button,
  Card,
  Empty,
  Input,
  InputNumber,
  Message,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
} from "@arco-design/web-react";
import { IconDelete, IconPlus, IconRefresh } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useRef, useState } from "react";
import { formatPrice } from "@/lib/format";

interface Tier {
  minCount: number;
  discount: number;
}

interface TierRow {
  goodsId: number;
  name: string;
  fulfillment: string;
  finalPrice: number;
  tiers: Tier[];
}

interface ProductOption {
  goodsId: number;
  name: string;
  finalPrice: number;
}

/** 折扣系数转成「X 折」文案：0.95 → 9.5 折 */
function discountLabel(d: number): string {
  const zhe = Number((d * 10).toFixed(2));
  return `${zhe} 折`;
}

/** 商品搜索选择器（远程搜索后台商品） */
function ProductPicker({
  value,
  onChange,
}: {
  value: ProductOption | null;
  onChange: (v: ProductOption | null) => void;
}) {
  const [options, setOptions] = useState<ProductOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState("");
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
    void search("");
  }, [search]);

  return (
    <div className="flex flex-col gap-2">
      <Input
        value={keyword}
        placeholder="搜索商品名称 / 编号"
        autoComplete="off"
        onChange={(v) => {
          setKeyword(v);
          if (timer.current) clearTimeout(timer.current);
          timer.current = setTimeout(() => search(v), 300);
        }}
      />
      <div className="max-h-[220px] overflow-auto rounded border border-[#e5e6eb]">
        {loading ? (
          <div className="px-3 py-4 text-center text-[12px] text-[#86909c]">搜索中…</div>
        ) : options.length === 0 ? (
          <div className="px-3 py-4 text-center text-[12px] text-[#86909c]">没有匹配的商品</div>
        ) : (
          options.map((o) => (
            <button
              key={o.goodsId}
              type="button"
              onClick={() => onChange(o)}
              className={`flex w-full items-center justify-between gap-2 border-b border-[#f2f3f5] px-3 py-2 text-left text-[12px] last:border-b-0 hover:bg-[#f7f8fa] ${
                value?.goodsId === o.goodsId ? "bg-[#e8f3ff]" : ""
              }`}
            >
              <span className="min-w-0 flex-1 truncate">
                <span className="mr-1 font-mono text-[#86909c]">{o.goodsId}</span>
                {o.name}
              </span>
              <span className="shrink-0 text-[#f53f3f]">¥{formatPrice(o.finalPrice)}</span>
            </button>
          ))
        )}
      </div>
    </div>
  );
}

/**
 * 商品批量折扣（阶梯折扣）配置。
 *
 * 规则：购买数量 >= 门槛时，单价按「加价模板后的售价 × 折扣」计算，
 * 再与会员等级折扣相乘。自营商品与上游商品都支持。
 */
export function TierDiscountSection() {
  const [rows, setRows] = useState<TierRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [product, setProduct] = useState<ProductOption | null>(null);
  const [tiers, setTiers] = useState<Tier[]>([{ minCount: 10, discount: 0.95 }]);

  const load = useCallback(async (kw: string) => {
    setLoading(true);
    try {
      const sp = new URLSearchParams();
      if (kw.trim()) sp.set("keyword", kw.trim());
      const j = await fetch(`/api/admin/tier-discounts?${sp}`).then((r) => r.json());
      if (j.ok) setRows((j.items ?? []) as TierRow[]);
      else Message.error(j.message ?? "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load("");
  }, [load]);

  function openAdd() {
    setProduct(null);
    setTiers([{ minCount: 10, discount: 0.95 }]);
    setOpen(true);
  }

  function openEdit(row: TierRow) {
    setProduct({ goodsId: row.goodsId, name: row.name, finalPrice: row.finalPrice });
    setTiers(row.tiers.map((t) => ({ minCount: t.minCount, discount: t.discount })));
    setOpen(true);
  }

  async function save() {
    if (!product) {
      Message.warning("请选择商品");
      return;
    }
    const valid = tiers.filter((t) => t.minCount >= 2 && t.discount > 0 && t.discount < 1);
    if (valid.length === 0) {
      Message.warning("请至少配置一档有效折扣（门槛 ≥ 2，折扣在 0~1 之间）");
      return;
    }
    setSaving(true);
    try {
      const j = await fetch("/api/admin/tier-discounts", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "save", goodsId: product.goodsId, tiers: valid }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("已保存");
        setOpen(false);
        await load(keyword);
      } else Message.error(j.message ?? "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function remove(row: TierRow) {
    const j = await fetch("/api/admin/tier-discounts", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "delete", goodsId: row.goodsId }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已清除折扣");
      await load(keyword);
    } else Message.error(j.message ?? "操作失败");
  }

  const columns = [
    {
      title: "商品",
      dataIndex: "name",
      render: (_: unknown, r: TierRow) => (
        <div className="min-w-0">
          <div className="truncate text-[12px] text-[#1d2129]" title={r.name}>
            {r.name}
          </div>
          <div className="mt-0.5 font-mono text-[11px] text-[#86909c]">{r.goodsId}</div>
        </div>
      ),
    },
    {
      title: "类型",
      dataIndex: "fulfillment",
      width: 80,
      render: (v: string) =>
        v === "self" ? (
          <Tag size="small" color="arcoblue">自营</Tag>
        ) : (
          <Tag size="small" color="gray">上游</Tag>
        ),
    },
    {
      title: "原价",
      dataIndex: "finalPrice",
      width: 90,
      render: (v: number) => <span className="text-[#f53f3f]">¥{formatPrice(v)}</span>,
    },
    {
      title: "阶梯折扣",
      dataIndex: "tiers",
      render: (_: unknown, r: TierRow) => (
        <Space size={4} wrap>
          {r.tiers.map((t) => (
            <Tag key={t.minCount} size="small" color="green">
              满 {t.minCount} 件 {discountLabel(t.discount)}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 130,
      render: (_: unknown, r: TierRow) => (
        <Space size="mini">
          <Button size="mini" onClick={() => openEdit(r)}>
            修改
          </Button>
          <Popconfirm title={`清除「${r.name}」的全部批量折扣？`} onOk={() => remove(r)}>
            <Button size="mini" status="danger">
              清除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Card
      bordered={false}
      title="商品批量折扣"
      extra={
        <Space>
          <Input.Search
            allowClear
            value={keyword}
            placeholder="搜索商品"
            style={{ width: 200 }}
            autoComplete="off"
            onChange={(v) => {
              setKeyword(v);
              if (!v) void load("");
            }}
            onSearch={(v) => load(v)}
          />
          <Button size="mini" icon={<IconRefresh />} loading={loading} onClick={() => load(keyword)}>
            刷新
          </Button>
          <Button size="mini" type="primary" icon={<IconPlus />} onClick={openAdd}>
            配置折扣
          </Button>
        </Space>
      }
    >
      <div className="mb-3 text-[11px] leading-relaxed text-[#86909c]">
        购买数量达到门槛后，单价按「加价模板算出的售价 × 折扣」计算，并与会员等级折扣叠加。
        同一商品可配置多档，取满足条件的最高门槛那一档。自营与上游商品均支持。
      </div>
      <Table
        rowKey="goodsId"
        size="small"
        loading={loading}
        columns={columns}
        data={rows}
        pagination={false}
        scroll={{ x: 760 }}
        noDataElement={
          <Empty description="还没有配置任何批量折扣，点击右上角「配置折扣」开始" />
        }
      />

      <Modal
        title={product ? `批量折扣：${product.name}` : "配置批量折扣"}
        visible={open}
        onCancel={() => setOpen(false)}
        onOk={save}
        confirmLoading={saving}
        okText="保存"
        style={{ width: 620 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          {!product && (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">选择商品</div>
              <ProductPicker value={product} onChange={setProduct} />
            </div>
          )}
          {product && (
            <>
              <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[12px] text-[#4e5969]">
                商品 <span className="font-mono">{product.goodsId}</span> · 原价{" "}
                <span className="text-[#f53f3f]">¥{formatPrice(product.finalPrice)}</span>
                <Button
                  size="mini"
                  style={{ marginLeft: 8 }}
                  onClick={() => setProduct(null)}
                >
                  重选
                </Button>
              </div>

              <div className="flex flex-col gap-2">
                <div className="text-[12px] text-[#4e5969]">折扣档位</div>
                {tiers.map((t, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <span className="shrink-0 text-[12px] text-[#86909c]">满</span>
                    <InputNumber
                      value={t.minCount}
                      min={2}
                      precision={0}
                      style={{ width: 100 }}
                      onChange={(v) =>
                        setTiers((prev) =>
                          prev.map((x, j) => (j === i ? { ...x, minCount: Number(v) || 2 } : x)),
                        )
                      }
                    />
                    <span className="shrink-0 text-[12px] text-[#86909c]">件，单价</span>
                    <InputNumber
                      value={Number((t.discount * 10).toFixed(2))}
                      min={0.1}
                      max={9.99}
                      step={0.1}
                      precision={2}
                      style={{ width: 110 }}
                      onChange={(v) =>
                        setTiers((prev) =>
                          prev.map((x, j) =>
                            j === i ? { ...x, discount: Number(((Number(v) || 10) / 10).toFixed(4)) } : x,
                          ),
                        )
                      }
                    />
                    <span className="shrink-0 text-[12px] text-[#86909c]">折</span>
                    <span className="shrink-0 text-[11px] text-[#86909c]">
                      ≈ ¥{formatPrice(product.finalPrice * t.discount)}
                    </span>
                    <Button
                      size="mini"
                      status="danger"
                      icon={<IconDelete />}
                      disabled={tiers.length <= 1}
                      onClick={() => setTiers((prev) => prev.filter((_, j) => j !== i))}
                    />
                  </div>
                ))}
                <Button
                  size="small"
                  icon={<IconPlus />}
                  style={{ width: 120 }}
                  onClick={() =>
                    setTiers((prev) => {
                      const last = prev[prev.length - 1];
                      return [...prev, { minCount: (last?.minCount ?? 10) * 2, discount: 0.9 }];
                    })
                  }
                >
                  添加档位
                </Button>
              </div>

              <div className="rounded bg-[#fff7e8] px-3 py-2 text-[11px] leading-relaxed text-[#d25f00]">
                示例：原价 ¥{formatPrice(product.finalPrice)}，配置「满 10 件 9.5 折」后，
                买 10 件的单价为 ¥{formatPrice(product.finalPrice * 0.95)}，总价
                ¥{formatPrice(product.finalPrice * 0.95 * 10)}。
              </div>
            </>
          )}
        </div>
      </Modal>
    </Card>
  );
}
