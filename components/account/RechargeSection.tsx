"use client";

import { Button, Card, Input, InputNumber, Message, Select, Space, Tag } from "@arco-design/web-react";
import QRCode from "qrcode";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import type { User } from "@/lib/types";
import { formatPrice } from "@/lib/format";

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
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);

  const [cfg, setCfg] = useState<CryptoCfg | null>(null);
  const [cny, setCny] = useState<number>(20);
  const [chain, setChain] = useState<string>("TRX");
  const [order, setOrder] = useState<CryptoOrderView | null>(null);
  const [redeemUrl, setRedeemUrl] = useState("");
  const [qr, setQr] = useState("");
  const [creating, setCreating] = useState(false);

  const loadUser = useCallback(async () => {
    const j = await fetch("/api/me").then((r) => r.json());
    if (j.ok) setUser(j.user as User);
  }, []);

  useEffect(() => {
    loadUser();
  }, [loadUser]);

  useEffect(() => {
    fetch("/api/site/config")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setRedeemUrl(j.redeemPurchaseUrl || "");
      })
      .catch(() => {});
  }, []);

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
            router.refresh();
            Message.success("充值到账");
          }
        }
      } catch {
        /* ignore */
      }
    }, 8000);
    return () => clearInterval(t);
  }, [order, loadUser, router]);

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
        Message.success(`${j.message}，到账 ¥${formatPrice(j.amount)}`);
        setCode("");
        await loadUser();
        router.refresh();
      } else Message.error(j.message ?? "兑换失败");
    } finally {
      setBusy(false);
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
          <Button size="small" onClick={() => router.push("/account/ledger")}>
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
              <div className="mt-auto pt-4 text-[11px] leading-relaxed text-[#86909c]">
                同一兑换码仅可核销一次；1 分钟内失败 5 次将临时锁定核销功能。
              </div>
            </div>
          </Card>
        </div>

        <div className="lg:col-span-3">
          <Card bordered={false} className="h-full" bodyStyle={{ padding: 16 }}>
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
                    // eslint-disable-next-line @next/next/no-img-element
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
                    <Button size="small" type="primary" onClick={() => router.push("/account/ledger")}>
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
          </Card>
        </div>
      </div>
    </div>
  );
}
