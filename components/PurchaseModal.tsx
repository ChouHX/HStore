"use client";
import { ceilToCents, formatPrice } from "@/lib/format";

import { Button, Drawer, Input, InputNumber, Message, Modal, Select, Space, Tag } from "@arco-design/web-react";
import QRCode from "qrcode";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import type { GoodsDetail, Product, RechargeTemplate, User } from "@/lib/types";
import { PaymentMethodTags, type PaymentChannel } from "@/components/PaymentMethodTags";
import {
  clearGuestToken,
  loadLastContact,
  saveGuestOrder,
  saveLastContact,
  saveGuestToken,
  saveOrder,
} from "@/lib/orderStorage";

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
  chains: {
    code: string;
    label: string;
    display: string;
    contract?: string;
    evm?: boolean;
    slowConfirm?: boolean;
    eta?: string;
  }[];
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
  /** 该链到账较慢（Arbitrum / Polygon），需要提示等待 */
  chainSlowConfirm?: boolean;
  chainEta?: string;
  rate: number;
  creditCny: number;
  error?: string;
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

export function PurchaseModal({ product, open, discount = 1, user, onOpenChange, onDone }: Props) {
  const [detail, setDetail] = useState<GoodsDetail | null>(null);
  const [count, setCount] = useState(1);
  const [attach, setAttach] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const navigate = useNavigate();
  // 下单必填：联系方式（手机 / 邮箱 / QQ），用于事后凭订单号或联系方式找回订单
  const [contact, setContact] = useState("");

  const [guestCfg, setGuestCfg] = useState<{ alipay: AlipayPaymentCfg | null; crypto: CryptoPaymentCfg | null }>({
    alipay: null,
    crypto: null,
  });
  const [channel, setChannel] = useState<PaymentChannel>(user ? "balance" : "alipay");
  const [guestChain, setGuestChain] = useState("TRX");
  const [guestOrder, setGuestOrder] = useState<GuestOrderView | null>(null);
  const [guestBusy, setGuestBusy] = useState(false);
  const [guestQr, setGuestQr] = useState("");
  const [guestRefreshing, setGuestRefreshing] = useState(false);
  const [guestCfgLoading, setGuestCfgLoading] = useState(false);

  const isGuest = !user;
  // 弹窗/抽屉的显隐由内部接管：先播完收起动画，再通知父组件卸载，
  // 否则父组件立刻 setPurchaseProduct(null) 会让抽屉直接消失、没有过渡。
  const [visible, setVisible] = useState(open);
  useEffect(() => {
    if (open) setVisible(true);
  }, [open]);
  const requestClose = useCallback(() => setVisible(false), []);
  const handleAfterClose = useCallback(() => {
    if (open) onOpenChange(false);
  }, [open, onOpenChange]);

  // 小屏用底部抽屉：商品详情 + 表单 + 支付方式在弹窗里容易超出屏幕
  const [isMobile, setIsMobile] = useState(false);

  useEffect(() => {
    const mq = window.matchMedia("(max-width: 767px)");
    const sync = () => setIsMobile(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);

  const alipayReady = Boolean(guestCfg.alipay?.enabled && guestCfg.alipay?.configured);
  const usdtReady = Boolean(guestCfg.crypto?.enabled && guestCfg.crypto?.configured);

  useEffect(() => {
    if (!open || !product) return;
    setDetail(null);
    setGuestOrder(null);
    setGuestQr("");
    setCount(1);
    setAttach({});
    setChannel(user ? "balance" : "alipay");
    // 优先用上次填过的，其次用账号里的手机号 / 邮箱
    setContact((prev) => prev || loadLastContact() || user?.phone || user?.email || "");
    setLoading(true);

    fetch(`/api/goods/detail?id=${product.goodsId}`)
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setDetail(j.detail);
        else Message.error(j.message ?? "获取详情失败");
      })
      .catch(() => Message.error("获取详情失败"))
      .finally(() => setLoading(false));
  }, [open, product, user]);

  const loadGuestCfg = useCallback(() => {
    setGuestCfgLoading(true);
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
      // 保留用户当前选择；当前渠道不可用时才自动切换
      setChannel((prev) => {
        if (prev === "balance") return prev;
        if (prev === "alipay" ? aReady : uReady) return prev;
        if (aReady) return "alipay";
        if (uReady) return "usdt";
        return prev;
      });
    }).finally(() => setGuestCfgLoading(false));
  }, []);

  useEffect(() => {
    if (!open) return;
    loadGuestCfg();
  }, [open, loadGuestCfg]);

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
            Message.success("支付成功，已自动发货");
            onDone();
            onOpenChange(false);
            navigate(`/g/${encodeURIComponent(next.token)}`);
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
  const payable = channel === "alipay" ? alipayTotal : total;
  const balanceInsufficient = !!user && Number(total.toFixed(2)) > user.balance;

  // USDT 单笔至少 1 USDT：单价太低时自动把数量提上去（与后端 MinUsdtCountForGuest 一致）
  const usdtRate = guestCfg.crypto?.rate ?? 0;
  const usdtMinCount =
    channel === "usdt" && usdtRate > 0 && unitPrice > 0
      ? Math.max(1, Math.ceil(usdtRate / unitPrice))
      : 1;
  const minCount =
    channel === "usdt"
      ? Math.max(detail?.minQuantity || 1, usdtMinCount)
      : detail?.minQuantity || 1;

  // 选中 USDT 时应付金额按后端规则换算：CNY / 汇率，向上取整到 0.01 USDT
  const usdtPayable = usdtRate > 0 ? Math.ceil((payable / usdtRate) * 100) / 100 : 0;

  // 切到 USDT（或数量被改小）时，自动把数量抬到「至少 1 USDT」的下限
  useEffect(() => {
    if (channel !== "usdt") return;
    setCount((c) => (c < usdtMinCount ? usdtMinCount : c));
  }, [channel, usdtMinCount]);

  function clearGuestOrder() {
    setGuestOrder(null);
    setGuestQr("");
    if (product) clearGuestToken(product.goodsId);
  }

  function openOrderPage(token: string) {
    onOpenChange(false);
    navigate(`/g/${encodeURIComponent(token)}`);
  }

  async function submitBalance() {
    if (!product || !user) return;
    if (balanceInsufficient) {
      Message.warning("余额不足，请选择其他支付方式");
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
        saveLastContact(contact.trim());
        // 记入本机订单，订单查询页无需再输入订单号
        if (j.order?.orderNumber) {
          saveOrder({
            orderNumber: j.order.orderNumber,
            contact: contact.trim(),
            goodsName: product.customName || detail?.name,
            amount: total,
          });
        }
        Message.success("下单成功");
        onDone();
        onOpenChange(false);
        if (j.order?.orderNumber) navigate(`/order/${encodeURIComponent(j.order.orderNumber)}`);
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
    if (!product || channel === "balance") return;
    if (channel === "alipay" && !alipayReady) {
      Message.warning("支付宝支付暂不可用");
      return;
    }
    if (channel === "usdt" && !usdtReady) {
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
          channel,
          chain: channel === "usdt" ? guestChain : "",
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
      // 游客单在发货前没有订单号，本机用 token 记录，订单查询页可直接列出
      saveGuestOrder({ token: view.token, goodsName: view.productName, amount: view.amountCny });
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
          onOpenChange(false);
          navigate(`/g/${encodeURIComponent(next.token)}`);
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

  const payDisabled =
    !detail || (channel === "balance" ? !user : channel === "alipay" ? !alipayReady : !usdtReady);
  const payLoading = channel === "balance" ? submitting : guestBusy;
  const submitButton = (
    <Button
      type="primary"
      size={isMobile ? "small" : "default"}
      loading={payLoading}
      disabled={payDisabled}
      onClick={channel === "balance" ? submitBalance : submitGuest}
    >
      {channel === "balance" ? "余额支付" : isMobile ? "立即购买" : "确认支付"}
    </Button>
  );

  const footer = guestOrder ? (
    guestOrder.status === "fulfilled" ? (
      <Space>
        <Button onClick={clearGuestOrder}>重新购买</Button>
        <Button type="primary" onClick={() => openOrderPage(guestOrder.token)}>查看订单详情</Button>
      </Space>
    ) : guestOrder.status === "failed" ? (
      <Space>
        <Button onClick={requestClose}>关闭</Button>
        <Button type="primary" loading={guestRefreshing} onClick={refreshGuest}>刷新重试</Button>
      </Space>
    ) : guestOrder.status === "disputed" ? (
      <Button onClick={requestClose}>关闭</Button>
    ) : ["expired", "closed", "cancelled"].includes(guestOrder.status) ? (
      <Space>
        <Button onClick={requestClose}>关闭</Button>
        <Button type="primary" onClick={clearGuestOrder}>重新购买</Button>
      </Space>
    ) : (
      <Space>
        <Button onClick={() => openOrderPage(guestOrder.token)}>订单详情</Button>
        <Button onClick={requestClose}>关闭</Button>
        <Button type="primary" loading={guestRefreshing} onClick={refreshGuest}>刷新状态</Button>
      </Space>
    )
  ) : isMobile ? null : (
    <Space>
      <Button onClick={requestClose}>取消</Button>
      {submitButton}
    </Space>
  );

  const title = isGuest ? "游客购买" : "购买商品";
  const body = loading ? (
        <div className="py-10 text-center text-[13px] text-[#86909c]">加载中…</div>
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
                  {guestOrder.expiresAt > 0 && (
                    <div className="mt-2 text-[12px] text-[#86909c]">
                      支付剩余 <Countdown expiresAt={guestOrder.expiresAt} />
                    </div>
                  )}
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
                  {guestOrder.chainSlowConfirm && (
                    <div className="mt-2 rounded border border-[#ffcf8b] bg-[#fff7e8] px-2 py-1.5 text-[11px] leading-relaxed text-[#d25f00]">
                      {guestOrder.chainLabel} 到账较慢，通常需要等待{" "}
                      {guestOrder.chainEta || "5~10 分钟"} 才能自动发货。
                      请耐心等待，<b>不要重复转账</b>。
                    </div>
                  )}
                </div>
              )}

              {guestOrder.channel === "alipay" && (
                <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[11px] leading-relaxed text-[#4e5969]">
                  打开支付宝「扫一扫」→ 扫描二维码 → 输入 <b>¥{formatPrice(guestOrder.payableCents / 100)}</b> →
                  确认付款。支付成功后会自动发货并显示卡密。
                </div>
              )}

              <div className="flex items-center justify-center gap-2 font-mono text-[11px] text-[#86909c]">
                <span className="break-all">{isGuest ? "游客订单凭证" : "订单凭证"}：{guestOrder.token}</span>
                <Button size="mini" onClick={() => copyText(guestOrder.token)}>复制</Button>
              </div>
            </>
          )}

          {guestOrder.status === "fulfilled" && (
            <div className="flex flex-col gap-3">
              <div className="rounded border border-[#aff0b5] bg-[#e8ffea] px-3 py-2 text-[12px] text-[#00b42a]">
                支付成功，已自动发货，正在打开订单详情…
              </div>
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
            {isGuest
              ? "游客无需注册即可购买。支付成功后可在「订单详情」页查看卡密；订单凭证会保存在本浏览器，关闭后也能继续查看。"
              : "支付成功后可在「订单详情」页查看卡密；订单也会同步到「我的订单」。"}
          </div>
        </div>
      ) : detail ? (
        <div className="flex flex-col gap-3">
          {/* 商品信息与详情合并成一张卡片，减少纵向占位 */}
          <div className="rounded border border-[#e5e6eb] p-3">
            <div className="flex gap-3">
              {detail.imgUrl && !detail.imgUrl.startsWith("http") && (
                <img
                  src={detail.imgUrl}
                  alt=""
                  className="h-14 w-14 shrink-0 rounded object-cover"
                  onError={(e) => {
                    e.currentTarget.style.display = "none";
                  }}
                />
              )}
              <div className="min-w-0 flex-1">
                <div className="truncate text-[13px] font-medium">{product?.customName || detail.name}</div>
                {(product?.customDesc || detail.goodsDescribe) && (
                  <div className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-[#86909c]">
                    {product?.customDesc || detail.goodsDescribe}
                  </div>
                )}
                <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-[#86909c]">
                  <span>库存 {detail.stockCount}</span>
                  <span>限购 {detail.minQuantity} ~ {detail.maxQuantity}</span>
                  {(detail.warrantyDays ?? 0) > 0 && <span>质保 {detail.warrantyDays} 天</span>}
                </div>
              </div>
            </div>

            {(detail.goodsDetail || detail.buyNotice) && (
              <div className="mt-3 border-t border-[#f2f3f5] pt-2">
                <div className="text-[12px] font-medium text-[#1d2129]">商品详情 / 购买须知</div>
                {/* 标题固定，详情在容器内滚动；移动端更矮一些 */}
                <div className="mt-1.5 max-h-[132px] overflow-y-auto text-[12px] leading-relaxed text-[#4e5969] sm:max-h-[180px]">
                  {detail.goodsDetail && (
                    <div className="product-detail" dangerouslySetInnerHTML={{ __html: detail.goodsDetail }} />
                  )}
                  {detail.buyNotice && (
                    <div className="mt-2 whitespace-pre-wrap">购买须知：{detail.buyNotice}</div>
                  )}
                </div>
              </div>
            )}
          </div>

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

          <div className="flex flex-col gap-2">
            <div className="text-[12px] font-medium">支付方式</div>
            <PaymentMethodTags
              value={channel}
              onChange={setChannel}
              alipayReady={alipayReady}
              usdtReady={usdtReady}
              showBalance={!!user}
              balanceReady={!!user}
            />
            {guestCfgLoading ? (
              <div className="text-[11px] text-[#86909c]">正在加载在线支付方式…</div>
            ) : null}
            {!user && !guestCfgLoading && !alipayReady && !usdtReady ? (
              <div className="flex items-center gap-2 text-[11px] text-[#f53f3f]">
                暂未获取到可用的支付渠道
                <Button size="mini" onClick={loadGuestCfg}>
                  重新加载
                </Button>
              </div>
            ) : null}
            {channel === "balance" && user ? (
              <div className={`text-[11px] ${balanceInsufficient ? "text-[#f53f3f]" : "text-[#86909c]"}`}>
                当前余额 ¥{formatPrice(user.balance)}，本单应付 ¥{formatPrice(total)}
                {balanceInsufficient
                  ? "，余额不足，请选择其他支付方式"
                  : "，确认后立即扣款并自动发货。"}
              </div>
            ) : null}
            {channel === "usdt" ? (
              <>
                <Select value={guestChain} onChange={(v) => setGuestChain(String(v))} style={{ width: "100%" }}>
                  {(guestCfg.crypto?.chains ?? []).map((c) => (
                    <Select.Option key={c.code} value={c.code}>{c.label}</Select.Option>
                  ))}
                </Select>
                {(() => {
                  const c = (guestCfg.crypto?.chains ?? []).find((x) => x.code === guestChain);
                  if (!c?.slowConfirm) return null;
                  return (
                    <div className="mt-1 rounded bg-[#fff7e8] px-2 py-1.5 text-[11px] leading-relaxed text-[#d25f00]">
                      {c.display} 到账较慢，通常需要等待 {c.eta || "5~10 分钟"} 才能自动发货，请耐心等待。
                    </div>
                  );
                })()}
              </>
            ) : null}
            {channel === "alipay" ? (
              <div className="text-[11px] text-[#86909c]">
                支付宝按商品售价支付，只能支付到分，不足 1 分向上取整
                {alipayTotal !== Number(total.toFixed(2)) ? (
                  <span className="text-[#d25f00]">
                    （¥{formatPrice(total)} → ¥{formatPrice(alipayTotal)}）
                  </span>
                ) : null}
                。
              </div>
            ) : null}
            {channel === "usdt" && guestCfg.crypto?.rate ? (
              <div className="text-[11px] text-[#86909c]">
                USDT 单笔至少 1 USDT，本商品至少买 {usdtMinCount} 件
                {usdtMinCount > 1 ? <span className="text-[#d25f00]">（已自动调整数量）</span> : null}；
                按汇率 1 USDT ≈ ¥{formatPrice(guestCfg.crypto.rate)} 换算，金额向上取整到 0.01 USDT。
              </div>
            ) : null}
          </div>

          {/* 移动端：数量 / 应付 / 立即购买压成一行，底部不再重复按钮 */}
          <div className="flex items-center justify-between gap-2">
            <div className="flex shrink-0 items-center gap-1.5 text-[12px] text-[#86909c]">
              数量
              <InputNumber
                value={count}
                onChange={(v) => setCount(Math.max(minCount, Number(v) || 1))}
                min={minCount}
                max={detail.maxQuantity || 999999}
                style={{ width: isMobile ? 84 : 120 }}
              />
            </div>
            <div className="min-w-0 text-right text-[12px] text-[#86909c]">
              应付
              {channel === "usdt" ? (
                <>
                  <div className="text-[16px] font-semibold leading-tight text-[#f53f3f] sm:text-[18px]">
                    {usdtPayable > 0 ? `${usdtPayable.toFixed(2)} USDT` : "—"}
                  </div>
                  <div className="text-[11px] text-[#86909c]">≈ ¥{formatPrice(total)}</div>
                </>
              ) : (
                <div className="text-[16px] font-semibold leading-tight text-[#f53f3f] sm:text-[18px]">
                  ¥{formatPrice(payable)}
                </div>
              )}
            </div>
            {isMobile ? <div className="shrink-0">{submitButton}</div> : null}
          </div>
        </div>
      ) : null;

  // 小屏：底部抽屉，高度固定 90vh，内容区自己滚动，不会被撑出屏幕
  if (isMobile) {
    return (
      <Drawer
        title={title}
        visible={visible}
        onCancel={requestClose}
        afterClose={handleAfterClose}
        getChildrenPopupContainer={() => document.body}
        footer={footer}
        placement="bottom"
        height="90vh"
        maskClosable={false}
        unmountOnExit
        bodyStyle={{ padding: 16, overflowY: "auto" }}
      >
        {body}
      </Drawer>
    );
  }

  return (
    <Modal
      title={title}
      visible={visible}
      onCancel={requestClose}
      afterClose={handleAfterClose}
      getChildrenPopupContainer={() => document.body}
      footer={footer}
      style={{ width: 580 }}
      className="purchase-dialog"
      maskClosable={false}
      unmountOnExit
    >
      {body}
    </Modal>
  );
}
