"use client";

import { Button, Card, Input, InputNumber, Message, Select, Space, Tabs, Tag } from "@arco-design/web-react";
import QRCode from "qrcode";
import { useLocation, useNavigate } from "react-router-dom";
import { useCallback, useEffect, useState } from "react";
import type { User } from "@/lib/types";
import { formatPrice } from "@/lib/format";
import { MailboxDrawer, type MailboxTarget } from "@/components/MailboxDrawer";

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

interface PerPayCfg {
  enabled: boolean;
  configured: boolean;
  minCny: number;
  maxCny: number;
  productName: string;
  notifyEnabled: boolean;
  returnEnabled: boolean;
}

interface PerPayOrderView {
  id: number;
  merchantOrderNo: string;
  perpayOrderId: string | null;
  amountCny: number;
  amountCents: number;
  payableCents: number | null;
  receivedCents: number | null;
  status: "creating" | "waiting" | "paid" | "expired" | "closed" | "failed" | "disputed";
  paymentBasis: string;
  checkoutUrl: string;
  checkoutStatus: string;
  checkoutExpiresAt: number | null;
  credited: number | null;
  lastError: string;
  createdAt: number;
  paidAt: number | null;
}

const PERPAY_TAG: Record<string, { color: string; text: string }> = {
  creating: { color: "blue", text: "创建中" },
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  closed: { color: "gray", text: "已关闭" },
  failed: { color: "red", text: "创建失败" },
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

export function RechargeSection() {
  const navigate = useNavigate();
  const location = useLocation();
  const [user, setUser] = useState<User | null>(null);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  // 商品码兑换成功后的取件入口
  const [redeemed, setRedeemed] = useState<{ orderNumber: string; message: string } | null>(null);
  const [mailOpen, setMailOpen] = useState(false);
  const [mailTarget, setMailTarget] = useState<MailboxTarget | null>(null);

  const [cfg, setCfg] = useState<CryptoCfg | null>(null);
  const [cny, setCny] = useState<number>(20);
  const [chain, setChain] = useState<string>("TRX");
  const [order, setOrder] = useState<CryptoOrderView | null>(null);
  const [redeemUrl, setRedeemUrl] = useState("");
  const [qr, setQr] = useState("");
  const [creating, setCreating] = useState(false);

  const [ppCfg, setPpCfg] = useState<PerPayCfg | null>(null);
  const [ppAmount, setPpAmount] = useState(20);
  const [ppOrder, setPpOrder] = useState<PerPayOrderView | null>(null);
  const [ppOrders, setPpOrders] = useState<PerPayOrderView[]>([]);
  const [ppBusy, setPpBusy] = useState(false);

  const loadUser = useCallback(async () => {
    const j = await fetch("/api/me").then((r) => r.json());
    if (j.ok) setUser(j.user as User);
  }, []);

  const loadPerPayOrders = useCallback(async () => {
    try {
      const j = await fetch("/api/perpay/orders?limit=10").then((r) => r.json());
      if (j.ok) setPpOrders((j.orders ?? []) as PerPayOrderView[]);
    } catch {
      /* ignore */
    }
  }, []);

  useEffect(() => {
    loadUser();
    loadPerPayOrders();
  }, [loadUser, loadPerPayOrders]);

  useEffect(() => {
    fetch("/api/site/config")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setRedeemUrl(j.redeemPurchaseUrl || "");
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    fetch("/api/perpay/config")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) {
          setPpCfg(j as PerPayCfg);
          setPpAmount(Math.max(j.minCny ?? 10, 20));
        }
      })
      .catch(() => {});
  }, []);

  // PerPay 收银台返回：?perpay=商户订单号，服务端查单后再展示最终状态。
  useEffect(() => {
    const merchantNo = new URLSearchParams(location.search).get("perpay");
    if (!merchantNo) return;
    let cancelled = false;
    (async () => {
      try {
        const j = await fetch(`/api/perpay/order?merchantOrderNo=${encodeURIComponent(merchantNo)}`).then((r) => r.json());
        if (cancelled) return;
        if (j.ok && j.order) {
          setPpOrder(j.order as PerPayOrderView);
          setPpAmount(j.order.amountCny);
          if (j.warning) Message.warning(String(j.warning));
          if (j.order.status === "paid") {
            await loadUser();
            Message.success("支付宝充值已到账");
          }
          await loadPerPayOrders();
        } else {
          Message.error(j.message ?? "查询支付订单失败");
        }
      } catch {
        if (!cancelled) Message.error("查询支付订单失败");
      } finally {
        if (!cancelled) window.history.replaceState({}, "", location.pathname);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [location.search, location.pathname, loadUser, loadPerPayOrders]);

  useEffect(() => {
    if (!ppOrder || !["waiting", "creating"].includes(ppOrder.status)) return;
    const t = setInterval(async () => {
      try {
        const j = await fetch(`/api/perpay/order?id=${ppOrder.id}`).then((r) => r.json());
        if (j.ok && j.order) {
          setPpOrder(j.order as PerPayOrderView);
          if (j.order.status === "paid") {
            await loadUser();
            await loadPerPayOrders();
            Message.success("支付宝充值已到账");
          }
        }
      } catch {
        /* ignore */
      }
    }, 8000);
    return () => clearInterval(t);
  }, [ppOrder, loadUser, loadPerPayOrders]);

  useEffect(() => {
    fetch("/api/crypto/config")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) {
          setCfg(j as CryptoCfg);
          setChain(j.defaultChain || "TRX");
          setCny(Math.max(j.minCny, 20));
        }
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!order || order.status !== "waiting") return;
    const t = setInterval(async () => {
      try {
        const j = await fetch(`/api/crypto/order?id=${order.id}`).then((r) => r.json());
        if (j.ok && j.order) {
          setOrder(j.order as CryptoOrderView);
          if (j.order.status === "paid") {
            await loadUser();
            Message.success("充值到账");
          }
        }
      } catch {
        /* ignore */
      }
    }, 8000);
    return () => clearInterval(t);
  }, [order, loadUser]);

  useEffect(() => {
    if (!order?.address) {
      setQr("");
      return;
    }
    QRCode.toDataURL(order.address, { width: 180, margin: 1 })
      .then(setQr)
      .catch(() => setQr(""));
  }, [order?.address]);

  async function submitCode() {
    if (!code.trim()) {
      Message.warning("请输入兑换码");
      return;
    }
    setBusy(true);
    try {
      const j = await fetch("/api/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code }),
      }).then((r) => r.json());
      if (j.ok) {
        setCode("");
        if (j.kind === "product") {
          // 商品码：已下单取件，跳转订单/邮箱
          setRedeemed({
            orderNumber: String(j.orderNumber ?? ""),
            message: String(j.message ?? "兑换成功"),
          });
          Message.success(String(j.message ?? "兑换成功"));
        } else {
          setRedeemed(null);
          Message.success(`${j.message}，到账 ¥${formatPrice(j.amount)}`);
          await loadUser();
        }
      } else Message.error(j.message ?? "兑换失败");
    } finally {
      setBusy(false);
    }
  }

  async function createPerPayOrder() {
    if (!ppCfg?.enabled) {
      Message.warning("支付宝充值未开启");
      return;
    }
    if (!ppCfg.configured) {
      Message.warning("管理员尚未配置 PerPay");
      return;
    }
    setPpBusy(true);
    try {
      const j = await fetch("/api/perpay/order", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cny: ppAmount }),
      }).then((r) => r.json());
      if (j.order) {
        setPpOrder(j.order as PerPayOrderView);
        setPpAmount(j.order.amountCny);
      }
      if (j.ok) {
        Message.success("订单已创建，正在打开支付宝收银台");
        const url = String(j.order?.checkoutUrl ?? "");
        if (url) window.location.assign(url);
      } else {
        Message.error(j.message ?? "创建失败");
      }
      await loadPerPayOrders();
    } finally {
      setPpBusy(false);
    }
  }

  async function retryPerPayOrder() {
    if (!ppOrder) return;
    setPpBusy(true);
    try {
      const j = await fetch("/api/perpay/order/retry", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id: ppOrder.id }),
      }).then((r) => r.json());
      if (j.order) setPpOrder(j.order as PerPayOrderView);
      if (j.ok) {
        Message.success("订单已重新创建");
        const url = String(j.order?.checkoutUrl ?? "");
        if (url) window.location.assign(url);
      } else Message.error(j.message ?? "重试失败");
      await loadPerPayOrders();
    } finally {
      setPpBusy(false);
    }
  }

  async function closePerPayOrder() {
    if (!ppOrder) return;
    setPpBusy(true);
    try {
      const j = await fetch(`/api/perpay/order?id=${ppOrder.id}`, { method: "DELETE" }).then((r) => r.json());
      if (j.order) setPpOrder(j.order as PerPayOrderView);
      if (j.ok) {
        Message.success("订单已关闭");
        if (j.order?.status === "closed" || j.order?.status === "expired") setPpOrder(null);
      } else Message.error(j.message ?? "关闭失败");
      await loadPerPayOrders();
    } finally {
      setPpBusy(false);
    }
  }

  async function cancelOrder() {
    if (!order) return;
    try {
      const j = await fetch(`/api/crypto/order?id=${order.id}`, { method: "DELETE" }).then((r) => r.json());
      if (j.ok) {
        Message.success("订单已取消，地址已释放");
        setOrder(null);
      } else Message.error(j.message ?? "取消失败");
    } catch {
      Message.error("取消失败");
    }
  }

  async function createOrder() {
    if (!cfg?.enabled) {
      Message.warning("USDT 充值未开启");
      return;
    }
    if (!cfg.configured) {
      Message.warning("管理员尚未配置 Bybit");
      return;
    }
    setCreating(true);
    try {
      const j = await fetch("/api/crypto/order", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cny, chain }),
      }).then((r) => r.json());
      if (j.ok) {
        setOrder(j.order as CryptoOrderView);
        Message.success("订单已创建，请在 30 分钟内完成转账");
      } else Message.error(j.message ?? "创建失败");
    } finally {
      setCreating(false);
    }
  }

  const rate = cfg?.rate ?? 0;
  const estUsdt = rate > 0 ? cny / rate : 0;
  const cryptoReady = Boolean(cfg?.enabled && cfg?.configured);

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-col gap-4">
      {/* 顶部：余额 */}
      <Card bordered={false} bodyStyle={{ padding: 20 }}>
        <div className="flex flex-col justify-between gap-4 md:flex-row md:items-end">
          <div>
            <div className="text-[13px] text-[#86909c]">账户余额（CNY）</div>
            <div className="mt-1 text-[34px] font-semibold leading-none text-[#f53f3f]">
              ¥{user ? formatPrice(user.balance) : "0"}
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-5 text-[12px] text-[#86909c]">
              <span>
                会员等级{" "}
                <Tag size="small" color="arcoblue" style={{ marginLeft: 4 }}>
                  {user?.levelName || "普通会员"}
                </Tag>
              </span>
              <span>
                累计消费{" "}
                <span className="font-medium text-[#1d2129]">
                  ¥{user ? formatPrice(user.totalSpent) : "0"}
                </span>
              </span>
            </div>
          </div>
          <Button size="small" onClick={() => navigate("/account/ledger")}>
            余额明细
          </Button>
        </div>
      </Card>

      {/* 充值方式：两列对齐 */}
      <div className="grid gap-4 lg:grid-cols-5">
        <div className="lg:col-span-2">
          <Card bordered={false} className="h-full" bodyStyle={{ padding: 16 }}>
            <div className="flex h-full flex-col">
              <div className="mb-1 text-[14px] font-medium">兑换码充值</div>
              <div className="mb-3 text-[12px] text-[#86909c]">输入卡密直接充值到账户余额</div>
              <Input
                value={code}
                onChange={(v) => setCode(v.toUpperCase())}
                placeholder="XXXX-XXXX-XXXX-XXXX"
                style={{ fontFamily: "monospace", letterSpacing: 1, marginBottom: 12 }}
                onPressEnter={submitCode}
              />
              <div className="flex gap-2">
                <Button type="primary" className="flex-1" loading={busy} onClick={submitCode}>
                  立即兑换
                </Button>
                {redeemUrl && (
                  <Button className="flex-1" href={redeemUrl} target="_blank" rel="noreferrer">
                    购买兑换码
                  </Button>
                )}
              </div>
              {redeemed && (
                <div className="mt-3 rounded border border-[#aff0b5] bg-[#e8ffea] px-3 py-2 text-[12px]">
                  <div className="text-[#00b42a]">{redeemed.message}</div>
                  {redeemed.orderNumber && (
                    <div className="mt-1 font-mono text-[11px] text-[#4e5969]">
                      订单号 {redeemed.orderNumber}
                    </div>
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

              <div className="mt-auto pt-4 text-[11px] leading-relaxed text-[#86909c]">
                同一兑换码仅可核销一次；1 分钟内失败 5 次将临时锁定核销功能。
                面值码直接充入余额，商品码自动下单并可在「我的订单」中取件。
              </div>
            </div>
          </Card>
        </div>

        <div className="lg:col-span-3">
          <Card bordered={false} className="h-full" bodyStyle={{ padding: 16 }}>
            <Tabs className="recharge-tabs" defaultActiveTab="alipay" animation lazyload={false} destroyOnHide={false}>
              <Tabs.TabPane key="alipay" title="支付宝扫码" lazyload={false} destroyOnHide={false}>
                <div className="flex flex-col gap-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <div className="text-[14px] font-medium">支付宝扫码充值</div>
                      <div className="text-[12px] text-[#86909c]">
                        {ppCfg
                          ? `由 PerPay 收银台收款，单笔 ¥${formatPrice(ppCfg.minCny)} – ¥${formatPrice(ppCfg.maxCny)}`
                          : "加载中…"}
                      </div>
                    </div>
                    <Tag size="small" color={ppCfg?.enabled && ppCfg?.configured ? "green" : "gray"}>
                      {ppCfg?.enabled && ppCfg?.configured ? "可用" : "不可用"}
                    </Tag>
                  </div>

                  {!ppCfg?.configured && (
                    <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
                      管理员尚未配置 PerPay，支付宝充值暂不可用。
                    </div>
                  )}

                  {!ppOrder && (
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div>
                        <div className="mb-1 text-[12px] text-[#4e5969]">充值金额（CNY）</div>
                        <InputNumber
                          value={ppAmount}
                          min={ppCfg?.minCny ?? 10}
                          max={ppCfg?.maxCny ?? 5000}
                          step={10}
                          precision={2}
                          style={{ width: "100%" }}
                          onChange={(v) => setPpAmount(Number(v) || 0)}
                        />
                      </div>
                      <div className="flex items-end">
                        <Button
                          type="primary"
                          className="w-full"
                          loading={ppBusy}
                          disabled={!ppCfg?.enabled || !ppCfg?.configured}
                          onClick={createPerPayOrder}
                        >
                          支付宝扫码支付
                        </Button>
                      </div>
                      <div className="text-[11px] text-[#86909c] sm:col-span-2">
                        下单后跳转到 PerPay 收银台，用支付宝扫码完成付款；返回本页后会自动查单确认。
                      </div>
                    </div>
                  )}

                  {ppOrder && (
                    <div className="rounded border border-[#e5e6eb] p-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <Tag size="small" color={PERPAY_TAG[ppOrder.status]?.color}>
                          {PERPAY_TAG[ppOrder.status]?.text ?? ppOrder.status}
                        </Tag>
                        <span className="font-mono text-[11px] text-[#86909c]">订单号 {ppOrder.merchantOrderNo}</span>
                      </div>

                      <div className="mt-2 grid gap-1 text-[12px] sm:grid-cols-2">
                        <div>
                          充值金额 <span className="font-semibold text-[#1d2129]">¥{formatPrice(ppOrder.amountCny)}</span>
                        </div>
                        {ppOrder.payableCents != null && (
                          <div>
                            应付金额{" "}
                            <span className="font-semibold text-[#f53f3f]">{centsText(ppOrder.payableCents)}</span>
                          </div>
                        )}
                        {ppOrder.status === "waiting" && ppOrder.checkoutExpiresAt && (
                          <div className="text-[#86909c]">
                            支付剩余 <Countdown expiresAt={ppOrder.checkoutExpiresAt} />
                          </div>
                        )}
                        {ppOrder.credited != null && (
                          <div>
                            已入账{" "}
                            <span className="font-semibold text-[#00b42a]">¥{formatPrice(ppOrder.credited)}</span>
                          </div>
                        )}
                      </div>

                      {ppOrder.status === "waiting" && (
                        <>
                          <div className="mt-3 flex flex-wrap gap-2">
                            {ppOrder.checkoutUrl && (
                              <Button
                                size="small"
                                type="primary"
                                onClick={() => window.location.assign(ppOrder.checkoutUrl)}
                              >
                                打开支付宝收银台
                              </Button>
                            )}
                            <Button size="small" loading={ppBusy} onClick={closePerPayOrder}>
                              关闭订单
                            </Button>
                          </div>
                          <div className="mt-2 text-[11px] leading-relaxed text-[#86909c]">
                            如已完成付款，返回本页或稍等片刻，系统会自动查单确认，无需重复支付。
                            金额必须与收银台显示完全一致。
                          </div>
                        </>
                      )}

                      {ppOrder.status === "creating" && (
                        <div className="mt-2 text-[12px] text-[#86909c]">正在创建 PerPay 订单…</div>
                      )}

                      {ppOrder.status === "paid" && (
                        <div className="mt-3">
                          <Button size="small" type="primary" onClick={() => setPpOrder(null)}>
                            继续充值
                          </Button>
                        </div>
                      )}

                      {ppOrder.status === "disputed" && (
                        <div className="mt-2 rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
                          该笔支付关联已被撤销或进入争议处理，请联系管理员核对。
                        </div>
                      )}

                      {ppOrder.status === "failed" && (
                        <div className="mt-3">
                          <div className="mb-2 break-all rounded bg-[#ffece8] px-3 py-2 text-[11px] text-[#f53f3f]">
                            {ppOrder.lastError || "创建 PerPay 订单失败"}
                          </div>
                          <Space>
                            <Button size="small" type="primary" loading={ppBusy} onClick={retryPerPayOrder}>
                              使用原单号重试
                            </Button>
                            <Button size="small" onClick={() => setPpOrder(null)}>
                              重新下单
                            </Button>
                          </Space>
                        </div>
                      )}

                      {(ppOrder.status === "expired" || ppOrder.status === "closed") && (
                        <div className="mt-3">
                          <div className="mb-2 text-[12px] text-[#86909c]">
                            {ppOrder.status === "expired" ? "收银台已过期。" : "订单已关闭。"}
                          </div>
                          <Button size="small" type="primary" onClick={() => setPpOrder(null)}>
                            重新充值
                          </Button>
                        </div>
                      )}
                    </div>
                  )}

                  {ppOrders.filter((o) => o.id !== ppOrder?.id).length > 0 && (
                    <div>
                      <div className="mb-1 text-[12px] font-medium text-[#4e5969]">最近支付宝订单</div>
                      <div className="divide-y divide-[#f2f3f5] rounded border border-[#e5e6eb] px-3">
                        {ppOrders
                          .filter((o) => o.id !== ppOrder?.id)
                          .slice(0, 5)
                          .map((o) => (
                            <div key={o.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
                              <div className="flex min-w-0 flex-wrap items-center gap-2 text-[11px]">
                                <Tag size="small" color={PERPAY_TAG[o.status]?.color}>
                                  {PERPAY_TAG[o.status]?.text ?? o.status}
                                </Tag>
                                <span className="font-medium text-[#1d2129]">¥{formatPrice(o.amountCny)}</span>
                                <span className="text-[#86909c]">
                                  {new Date(o.createdAt).toLocaleString("zh-CN")}
                                </span>
                              </div>
                              <Button
                                size="mini"
                                onClick={() => {
                                  setPpOrder(o);
                                  setPpAmount(o.amountCny);
                                }}
                              >
                                查看
                              </Button>
                            </div>
                          ))}
                      </div>
                    </div>
                  )}

                  <div className="text-[11px] leading-relaxed text-[#86909c]">
                    收款由自托管 PerPay 完成。系统只以服务端查单或验签通知的确认结果入账，浏览器返回不代表支付成功。
                  </div>
                </div>
              </Tabs.TabPane>

              <Tabs.TabPane key="usdt" title="USDT 链上" lazyload={false} destroyOnHide={false}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="text-[14px] font-medium">USDT 链上充值</div>
                <div className="text-[12px] text-[#86909c]">
                  {cfg ? `按 CNY 下单，1 USDT ≈ ¥${cfg.rate}` : "加载中…"}
                </div>
              </div>
              <Tag size="small" color={cryptoReady ? "green" : "gray"}>
                {cryptoReady ? "可用" : "不可用"}
              </Tag>
            </div>

            {!order && (
              <div className="mt-3 grid gap-3 sm:grid-cols-2">
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">充值金额（CNY）</div>
                  <InputNumber
                    value={cny}
                    min={cfg?.minCny ?? 14.4}
                    step={10}
                    precision={2}
                    style={{ width: "100%" }}
                    onChange={(v) => setCny(Number(v) || 0)}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">选择链路</div>
                  <Select value={chain} onChange={(v) => setChain(String(v))} style={{ width: "100%" }}>
                    {(cfg?.chains ?? []).map((c) => (
                      <Select.Option key={c.code} value={c.code}>
                        {c.label}
                      </Select.Option>
                    ))}
                  </Select>
                </div>
                <div className="flex items-end gap-2 sm:col-span-2">
                  <div className="text-[12px] text-[#86909c]">
                    预计支付{" "}
                    <span className="font-semibold text-[#f53f3f]">{estUsdt.toFixed(4)} USDT</span>
                  </div>
                  <Button
                    type="primary"
                    className="ml-auto"
                    loading={creating}
                    disabled={!cryptoReady}
                    onClick={createOrder}
                  >
                    生成充值订单
                  </Button>
                </div>
              </div>
            )}

            {order && order.status === "waiting" && (
              <div className="mt-3 grid gap-4 sm:grid-cols-[150px_1fr]">
                <div className="flex flex-col items-center gap-2 rounded border border-[#e5e6eb] p-2">
                  {qr ? (
                    <img src={qr} alt="充值地址二维码" className="h-[140px] w-[140px]" />
                  ) : (
                    <div className="h-[140px] w-[140px] bg-[#f7f8fa]" />
                  )}
                  <span className="text-[11px] text-[#86909c]">扫码或复制地址</span>
                </div>

                <div className="flex min-w-0 flex-col gap-2.5">
                  <div className="flex flex-wrap items-center gap-2 text-[12px] text-[#86909c]">
                    <Tag color="arcoblue">等待支付</Tag>
                    <Tag color="arcoblue">{order.chainLabel}</Tag>
                    <span>
                      剩余 <Countdown expiresAt={order.expiresAt} />
                    </span>
                  </div>
                  <div>
                    <div className="text-[12px] text-[#86909c]">请精确转账（{order.chainLabel}）</div>
                    <div className="flex items-center gap-2">
                      <span className="text-[22px] font-semibold text-[#f53f3f]">{order.usdtAmount} USDT</span>
                      <Button size="mini" onClick={() => copy(order.usdtAmount)}>
                        复制
                      </Button>
                    </div>
                    <div className="mt-1 text-[11px] text-[#d25f00]">
                      申请 ¥{formatPrice(order.requestedCny)}，预计到账 ¥{formatPrice(order.creditCny)}
                      ；金额必须完全一致
                    </div>
                  </div>
                  <div className="min-w-0">
                    <div className="text-[12px] text-[#86909c]">充值地址</div>
                    <div className="flex items-center gap-2">
                      <span className="break-all font-mono text-[12px]">{order.address}</span>
                      <Button size="mini" onClick={() => copy(order.address)}>
                        复制
                      </Button>
                    </div>
                  </div>
                  <div className="text-[11px] text-[#86909c]">
                    到账后自动入账，可关闭页面稍后在「余额明细」查看。
                  </div>
                  <div>
                    <Button size="small" status="danger" onClick={cancelOrder}>
                      取消订单并释放地址
                    </Button>
                  </div>
                </div>
              </div>
            )}

            {order && order.status === "paid" && (
              <div className="mt-3 rounded border border-[#aff0b5] bg-[#e8ffea] p-4 text-[13px]">
                充值成功：到账{" "}
                <span className="font-semibold text-[#00b42a]">¥{formatPrice(order.credited ?? 0)}</span>（
                {order.receivedUsdt} USDT）
                <div className="mt-2">
                  <Space>
                    <Button size="small" onClick={() => setOrder(null)}>
                      继续充值
                    </Button>
                    <Button size="small" type="primary" onClick={() => navigate("/account/ledger")}>
                      查看余额明细
                    </Button>
                  </Space>
                </div>
              </div>
            )}

            {order && order.status === "expired" && (
              <div className="mt-3 rounded border border-[#ffcf8b] bg-[#fff7e8] p-4 text-[13px]">
                订单已过期（30 分钟未到账），地址已释放。
                <div className="mt-2">
                  <Button size="small" type="primary" onClick={() => setOrder(null)}>
                    重新生成
                  </Button>
                </div>
              </div>
            )}
              </Tabs.TabPane>
            </Tabs>
          </Card>
        </div>
      </div>

      <MailboxDrawer visible={mailOpen} target={mailTarget} onClose={() => setMailOpen(false)} />
    </div>
  );
}
