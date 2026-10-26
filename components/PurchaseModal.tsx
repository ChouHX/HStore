"use client";
import { formatPrice } from "@/lib/format";

import { Button, Input, InputNumber, Message, Modal, Select, Space, Tag } from "@arco-design/web-react";
import { useEffect, useMemo, useState } from "react";
import type { GoodsDetail, LocalOrder, Product, RechargeTemplate } from "@/lib/types";
import { CARD_STATUS, cardStatusColor, arcoColor } from "@/lib/status";

interface Props {
  product: Product | null;
  open: boolean;
  discount?: number;
  onOpenChange: (open: boolean) => void;
  onDone: () => void;
}

function TemplateField({
  tpl,
  value,
  onChange,
}: {
  tpl: RechargeTemplate;
  value: string;
  onChange: (v: string) => void;
}) {
  const label = `${tpl.title}${tpl.required === 1 ? " *" : ""}`;
  if (tpl.type === 14 && tpl.options?.length) {
    return (
      <div className="flex flex-col gap-1 text-[12px]">
        <span className="text-[#86909c]">{label}</span>
        <Select value={value || undefined} onChange={(v) => onChange(String(v ?? ""))} placeholder="请选择">
          {tpl.options.map((o) => (
            <Select.Option key={o.value} value={o.value}>
              {o.name}
            </Select.Option>
          ))}
        </Select>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-1 text-[12px]">
      <span className="text-[#86909c]">{label}</span>
      <Input placeholder={tpl.placeholder || "请输入"} value={value} onChange={onChange} />
    </div>
  );
}

export function PurchaseModal({ product, open, discount = 1, onOpenChange, onDone }: Props) {
  const [detail, setDetail] = useState<GoodsDetail | null>(null);
  const [count, setCount] = useState(1);
  const [attach, setAttach] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [order, setOrder] = useState<LocalOrder | null>(null);

  useEffect(() => {
    if (!open || !product) return;
    setDetail(null);
    setOrder(null);
    setCount(1);
    setAttach({});
    setLoading(true);
    fetch(`/api/goods/detail?id=${product.goodsId}`)
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setDetail(j.detail);
        else Message.error(j.message ?? "获取详情失败");
      })
      .catch(() => Message.error("获取详情失败"))
      .finally(() => setLoading(false));
  }, [open, product]);

  const templates = useMemo(() => (detail?.rechargeTemplates ?? []).filter((t) => t.title), [detail]);
  const unitPrice = product ? Number((product.finalPrice * discount).toFixed(4)) : 0;
  const total = unitPrice * count;

  async function submit() {
    if (!product) return;
    const missing = templates.find((t) => t.required === 1 && !attach[t.title]);
    if (missing) {
      Message.warning(`请填写「${missing.title}」`);
      return;
    }
    setSubmitting(true);
    try {
      const res = await fetch("/api/order/create", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          goodsId: product.goodsId,
          count,
          attach: templates.map((t) => ({ name: t.title, value: attach[t.title] ?? "" })),
        }),
      });
      const j = await res.json();
      if (j.ok) {
        setOrder(j.order);
        Message.success("下单成功");
        onDone();
      } else {
        Message.error(j.message ?? "下单失败");
      }
    } catch {
      Message.error("下单请求失败");
    } finally {
      setSubmitting(false);
    }
  }

  const footer = order ? (
    <Button type="primary" onClick={() => onOpenChange(false)}>
      完成
    </Button>
  ) : (
    <Space>
      <Button onClick={() => onOpenChange(false)}>取消</Button>
      <Button type="primary" loading={submitting} disabled={!detail} onClick={submit}>
        确认购买
      </Button>
    </Space>
  );

  return (
    <Modal
      title="购买商品"
      visible={open}
      onCancel={() => onOpenChange(false)}
      footer={footer}
      style={{ width: 560 }}
      unmountOnExit
    >
      {loading ? (
        <div className="py-10 text-center text-[13px] text-[#86909c]">加载中…</div>
      ) : order ? (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2">
            <Tag color="green">下单成功</Tag>
            <span className="font-mono text-[12px]">订单号 {order.orderNumber}</span>
          </div>
          <div className="text-[12px] text-[#86909c]">
            {order.goodsName} × {order.count}，金额 ¥{formatPrice(order.money)}
          </div>
          <div className="flex flex-col gap-2">
            {order.cards.length === 0 && (
              <div className="text-[12px] text-[#86909c]">暂无卡券，请稍后刷新订单查看发货结果。</div>
            )}
            {order.cards.map((c) => (
              <div
                key={c.cardId}
                className="flex items-center justify-between gap-2 rounded border border-[#e5e6eb] px-3 py-2"
              >
                <div className="min-w-0 font-mono text-[12px]">
                  <div className="truncate">{c.cardNo || "(无卡号)"}</div>
                  {c.cardPwd ? <div className="truncate text-[#86909c]">{c.cardPwd}</div> : null}
                </div>
                <Space>
                  <Tag color={arcoColor(cardStatusColor(c.status))}>{CARD_STATUS[c.status] ?? c.status}</Tag>
                  <Button
                    size="small"
                    onClick={() => {
                      navigator.clipboard?.writeText([c.cardNo, c.cardPwd].filter(Boolean).join("----"));
                      Message.success("已复制");
                    }}
                  >
                    复制
                  </Button>
                </Space>
              </div>
            ))}
          </div>
        </div>
      ) : detail ? (
        <div className="flex flex-col gap-3">
          <div className="flex gap-3">
            {detail.imgUrl && (
              <img
                src={`/api/image?url=${encodeURIComponent(detail.imgUrl)}`}
                alt=""
                className="h-16 w-16 shrink-0 rounded object-cover"
              />
            )}
            <div className="min-w-0">
              <div className="text-[13px] font-medium">{product?.customName || detail.name}</div>
              {(product?.customDesc || detail.goodsDescribe) && (
                <div className="mt-1 text-[11px] text-[#86909c]">
                  {product?.customDesc || detail.goodsDescribe}
                </div>
              )}
              <Space size={12} style={{ marginTop: 6 }}>
                <Tag size="small">库存 {detail.stockCount}</Tag>
                <Tag size="small">限购 {detail.minQuantity} ~ {detail.maxQuantity}</Tag>
              </Space>
            </div>
          </div>

          <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[12px] text-[#4e5969]">
            单价 <span className="font-semibold text-[#f53f3f]">¥{formatPrice(unitPrice)}</span>
            {discount < 1 && <span className="ml-1">（原价 ¥{formatPrice(product?.finalPrice)}，会员 {Math.round(discount * 100)} 折）</span>}
            ，下单后自动发货。
          </div>

          {(detail.goodsDetail || detail.buyNotice) && (
            <details open className="rounded border border-[#e5e6eb] px-3 py-2">
              <summary className="cursor-pointer text-[12px] text-[#4e5969]">商品详情 / 购买须知</summary>
              <div className="mt-2 max-h-[260px] overflow-y-auto text-[12px] leading-relaxed text-[#4e5969]">
                {detail.goodsDetail && (
                  <div
                    className="product-detail"
                    dangerouslySetInnerHTML={{ __html: detail.goodsDetail }}
                  />
                )}
                {detail.buyNotice && (
                  <div className="mt-2 whitespace-pre-wrap">购买须知：{detail.buyNotice}</div>
                )}
              </div>
            </details>
          )}

          {templates.length > 0 && (
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {templates.map((t) => (
                <TemplateField
                  key={t.title}
                  tpl={t}
                  value={attach[t.title] ?? ""}
                  onChange={(v) => setAttach((s) => ({ ...s, [t.title]: v }))}
                />
              ))}
            </div>
          )}

          <div className="flex items-end justify-between">
            <div className="flex items-center gap-2 text-[12px] text-[#86909c]">
              购买数量
              <InputNumber
                value={count}
                onChange={(v) => setCount(Math.max(1, Number(v) || 1))}
                min={detail.minQuantity || 1}
                max={detail.maxQuantity || 999999}
                style={{ width: 120 }}
              />
            </div>
            <div className="text-right text-[12px] text-[#86909c]">
              应付 <div className="text-[18px] font-semibold text-[#f53f3f]">¥{formatPrice(total)}</div>
            </div>
          </div>
        </div>
      ) : null}
    </Modal>
  );
}
