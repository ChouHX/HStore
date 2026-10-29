"use client";

import { Button, Card, Input, InputNumber, Message, Modal, Select, Space, Tabs, Tag } from "@arco-design/web-react";
import QRCode from "qrcode";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { MailboxDrawer, type MailboxTarget } from "@/components/MailboxDrawer";
import { PaymentMethodTags } from "@/components/PaymentMethodTags";
import { formatPrice } from "@/lib/format";

interface AlipayCfg {
  enabled: boolean;
  configured: boolean;
  minCny: number;
  maxCny: number;
  productName: string;
}

interface AlipayOrderView {
  id: number;
  orderNo: string;
  requestedCny: number;
  requestedCents: number;
  payableCents: number;
  receivedCents: number | null;
  status: "waiting" | "paid" | "expired" | "closed" | "disputed";
  paymentBasis: string;
  checkoutStatus: string;
  expiresAt: number;
  credited: number | null;
  alipayOrderNo: string;
  createdAt: number;
  paidAt: number | null;
}

interface AlipayCheckoutView {
  orderNo: string;
  productName: string;
  collectionCode: string;
  requestedCents: number;
  payableCents: number;
  expiresAt: number;
}

interface CryptoCfg {
  enabled: boolean;
  configured: boolean;
  rate: number;
  minUsdt: number;
  minCny: number;
  defaultChain: string;
  chains: { code: string; label: string; display: string }[];
}

interface CryptoOrderView {
  id: number;
  /** 系统生成的支付单号（US 开头），用户端只认这个号 */
  orderNo: string;
  address: string;
  chain: string;
  chainLabel: string;
  requestedCny: number;
  usdtAmount: string;
  rate: number;
  creditCny: number;
  status: "waiting" | "paid" | "expired" | "cancelled";
  expiresAt: number;
  txid?: string | null;
  receivedUsdt?: string | null;
  credited?: number | null;
}

type ActiveOrder =
  | { channel: "alipay"; order: AlipayOrderView; checkout: AlipayCheckoutView | null }
  | { channel: "usdt"; order: CryptoOrderView };

const ACTIVE_STORAGE_KEY = "hstore:recharge-active";

const ORDER_TAG: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  closed: { color: "gray", text: "已关闭" },
  cancelled: { color: "red", text: "已取消" },
  disputed: { color: "red", text: "支付争议" },
};

function centsText(v: number | null | undefined) {
  return v === null || v === undefined ? "-" : `¥${formatPrice(v / 100)}`;
}

function copy(text: string) {
  navigator.clipboard?.writeText(text);
  Message.success("已复制");
}

function Countdown({ expiresAt }: { expiresAt: number }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const remain = Math.max(0, expiresAt - now);
  const m = Math.floor(remain / 60000);
  const s = Math.floor((remain % 60000) / 1000);
  return (
    <span className="font-mono text-[13px] font-semibold text-[#f53f3f]">
      {m}:{String(s).padStart(2, "0")}
    </span>
  );
}

export function RechargeSection({ onRecharged }: { onRecharged?: () => void }) {
  const navigate = useNavigate();
  const [redeemUrl, setRedeemUrl] = useState("");

  const [code, setCode] = useState("");
  const [redeemBusy, setRedeemBusy] = useState(false);
  const [redeemed, setRedeemed] = useState<{ orderNumber: string; message: string } | null>(null);
  const [mailOpen, setMailOpen] = useState(false);
  const [mailTarget, setMailTarget] = useState<MailboxTarget | null>(null);

  const [alipayCfg, setAlipayCfg] = useState<AlipayCfg | null>(null);
  const [cryptoCfg, setCryptoCfg] = useState<CryptoCfg | null>(null);
  const [channel, setChannel] = useState<"alipay" | "usdt">("alipay");
  const [amount, setAmount] = useState(20);
  const [chain, setChain] = useState("TRX");
  const [creating, setCreating] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [active, setActive] = useState<ActiveOrder | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [qr, setQr] = useState("");
  const [restored, setRestored] = useState(false);

  const alipayReady = Boolean(alipayCfg?.enabled && alipayCfg?.configured);
  const usdtReady = Boolean(cryptoCfg?.enabled && cryptoCfg?.configured);
  const ready = alipayReady || usdtReady;
  const minCny = channel === "alipay" ? (alipayCfg?.minCny ?? 1) : (cryptoCfg?.minCny ?? 14.4);
  const maxCny = channel === "alipay" ? (alipayCfg?.maxCny ?? 5000) : 100000;
  const rate = cryptoCfg?.rate ?? 0;
  const estUsdt = rate > 0 ? amount / rate : 0;

  const loadConfigs = useCallback(async () => {
    const [sj, aj, cj] = await Promise.all([
      fetch("/api/site/config").then((r) => r.json()).catch(() => null),
      fetch("/api/alipay/config").then((r) => r.json()).catch(() => null),
      fetch("/api/crypto/config").then((r) => r.json()).catch(() => null),
    ]);
    if (sj?.ok) setRedeemUrl(sj.redeemPurchaseUrl || "");
    if (aj?.ok) setAlipayCfg(aj as AlipayCfg);
    if (cj?.ok) {
      setCryptoCfg(cj as CryptoCfg);
      setChain((prev) => (cj.chains as CryptoCfg["chains"]).some((c) => c.code === prev) ? prev : cj.defaultChain || "TRX");
    }
  }, []);

  useEffect(() => {
    loadConfigs();
  }, [loadConfigs]);

  useEffect(() => {
    try {
      const raw = sessionStorage.getItem(ACTIVE_STORAGE_KEY);
      if (raw) {
        const parsed = JSON.parse(raw) as ActiveOrder;
        if (parsed?.order && (parsed.order.status === "waiting" || parsed.order.status === "paid")) {
          setActive(parsed);
        }
      }
    } catch {
      /* ignore invalid session state */
    } finally {
      setRestored(true);
    }
  }, []);

  useEffect(() => {
    if (!restored) return;
    try {
      if (active) sessionStorage.setItem(ACTIVE_STORAGE_KEY, JSON.stringify(active));
      else sessionStorage.removeItem(ACTIVE_STORAGE_KEY);
    } catch {
      /* storage may be unavailable */
    }
  }, [active, restored]);

  useEffect(() => {
    if (!alipayReady && usdtReady) setChannel("usdt");
    else if (alipayReady && !usdtReady) setChannel("alipay");
  }, [alipayReady, usdtReady]);

  useEffect(() => {
    setAmount((prev) => (prev < minCny ? minCny : prev));
  }, [minCny]);

  const qrPayload = active
    ? active.channel === "alipay"
      ? active.checkout?.collectionCode ?? ""
      : active.order.address
    : "";

  useEffect(() => {
    if (!qrPayload) {
      setQr("");
      return;
    }
    QRCode.toDataURL(qrPayload, { width: 480, margin: 1, errorCorrectionLevel: "M" })
      .then(setQr)
      .catch(() => setQr(""));
  }, [qrPayload]);

  useEffect(() => {
    if (!active || active.order.status !== "waiting") return;
    const currentChannel = active.channel;
    const orderID = active.order.id;
    const orderNo = active.order.orderNo;
    const timer = setInterval(async () => {
      try {
        const j =
          currentChannel === "alipay"
            ? await fetch("/api/alipay/order/check", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ id: orderID }),
              }).then((r) => r.json())
            : await fetch(`/api/crypto/order?orderNo=${encodeURIComponent(orderNo)}`).then((r) => r.json());
        if (!j.ok || !j.order) return;
        if (currentChannel === "alipay") {
          setActive((prev) =>
            prev && prev.channel === "alipay" ? { ...prev, order: j.order as AlipayOrderView } : prev,
          );
        } else {
          setActive((prev) =>
            prev && prev.channel === "usdt" ? { ...prev, order: j.order as CryptoOrderView } : prev,
          );
        }
        if (j.order.status === "paid") {
          Message.success("充值已到账");
          onRecharged?.();
        }
      } catch {
        /* 下一轮继续 */
      }
    }, currentChannel === "alipay" ? 4000 : 5000);
    return () => clearInterval(timer);
  }, [active?.channel, active?.order.id, active?.order.orderNo, active?.order.status, onRecharged]);

  async function submitCode() {
    if (!code.trim()) {
      Message.warning("请输入兑换码");
      return;
    }
    setRedeemBusy(true);
    try {
      const j = await fetch("/api/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code }),
      }).then((r) => r.json());
      if (j.ok) {
        setCode("");
        if (j.kind === "product") {
          setRedeemed({
            orderNumber: String(j.orderNumber ?? ""),
            message: String(j.message ?? "兑换成功"),
          });
          Message.success(String(j.message ?? "兑换成功"));
        } else {
          setRedeemed(null);
          Message.success(`${j.message}，到账 ¥${formatPrice(j.amount)}`);
          onRecharged?.();
        }
      } else Message.error(j.message ?? "兑换失败");
    } finally {
      setRedeemBusy(false);
    }
  }

  async function confirmRecharge() {
    if (!ready) {
      Message.warning("管理员尚未配置充值渠道");
      return;
    }
    if (active?.order.status === "waiting") {
      setModalOpen(true);
      Message.info("请先完成当前充值订单");
      return;
    }
    setCreating(true);
    try {
      if (channel === "alipay") {
        const j = await fetch("/api/alipay/order", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ cny: amount }),
        }).then((r) => r.json());
        if (!j.ok) {
          Message.error(j.message ?? "创建支付宝订单失败");
          return;
        }
        setActive({
          channel: "alipay",
          order: j.order as AlipayOrderView,
          checkout: (j.checkout as AlipayCheckoutView | undefined) ?? null,
        });
        setModalOpen(true);
      } else {
        const j = await fetch("/api/crypto/order", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ cny: amount, chain }),
        }).then((r) => r.json());
        if (!j.ok) {
          Message.error(j.message ?? "创建 USDT 订单失败");
          return;
        }
        setActive({ channel: "usdt", order: j.order as CryptoOrderView });
        setModalOpen(true);
      }
    } catch {
      Message.error("创建充值订单失败，请稍后重试");
    } finally {
      setCreating(false);
    }
  }

  async function refreshActiveNow() {
    if (!active || refreshing) return;
    setRefreshing(true);
    try {
      const j =
        active.channel === "alipay"
          ? await fetch("/api/alipay/order/check", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ id: active.order.id }),
            }).then((r) => r.json())
          : await fetch(`/api/crypto/order?orderNo=${encodeURIComponent(active.order.orderNo)}`).then((r) => r.json());
      if (!j.ok || !j.order) {
        Message.warning(j.message ?? "查询订单失败");
        return;
      }
      if (active.channel === "alipay") {
        setActive((prev) =>
          prev && prev.channel === "alipay" ? { ...prev, order: j.order as AlipayOrderView } : prev,
        );
      } else {
        setActive((prev) =>
          prev && prev.channel === "usdt" ? { ...prev, order: j.order as CryptoOrderView } : prev,
        );
      }
      if (j.order.status === "paid") {
        Message.success("充值已到账");
        onRecharged?.();
      } else if (j.order.status === "waiting") {
        Message.info("暂未查询到账，请确认已按弹窗金额支付");
      }
    } catch {
      Message.error("查询订单失败，请稍后重试");
    } finally {
      setRefreshing(false);
    }
  }

  async function cancelActive() {
    if (!active || cancelling) return;
    setCancelling(true);
    try {
      const endpoint =
        active.channel === "alipay"
          ? `/api/alipay/order?id=${active.order.id}`
          : `/api/crypto/order?orderNo=${encodeURIComponent(active.order.orderNo)}`;
      const j = await fetch(endpoint, { method: "DELETE" }).then((r) => r.json());
      if (!j.ok) {
        Message.error(j.message ?? "取消订单失败");
        return;
      }
      if (active.channel === "alipay") {
        setActive((prev) =>
          prev && prev.channel === "alipay" ? { ...prev, order: j.order as AlipayOrderView } : prev,
        );
      } else {
        setActive((prev) =>
          prev && prev.channel === "usdt" ? { ...prev, order: j.order as CryptoOrderView } : prev,
        );
      }
      Message.success("订单已关闭");
    } finally {
      setCancelling(false);
    }
  }

  function resetActive() {
    setModalOpen(false);
    setActive(null);
    setQr("");
  }

  const activeStatus = active ? ORDER_TAG[active.order.status] ?? { color: "gray", text: active.order.status } : null;
  const activeAmount = active
    ? active.channel === "alipay"
      ? `¥${formatPrice(active.order.payableCents / 100)}`
      : `${active.order.usdtAmount} USDT`
    : "";

  return (
    <div id="account-recharge" className="flex w-full flex-col gap-4">
      <Tabs className="recharge-tabs" defaultActiveTab="online" lazyload={false} destroyOnHide={false}>
        <Tabs.TabPane key="online" title="在线充值" lazyload={false} destroyOnHide={false}>
          <Card bordered={false} bodyStyle={{ padding: 18 }}>
            <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="text-[15px] font-medium">在线充值</div>
                <div className="text-[12px] text-[#86909c]">填写金额并选择渠道，确认后弹出支付二维码</div>
              </div>
              <Space>
                <Tag size="small" color={alipayReady ? "green" : "gray"}>支付宝 {alipayReady ? "可用" : "未配置"}</Tag>
                <Tag size="small" color={usdtReady ? "green" : "gray"}>USDT {usdtReady ? "可用" : "未配置"}</Tag>
              </Space>
            </div>

            {!ready && (
              <div className="mb-4 rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
                管理员尚未配置可用的充值渠道，请联系管理员。
              </div>
            )}

            <div className="grid gap-4 sm:grid-cols-2">
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">充值金额（CNY）</div>
                <InputNumber
                  value={amount}
                  min={minCny}
                  max={maxCny}
                  step={10}
                  precision={2}
                  style={{ width: "100%" }}
                  onChange={(v) => setAmount(Number(v) || 0)}
                />
                <div className="mt-1 text-[11px] text-[#86909c]">
                  单笔 ¥{formatPrice(minCny)} – ¥{formatPrice(maxCny)}
                </div>
              </div>

              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">充值渠道</div>
                <PaymentMethodTags
                  value={channel}
                  alipayReady={alipayReady}
                  usdtReady={usdtReady}
                  onChange={(next) => {
                    if (next === "balance") return;
                    setChannel(next);
                    const nextMin = next === "alipay" ? (alipayCfg?.minCny ?? 1) : (cryptoCfg?.minCny ?? 14.4);
                    if (amount < nextMin) setAmount(nextMin);
                  }}
                />
              </div>

              {channel === "usdt" && (
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">链路</div>
                  <Select value={chain} onChange={(v) => setChain(String(v))} style={{ width: "100%" }}>
                    {(cryptoCfg?.chains ?? []).map((c) => (
                      <Select.Option key={c.code} value={c.code}>
                        {c.label}
                      </Select.Option>
                    ))}
                  </Select>
                </div>
              )}

              <div className="flex flex-wrap gap-2 sm:col-span-2">
                {[20, 50, 100, 200].map((v) => (
                  <Button
                    key={v}
                    size="small"
                    type="secondary"
                    onClick={() => setAmount(Math.max(v, minCny))}
                  >
                    ¥{v}
                  </Button>
                ))}
                {channel === "usdt" && rate > 0 && (
                  <span className="flex items-center text-[12px] text-[#86909c]">
                    预计到账约 <b className="mx-1 text-[#1d2129]">{estUsdt.toFixed(4)} USDT</b>
                  </span>
                )}
              </div>
            </div>

            <div className="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-[#f2f3f5] pt-4">
              <div className="text-[11px] leading-relaxed text-[#86909c]">
                {channel === "alipay"
                  ? "支付宝下单后会生成唯一应付金额，请务必按弹窗金额支付。"
                  : "USDT 下单后请按弹窗地址和精确金额转账，到账后自动入账。"}
              </div>
              <Button
                type="primary"
                size="large"
                loading={creating}
                disabled={!ready || (channel === "alipay" ? !alipayReady : !usdtReady)}
                onClick={confirmRecharge}
              >
                确认充值
              </Button>
            </div>

            {active && !modalOpen && (
              <div className="mt-4 flex flex-wrap items-center justify-between gap-2 rounded border border-[#bedaff] bg-[#e8f3ff] px-3 py-2.5">
                <div className="flex flex-wrap items-center gap-2 text-[12px]">
                  <Tag size="small" color={active.channel === "alipay" ? "arcoblue" : "orange"}>
                    {active.channel === "alipay" ? "支付宝" : "USDT"}
                  </Tag>
                  <span className="font-medium text-[#1d2129]">{activeAmount}</span>
                  {activeStatus && <Tag size="small" color={activeStatus.color}>{activeStatus.text}</Tag>}
                  <span className="font-mono text-[11px] text-[#86909c]">
                    {active.order.orderNo}
                  </span>
                </div>
                <Button size="small" type="primary" onClick={() => setModalOpen(true)}>
                  {active.order.status === "waiting" ? "继续支付" : "查看结果"}
                </Button>
              </div>
            )}
          </Card>
        </Tabs.TabPane>

        <Tabs.TabPane key="redeem" title="兑换码" lazyload={false} destroyOnHide={false}>
          <Card bordered={false} bodyStyle={{ padding: 18 }}>
            <div className="mb-4">
              <div className="text-[15px] font-medium">兑换码充值</div>
              <div className="text-[12px] text-[#86909c]">输入卡密直接充值余额，商品码会自动下单</div>
            </div>
            <div className="max-w-[560px]">
              <Input
                value={code}
                onChange={(v) => setCode(v.toUpperCase())}
                placeholder="XXXX-XXXX-XXXX-XXXX"
                style={{ fontFamily: "monospace", letterSpacing: 1, marginBottom: 12 }}
                onPressEnter={submitCode}
              />
              <div className="flex flex-wrap gap-2">
                <Button type="primary" loading={redeemBusy} onClick={submitCode}>
                  立即兑换
                </Button>
                {redeemUrl && (
                  <Button href={redeemUrl} target="_blank" rel="noreferrer">
                    购买兑换码
                  </Button>
                )}
              </div>

              {redeemed && (
                <div className="mt-4 rounded border border-[#aff0b5] bg-[#e8ffea] px-3 py-2 text-[12px]">
                  <div className="text-[#00b42a]">{redeemed.message}</div>
                  {redeemed.orderNumber && (
                    <div className="mt-1 font-mono text-[11px] text-[#4e5969]">订单号 {redeemed.orderNumber}</div>
                  )}
                  <div className="mt-2 flex gap-2">
                    <Button
                      size="mini"
                      type="primary"
                      onClick={() => {
                        setMailTarget({ orderNumber: redeemed.orderNumber });
                        setMailOpen(true);
                      }}
                    >
                      取件
                    </Button>
                    <Button size="mini" onClick={() => navigate("/account/orders")}>
                      我的订单
                    </Button>
                  </div>
                </div>
              )}

              <div className="mt-4 text-[11px] leading-relaxed text-[#86909c]">
                同一兑换码仅可核销一次；1 分钟内失败 5 次将临时锁定核销功能。
                面值码直接充入余额，商品码自动下单并可在「我的订单」中取件。
              </div>
            </div>
          </Card>
        </Tabs.TabPane>
      </Tabs>

      <Modal
        visible={modalOpen}
        onCancel={() => setModalOpen(false)}
        footer={null}
        style={{ width: 480 }}
        maskClosable={false}
        unmountOnExit
        title={active?.channel === "alipay" ? "支付宝" : active?.channel === "usdt" ? "USDT" : "充值"}
      >
        {active && (
          <div className="pt-1">
            {active.order.status === "waiting" ? (
              <>
                <div className="text-center">
                  <div className="text-[12px] text-[#86909c]">
                    {active.channel === "alipay" ? "请使用支付宝扫一扫，支付以下精确金额" : "请按以下地址和精确金额转账"}
                  </div>
                  <div className="mt-2 text-[34px] font-semibold leading-none text-[#f53f3f]">
                    {active.channel === "alipay"
                      ? `¥${formatPrice(active.order.payableCents / 100)}`
                      : `${active.order.usdtAmount} USDT`}
                  </div>
                  {active.channel === "alipay" && active.checkout?.collectionCode && (
                    <div className="mt-1 text-[11px] text-[#d25f00]">多付或少付都不会自动入账</div>
                  )}
                </div>

                <div className="mt-4 flex flex-col items-center">
                  {qr ? (
                    <img
                      src={qr}
                      alt={active.channel === "alipay" ? "支付宝二维码" : "USDT 地址"}
                      className="h-[240px] w-[240px] rounded-lg border border-[#e5e6eb]"
                    />
                  ) : (
                    <div className="flex h-[240px] w-[240px] items-center justify-center rounded-lg bg-[#f7f8fa] text-[12px] text-[#86909c]">
                      二维码加载中…
                    </div>
                  )}
                  <div className="mt-3 text-[12px] text-[#86909c]">
                    支付剩余 <Countdown expiresAt={active.order.expiresAt} />
                  </div>
                </div>

                {active.channel === "usdt" && (
                  <div className="mt-4">
                    <div className="text-[12px] text-[#86909c]">充值地址（{active.order.chainLabel}）</div>
                    <div className="mt-1 flex items-center gap-2">
                      <span className="break-all font-mono text-[12px]">{active.order.address}</span>
                      <Button size="mini" onClick={() => copy(active.order.address)}>
                        复制
                      </Button>
                    </div>
                    <div className="mt-1 text-[11px] text-[#d25f00]">必须使用完全相同的 {active.order.usdtAmount} USDT 转账</div>
                  </div>
                )}

                {active.channel === "alipay" && (
                  <div className="mt-4 rounded bg-[#f7f8fa] px-3 py-2 text-[11px] leading-relaxed text-[#4e5969]">
                    打开支付宝「扫一扫」→ 扫描经营码 → 输入 <b>¥{formatPrice(active.order.payableCents / 100)}</b> →
                    确认付款。支付后本弹窗会自动检测到账。
                  </div>
                )}

                <div className="mt-4 flex flex-wrap justify-center gap-2">
                  <Button loading={refreshing} onClick={refreshActiveNow}>
                    刷新状态
                  </Button>
                  <Button status="danger" loading={cancelling} onClick={cancelActive}>
                    取消订单
                  </Button>
                  <Button onClick={() => setModalOpen(false)}>稍后支付</Button>
                </div>
                <div className="mt-3 text-center font-mono text-[11px] text-[#86909c]">
                  {active.order.orderNo}
                </div>
              </>
            ) : active.order.status === "paid" ? (
              <div className="py-6 text-center">
                <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-[#e8ffea] text-[30px] text-[#00b42a]">
                  ✓
                </div>
                <div className="mt-4 text-[15px] font-medium">充值成功</div>
                <div className="mt-2 text-[32px] font-semibold text-[#00b42a]">
                  ¥{formatPrice(active.order.credited ?? 0)}
                </div>
                <div className="mt-2 font-mono text-[11px] text-[#86909c]">
                  {active.order.orderNo}
                </div>
                <div className="mt-6 flex justify-center gap-2">
                  <Button type="primary" onClick={resetActive}>
                    继续充值
                  </Button>
                  <Button onClick={() => navigate("/account/ledger")}>余额明细</Button>
                </div>
              </div>
            ) : (
              <div className="py-8 text-center">
                <div className="text-[15px] font-medium">
                  {active.order.status === "expired"
                    ? "订单已过期"
                    : active.order.status === "disputed"
                      ? "支付存在争议"
                      : "订单已关闭"}
                </div>
                <div className="mt-2 text-[12px] text-[#86909c]">
                  如果已经付款但未入账，请保留订单号联系管理员核对。
                </div>
                <div className="mt-5 flex justify-center gap-2">
                  <Button type="primary" onClick={resetActive}>
                    重新充值
                  </Button>
                  <Button onClick={() => navigate("/account/ledger")}>余额明细</Button>
                </div>
              </div>
            )}
          </div>
        )}
      </Modal>

      <MailboxDrawer visible={mailOpen} target={mailTarget} onClose={() => setMailOpen(false)} />
    </div>
  );
}
