"use client";
import { ceilToCents, formatPrice } from "@/lib/format";

import { Button, Input, InputNumber, Message, Modal, Select, Space, Tag } from "@arco-design/web-react";
import QRCode from "qrcode";
import { useEffect, useMemo, useState } from "react";
import type { GoodsDetail, LocalOrder, Product, RechargeTemplate, User } from "@/lib/types";
import { CARD_STATUS, cardStatusColor, arcoColor } from "@/lib/status";
import { PaymentMethodTags } from "@/components/PaymentMethodTags";
import { loadLastContact, saveLastContact, saveOrder } from "@/lib/orderStorage";

interface Props {
  product: Product | null;
  open: boolean;
  discount?: number;
  user: User | null;
  onOpenChange: (open: boolean) => void;
  onDone: () => void;
}

interface AlipayPaymentCfg {
  enabled: boolean;
  configured: boolean;
  minCny: number;
  maxCny: number;
}

interface CryptoPaymentCfg {
  enabled: boolean;
  configured: boolean;
  rate: number;
  minCny: number;
  chains: { code: string; label: string; display: string }[];
}

interface GuestOrderResult {
  orderNumber: string;
  goodsName: string;
  count: number;
  status: number;
  result: string;
  createdAt: number;
  cards: LocalOrder["cards"];
}

interface GuestOrderView {
  token: string;
  status: "waiting" | "paid" | "fulfilling" | "fulfilled" | "failed" | "expired" | "closed" | "cancelled" | "disputed";
  channel: "alipay" | "usdt";
  productName: string;
  goodsCount: number;
  amountCny: number;
  paymentStatus: string;
  expiresAt: number;
  payableCents: number;
  collectionCode: string;
  address: string;
  usdtAmount: string;
  chain: string;
  chainLabel: string;
  rate: number;
  creditCny: number;
  order: GuestOrderResult | null;
  error?: string;
}

function guestStorageKey(goodsId: number) {
  return `hstore:guest-purchase:${goodsId}`;
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

function Countdown({ expiresAt }: { expiresAt: number }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const remain = Math.max(0, expiresAt - now);
  const m = Math.floor(remain / 60000);
  const s = Math.floor((remain % 60000) / 1000);
  return (
    <span className="font-mono font-semibold text-[#f53f3f]">
      {m}:{String(s).padStart(2, "0")}
    </span>
  );
}

const GUEST_STATUS: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "blue", text: "支付成功，正在发货" },
  fulfilling: { color: "blue", text: "正在发货" },
  fulfilled: { color: "green", text: "已发货" },
  failed: { color: "red", text: "发货失败" },
  expired: { color: "gray", text: "订单已过期" },
  closed: { color: "gray", text: "订单已关闭" },
  cancelled: { color: "red", text: "订单已取消" },
  disputed: { color: "red", text: "支付争议" },
};

function copyText(text: string) {
  navigator.clipboard?.writeText(text);
  Message.success("已复制");
}

function OrderCards({ cards }: { cards: LocalOrder["cards"] }) {
  return (
    <div className="flex flex-col gap-2">
      {cards.length === 0 && (
        <div className="text-[12px] text-[#86909c]">暂无卡券，上游可能仍在发货，请稍后刷新订单。</div>
      )}
      {cards.map((c) => (
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
                copyText([c.cardNo, c.cardPwd].filter(Boolean).join("----"));
              }}
            >
              复制
            </Button>
          </Space>
        </div>
      ))}
    </div>
  );
}

export function PurchaseModal({ product, open, discount = 1, user, onOpenChange, onDone }: Props) {
  const [detail, setDetail] = useState<GoodsDetail | null>(null);
  const [count, setCount] = useState(1);
  const [attach, setAttach] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [order, setOrder] = useState<LocalOrder | null>(null);
  // 下单必填：联系方式（手机 / 邮箱 / QQ），用于事后凭订单号或联系方式找回订单
  const [contact, setContact] = useState("");

  const [guestCfg, setGuestCfg] = useState<{ alipay: AlipayPaymentCfg | null; crypto: CryptoPaymentCfg | null }>({
    alipay: null,
    crypto: null,
  });
  const [guestChannel, setGuestChannel] = useState<"alipay" | "usdt">("alipay");
  const [guestChain, setGuestChain] = useState("TRX");
  const [guestOrder, setGuestOrder] = useState<GuestOrderView | null>(null);
  const [guestBusy, setGuestBusy] = useState(false);
  const [guestQr, setGuestQr] = useState("");
  const [guestRefreshing, setGuestRefreshing] = useState(false);

  const isGuest = !user;
  const alipayReady = Boolean(guestCfg.alipay?.enabled && guestCfg.alipay?.configured);
  const usdtReady = Boolean(guestCfg.crypto?.enabled && guestCfg.crypto?.configured);

  useEffect(() => {
    if (!open || !product) return;
    setDetail(null);
    setOrder(null);
    setGuestOrder(null);
    setGuestQr("");
    setCount(1);
    setAttach({});
    // 优先用上次填过的，其次用账号里的手机号 / 邮箱
    setContact((prev) => prev || loadLastContact() || user?.phone || user?.email || "");
    setLoading(true);

    if (!user) {
      try {
        const raw = localStorage.getItem(guestStorageKey(product.goodsId));
        if (raw) {
          const saved = JSON.parse(raw) as { token?: string; goodsId?: number; at?: number };
          if (saved.token && saved.goodsId === product.goodsId) {
            fetch(`/api/guest/order?token=${encodeURIComponent(saved.token)}`)
              .then((r) => r.json())
              .then((j) => {
                if (j.ok && j.guest) setGuestOrder(j.guest as GuestOrderView);
              })
              .catch(() => {});
          }
        }
      } catch {
        /* ignore */
      }
    }

    fetch(`/api/goods/detail?id=${product.goodsId}`)
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setDetail(j.detail);
        else Message.error(j.message ?? "获取详情失败");
      })
      .catch(() => Message.error("获取详情失败"))
      .finally(() => setLoading(false));
  }, [open, product, user]);

  useEffect(() => {
    if (!open || !isGuest) return;
    Promise.all([
      fetch("/api/alipay/config").then((r) => r.json()).catch(() => null),
      fetch("/api/crypto/config").then((r) => r.json()).catch(() => null),
    ]).then(([aj, cj]) => {
      const next = {
        alipay: aj?.ok ? (aj as AlipayPaymentCfg) : null,
        crypto: cj?.ok ? (cj as CryptoPaymentCfg) : null,
      };
      setGuestCfg(next);
      if (cj?.ok) {
        setGuestChain((prev) =>
          (cj.chains as CryptoPaymentCfg["chains"]).some((c) => c.code === prev) ? prev : cj.defaultChain || "TRX",
        );
      }
      const aReady = Boolean(next.alipay?.enabled && next.alipay?.configured);
      const uReady = Boolean(next.crypto?.enabled && next.crypto?.configured);
      if (!aReady && uReady) setGuestChannel("usdt");
      else if (aReady) setGuestChannel("alipay");
    });
  }, [open, isGuest]);

  useEffect(() => {
    if (!guestOrder || !["waiting", "paid", "fulfilling"].includes(guestOrder.status)) return;
    const token = guestOrder.token;
    const interval = guestOrder.channel === "usdt" ? 5000 : 4000;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    // 链式 setTimeout：上一次请求回来才排下一次，绝不让轮询请求叠加。
    // 之前用 setInterval，后端一慢就会堆到浏览器 6 个并发上限，整页卡死。
    const tick = async () => {
      try {
        const j = await fetch(`/api/guest/order?token=${encodeURIComponent(token)}`).then((r) => r.json());
        if (!stopped && j.ok && j.guest) {
          const next = j.guest as GuestOrderView;
          setGuestOrder(next);
          if (next.status === "fulfilled") {
            if (next.order?.orderNumber) {
              saveOrder({
                orderNumber: next.order.orderNumber,
                contact: loadLastContact(),
                goodsName: next.productName,
                amount: next.amountCny,
              });
            }
            Message.success("支付成功，已自动发货");
            onDone();
            return;
          }
        }
      } catch {
        /* 网络抖动忽略，下一轮再试 */
      }
      if (!stopped) timer = setTimeout(tick, interval);
    };

    timer = setTimeout(tick, interval);
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
    };
  }, [guestOrder?.token, guestOrder?.channel, guestOrder?.status, onDone]);

  const guestQrPayload = guestOrder
    ? guestOrder.channel === "alipay"
      ? guestOrder.collectionCode
      : guestOrder.address
    : "";

  useEffect(() => {
    if (!guestQrPayload) {
      setGuestQr("");
      return;
    }
    QRCode.toDataURL(guestQrPayload, { width: 440, margin: 1, errorCorrectionLevel: "M" })
      .then(setGuestQr)
      .catch(() => setGuestQr(""));
  }, [guestQrPayload]);

  const templates = useMemo(() => (detail?.rechargeTemplates ?? []).filter((t) => t.title), [detail]);
  const unitPrice = product ? Number((product.finalPrice * discount).toFixed(4)) : 0;
  const total = unitPrice * count;
  // 支付宝只能支付到分：不足 1 分向上取整（0.064 → 0.07），展示与后端保持一致
  const alipayTotal = ceilToCents(total);
  const payable = isGuest && guestChannel === "alipay" ? alipayTotal : total;

  function saveGuestToken(token: string, goodsId: number) {
    try {
      localStorage.setItem(guestStorageKey(goodsId), JSON.stringify({ token, goodsId, at: Date.now() }));
    } catch {
      /* ignore */
    }
  }

  function clearGuestOrder() {
    setGuestOrder(null);
    setGuestQr("");
    try {
      if (product) localStorage.removeItem(guestStorageKey(product.goodsId));
    } catch {
      /* ignore */
    }
  }

  async function submitBalance() {
    if (!product) return;
    if (!contact.trim()) {
      Message.warning("请填写联系方式，便于查询订单");
      return;
    }
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
          contact: contact.trim(),
          attach: templates.map((t) => ({ name: t.title, value: attach[t.title] ?? "" })),
        }),
      });
      const j = await res.json();
      if (j.ok) {
        setOrder(j.order);
        saveLastContact(contact.trim());
        if (j.order?.orderNumber) {
          saveOrder({
            orderNumber: j.order.orderNumber,
            contact: contact.trim(),
            goodsName: j.order.goodsName,
            amount: j.order.money,
          });
        }
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

  async function submitGuest() {
    if (!product) return;
    if (guestChannel === "alipay" && !alipayReady) {
      Message.warning("支付宝支付暂不可用");
      return;
    }
    if (guestChannel === "usdt" && !usdtReady) {
      Message.warning("USDT 支付暂不可用");
      return;
    }
    if (!contact.trim()) {
      Message.warning("请填写联系方式，便于查询订单");
      return;
    }
    const missing = templates.find((t) => t.required === 1 && !attach[t.title]);
    if (missing) {
      Message.warning(`请填写「${missing.title}」`);
      return;
    }
    setGuestBusy(true);
    try {
      const res = await fetch("/api/guest/order", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          goodsId: product.goodsId,
          count,
          attach: templates.map((t) => ({ name: t.title, value: attach[t.title] ?? "" })),
          channel: guestChannel,
          chain: guestChannel === "usdt" ? guestChain : "",
          contact: contact.trim(),
        }),
      });
      const j = await res.json();
      if (!j.ok) {
        Message.error(j.message ?? "创建支付订单失败");
        return;
      }
      const view = j.guest as GuestOrderView;
      setGuestOrder(view);
      saveGuestToken(view.token, product.goodsId);
      saveLastContact(contact.trim());
      Message.success("订单已创建，请扫码支付");
    } catch {
      Message.error("创建支付订单失败，请稍后重试");
    } finally {
      setGuestBusy(false);
    }
  }

  async function refreshGuest() {
    if (!guestOrder || guestRefreshing) return;
    setGuestRefreshing(true);
    try {
      const j = await fetch(`/api/guest/order?token=${encodeURIComponent(guestOrder.token)}`).then((r) => r.json());
      if (j.ok && j.guest) {
        const next = j.guest as GuestOrderView;
        setGuestOrder(next);
        if (next.status === "fulfilled") {
          Message.success("支付成功，已自动发货");
          onDone();
        } else if (["waiting", "paid", "fulfilling"].includes(next.status)) {
          Message.info(next.status === "waiting" ? "暂未查询到付款，请确认支付金额" : "已支付，正在等待上游发货");
        }
      }
    } catch {
      Message.error("查询订单失败，请稍后重试");
    } finally {
      setGuestRefreshing(false);
    }
  }

  const guestStatus = guestOrder ? GUEST_STATUS[guestOrder.status] ?? { color: "gray", text: guestOrder.status } : null;
  const guestAmount = guestOrder
    ? guestOrder.channel === "alipay"
      ? `¥${formatPrice(guestOrder.payableCents / 100)}`
      : `${guestOrder.usdtAmount} USDT`
    : "";

  const footer = guestOrder ? (
    guestOrder.status === "fulfilled" ? (
      <Space>
        <Button onClick={clearGuestOrder}>重新购买</Button>
        <Button type="primary" onClick={() => onOpenChange(false)}>完成</Button>
      </Space>
    ) : guestOrder.status === "failed" ? (
      <Space>
        <Button onClick={() => onOpenChange(false)}>关闭</Button>
        <Button type="primary" loading={guestRefreshing} onClick={refreshGuest}>刷新重试</Button>
      </Space>
    ) : guestOrder.status === "disputed" ? (
      <Button onClick={() => onOpenChange(false)}>关闭</Button>
    ) : ["expired", "closed", "cancelled"].includes(guestOrder.status) ? (
      <Space>
        <Button onClick={() => onOpenChange(false)}>关闭</Button>
        <Button type="primary" onClick={clearGuestOrder}>重新购买</Button>
      </Space>
    ) : (
      <Space>
        <Button onClick={() => onOpenChange(false)}>关闭</Button>
        <Button type="primary" loading={guestRefreshing} onClick={refreshGuest}>刷新状态</Button>
      </Space>
    )
  ) : order ? (
    <Button type="primary" onClick={() => onOpenChange(false)}>完成</Button>
  ) : (
    <Space>
      <Button onClick={() => onOpenChange(false)}>取消</Button>
      <Button
        type="primary"
        loading={isGuest ? guestBusy : submitting}
        disabled={!detail || (isGuest && !alipayReady && !usdtReady)}
        onClick={isGuest ? submitGuest : submitBalance}
      >
        {isGuest ? "确认支付" : "确认购买"}
      </Button>
    </Space>
  );

  return (
    <Modal
      title={isGuest ? "游客购买" : "购买商品"}
      visible={open}
      onCancel={() => onOpenChange(false)}
      footer={footer}
      style={{ width: 580 }}
      maskClosable={false}
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
          <OrderCards cards={order.cards} />
        </div>
      ) : guestOrder ? (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <Tag color={guestStatus?.color}>{guestStatus?.text ?? guestOrder.status}</Tag>
            <Tag color={guestOrder.channel === "alipay" ? "arcoblue" : "orange"}>
              {guestOrder.channel === "alipay" ? "支付宝" : "USDT"}
            </Tag>
            <span className="text-[12px] text-[#86909c]">
              {guestOrder.productName} × {guestOrder.goodsCount}
            </span>
          </div>

          {["waiting", "paid", "fulfilling"].includes(guestOrder.status) && (
            <>
              <div className="text-center">
                <div className="text-[12px] text-[#86909c]">
                  {guestOrder.channel === "alipay" ? "请使用支付宝扫一扫，支付精确金额" : "请按以下地址和精确金额转账"}
                </div>
                <div className="mt-2 text-[34px] font-semibold leading-none text-[#f53f3f]">{guestAmount}</div>
                {guestOrder.channel === "alipay" && (
                  <div className="mt-1 text-[11px] text-[#d25f00]">商品直购价，无额外加价</div>
                )}
              </div>

              {guestOrder.status === "waiting" && (
                <div className="flex flex-col items-center">
                  {guestQr ? (
                    <img src={guestQr} alt="支付二维码" className="h-[240px] w-[240px] rounded-lg border border-[#e5e6eb]" />
                  ) : (
                    <div className="flex h-[240px] w-[240px] items-center justify-center rounded-lg bg-[#f7f8fa] text-[12px] text-[#86909c]">
                      二维码加载中…
                    </div>
                  )}
                  <div className="mt-2 text-[12px] text-[#86909c]">
                    支付剩余 <Countdown expiresAt={guestOrder.expiresAt} />
                  </div>
                </div>
              )}

              {guestOrder.status !== "waiting" && (
                <div className="rounded border border-[#bedaff] bg-[#e8f3ff] px-3 py-3 text-center text-[12px] text-[#165dff]">
                  支付已确认，正在自动发货，请稍候…
                </div>
              )}

              {guestOrder.channel === "usdt" && (
                <div>
                  <div className="text-[12px] text-[#86909c]">充值地址（{guestOrder.chainLabel}）</div>
                  <div className="mt-1 flex items-center gap-2">
                    <span className="break-all font-mono text-[12px]">{guestOrder.address}</span>
                    <Button size="mini" onClick={() => copyText(guestOrder.address)}>复制</Button>
                  </div>
                  <div className="mt-1 text-[11px] text-[#d25f00]">
                    必须使用完全相同的 {guestOrder.usdtAmount} USDT 转账
                  </div>
                </div>
              )}

              {guestOrder.channel === "alipay" && (
                <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[11px] leading-relaxed text-[#4e5969]">
                  打开支付宝「扫一扫」→ 扫描二维码 → 输入 <b>¥{formatPrice(guestOrder.payableCents / 100)}</b> →
                  确认付款。支付成功后会自动发货并显示卡密。
                </div>
              )}

              <div className="flex items-center justify-center gap-2 font-mono text-[11px] text-[#86909c]">
                <span className="break-all">游客订单凭证：{guestOrder.token}</span>
                <Button size="mini" onClick={() => copyText(guestOrder.token)}>复制</Button>
              </div>
            </>
          )}

          {guestOrder.status === "fulfilled" && (
            <div className="flex flex-col gap-3">
              <div className="rounded border border-[#aff0b5] bg-[#e8ffea] px-3 py-2 text-[12px] text-[#00b42a]">
                支付成功，已自动发货。
              </div>
              {guestOrder.order ? (
                <>
                  <div className="flex items-center justify-between text-[12px]">
                    <span className="text-[#86909c]">订单号</span>
                    <span className="font-mono">{guestOrder.order.orderNumber}</span>
                  </div>
                  <OrderCards cards={guestOrder.order.cards} />
                </>
              ) : (
                <div className="text-[12px] text-[#86909c]">发货完成，但暂时没有取到卡券，请联系管理员。</div>
              )}
            </div>
          )}

          {guestOrder.status === "failed" && (
            <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
              支付已到账，但自动发货失败：{guestOrder.error || "请稍后刷新或联系管理员"}。
              请勿重复支付。
            </div>
          )}

          {["expired", "closed", "cancelled"].includes(guestOrder.status) && (
            <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
              订单已结束，未完成支付。可以重新购买。
            </div>
          )}

          {guestOrder.status === "disputed" && (
            <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
              支付关联存在争议，请联系管理员核对，请勿重复支付。
            </div>
          )}

          <div className="text-[11px] leading-relaxed text-[#86909c]">
            游客无需注册即可购买。支付成功后请在此窗口查看卡密；凭证会保存在本浏览器，重新打开同一商品即可继续查看，也可以复制凭证联系客服查询。
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
            {discount < 1 && user && (
              <span className="ml-1">（原价 ¥{formatPrice(product?.finalPrice)}，会员 {Math.round(discount * 100)} 折）</span>
            )}
            ，下单后自动发货。
          </div>

          {(detail.goodsDetail || detail.buyNotice) && (
            <details open className="rounded border border-[#e5e6eb] px-3 py-2">
              <summary className="cursor-pointer text-[12px] text-[#4e5969]">商品详情 / 购买须知</summary>
              <div className="mt-2 max-h-[260px] overflow-y-auto text-[12px] leading-relaxed text-[#4e5969]">
                {detail.goodsDetail && (
                  <div className="product-detail" dangerouslySetInnerHTML={{ __html: detail.goodsDetail }} />
                )}
                {detail.buyNotice && <div className="mt-2 whitespace-pre-wrap">购买须知：{detail.buyNotice}</div>}
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

          {/* 下单必填：联系方式。游客丢了页面也能凭它找回订单 */}
          <div className="flex flex-col gap-1 text-[12px]">
            <span className="text-[#86909c]">
              联系方式 <span className="text-[#f53f3f]">*</span>
            </span>
            <Input
              placeholder="手机号 / 邮箱 / QQ，用于查询订单"
              value={contact}
              maxLength={64}
              onChange={setContact}
            />
            <span className="text-[11px] text-[#86909c]">
              下单后可凭订单号或这个联系方式在「订单查询」页找回卡密。
            </span>
          </div>

          {isGuest && (
            <div className="flex flex-col gap-2 rounded border border-[#e5e6eb] p-3">
              <div className="text-[12px] font-medium">支付方式</div>
              <PaymentMethodTags
                value={guestChannel}
                onChange={setGuestChannel}
                alipayReady={alipayReady}
                usdtReady={usdtReady}
              />
              {guestChannel === "usdt" && (
                <Select value={guestChain} onChange={(v) => setGuestChain(String(v))} style={{ width: "100%" }}>
                  {(guestCfg.crypto?.chains ?? []).map((c) => (
                    <Select.Option key={c.code} value={c.code}>{c.label}</Select.Option>
                  ))}
                </Select>
              )}
              {!alipayReady && !usdtReady && (
                <div className="text-[11px] text-[#f53f3f]">管理员尚未配置可用的支付渠道。</div>
              )}
              {guestChannel === "alipay" && (
                <div className="text-[11px] text-[#86909c]">
                  支付宝按商品售价支付，只能支付到分，不足 1 分向上取整
                  {alipayTotal !== Number(total.toFixed(2)) ? (
                    <span className="text-[#d25f00]">
                      （¥{formatPrice(total)} → ¥{formatPrice(alipayTotal)}）
                    </span>
                  ) : null}
                  。
                </div>
              )}
              {guestChannel === "usdt" && guestCfg.crypto?.rate ? (
                <div className="text-[11px] text-[#86909c]">
                  按汇率换算约 {((total || 0) / guestCfg.crypto.rate).toFixed(4)} USDT，实际金额以下单页面为准。
                </div>
              ) : null}
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
              应付 <div className="text-[18px] font-semibold text-[#f53f3f]">¥{formatPrice(payable)}</div>
            </div>
          </div>

          {isGuest && (
            <div className="text-[11px] leading-relaxed text-[#86909c]">
              游客购买无需注册，支付成功后卡密会显示在当前窗口。请勿关闭支付窗口，或保留浏览器会话以便继续查看。
            </div>
          )}
        </div>
      ) : null}
    </Modal>
  );
}
