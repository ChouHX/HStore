import { Button, Card, Input, Message, Space, Spin, Tag } from "@arco-design/web-react";
import { IconArrowLeft, IconCopy, IconDownload, IconRefresh } from "@arco-design/web-react/icon";
import QRCode from "qrcode";
import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { formatPrice } from "@/lib/format";
import { clearGuestToken, loadLastContact, saveOrder } from "@/lib/orderStorage";
import { ORDER_STATUS, arcoColor, orderStatusColor } from "@/lib/status";
import { useSession } from "@/src/hooks/useSession";

interface CardItem {
  cardId: string;
  cardNo: string;
  cardPwd: string;
  status: number;
  cancelRequest?: number;
}

interface GuestOrderView {
  token: string;
  status: string;
  channel: "alipay" | "usdt";
  goodsId: number;
  productName: string;
  goodsCount: number;
  amountCny: number;
  expiresAt: number;
  payableCents: number;
  collectionCode: string;
  address: string;
  usdtAmount: string;
  chainLabel: string;
  createdAt: number;
  order: {
    orderNumber: string;
    goodsName: string;
    count: number;
    cards: CardItem[];
  } | null;
  error?: string;
}

interface OrderView {
  orderNumber: string;
  /** 上游（天启）订单号，仅管理员可见 */
  upstreamOrderNo?: string;
  goodsName: string;
  count: number;
  money: number;
  statusText: string;
  statusColor: string;
  createdAt: number;
  contact: string;
  remark: string;
  cards: CardItem[];
  guest: GuestOrderView | null;
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

function fmtTime(ms: number): string {
  if (!ms) return "—";
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function copy(text: string) {
  navigator.clipboard?.writeText(text);
  Message.success("已复制");
}

function cardLine(c: CardItem): string {
  return [c.cardNo, c.cardPwd].filter(Boolean).join("----");
}

function toGuestView(g: GuestOrderView): OrderView {
  const st = GUEST_STATUS[g.status] ?? { color: "gray", text: g.status };
  return {
    orderNumber: g.order?.orderNumber || "",
    goodsName: g.productName,
    count: g.goodsCount,
    money: g.amountCny,
    statusText: st.text,
    statusColor: st.color,
    createdAt: g.createdAt,
    contact: loadLastContact(),
    remark: g.error || "",
    cards: g.order?.cards ?? [],
    guest: g,
  };
}

/** 一行一个字段，默认不换行；商品名等长文本允许换行 */
function InfoRow({ label, value, wrap }: { label: string; value: string; wrap?: boolean }) {
  return (
    <div className="flex items-start gap-3 border-b border-[#f2f3f5] py-2 text-[12px] last:border-b-0">
      <span className="w-[76px] shrink-0 text-[#86909c]">{label}</span>
      <div className={`min-w-0 flex-1 ${wrap ? "break-words" : "overflow-x-auto"}`}>
        <div className={wrap ? "whitespace-pre-wrap" : "whitespace-nowrap"}>{value || "—"}</div>
      </div>
    </div>
  );
}

/**
 * 统一订单详情页：
 *   /g/:token         游客购买后的订单（含待支付）
 *   /order/:orderNumber  我的订单 / 订单查询点「查看详情」
 * 两个入口共用同一套订单信息展示。
 */
export function OrderDetailPage() {
  const { token = "", orderNumber = "" } = useParams();
  const [search] = useSearchParams();
  const contact = search.get("contact") || "";
  const navigate = useNavigate();
  const user = useSession();

  const [view, setView] = useState<OrderView | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [qr, setQr] = useState("");


  const load = useCallback(
    async (mode: "initial" | "silent" | "manual" = "silent") => {
      if (mode === "initial") setLoading(true);
      if (mode === "manual") setRefreshing(true);
      try {
        if (token) {
          const j = await fetch(`/api/guest/order?token=${encodeURIComponent(token)}`).then((r) => r.json());
          if (j.ok && j.guest) {
            setView(toGuestView(j.guest as GuestOrderView));
            setError("");
          } else if (mode !== "silent") {
            setError(j.message ?? "订单不存在或凭证已失效");
          }
        } else if (orderNumber) {
          const j = contact
            ? await fetch(
                `/api/orders/query/detail?${new URLSearchParams({ orderNumber, contact }).toString()}`,
              ).then((r) => r.json())
            : await fetch("/api/order/detail", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ orderNumber }),
              }).then((r) => r.json());
          if (j.ok && j.order) {
            const o = j.order;
            const status = Number(o.status ?? 0);
            setView({
              orderNumber: o.orderNumber,
              upstreamOrderNo: o.upstreamOrderNo,
              goodsName: o.goodsName,
              count: o.count,
              money: Number(o.saleMoney || o.money || 0),
              statusText: ORDER_STATUS[status] ?? String(o.statusText ?? status),
              statusColor: arcoColor(orderStatusColor(status)),
              createdAt: o.createdAt,
              contact: o.contact ?? contact,
              remark: o.result ?? "",
              cards: o.cards ?? [],
              guest: null,
            });
            setError("");
          } else if (mode !== "silent") {
            setError(j.message ?? "订单详情加载失败");
          }
        } else {
          setError("缺少订单号");
        }
      } catch {
        if (mode === "manual") Message.error("刷新失败，请稍后重试");
        else if (mode === "initial") setError("网络错误，请稍后重试");
      } finally {
        if (mode === "initial") setLoading(false);
        if (mode === "manual") setRefreshing(false);
      }
    },
    [token, orderNumber, contact],
  );

  useEffect(() => {
    load("initial");
  }, [load]);

  // 游客待支付 / 发货中：链式轮询，避免请求叠加
  useEffect(() => {
    if (!token || !view?.guest) return;
    if (!["waiting", "paid", "fulfilling"].includes(view.guest.status)) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const tick = async () => {
      await load("silent");
      if (!stopped) timer = setTimeout(tick, 4000);
    };
    timer = setTimeout(tick, 4000);
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
    };
  }, [token, view?.guest?.status, load]);

  // 游客单发货后记入本机订单，订单查询页可直接看到
  useEffect(() => {
    if (view?.guest?.status !== "fulfilled" || !view.orderNumber) return;
    saveOrder({
      orderNumber: view.orderNumber,
      contact: loadLastContact(),
      goodsName: view.goodsName,
      amount: view.money,
    });
  }, [view?.guest?.status, view?.orderNumber, view?.goodsName, view?.money]);

  const qrPayload = view?.guest
    ? view.guest.channel === "alipay"
      ? view.guest.collectionCode
      : view.guest.address
    : "";

  useEffect(() => {
    if (!qrPayload) {
      setQr("");
      return;
    }
    QRCode.toDataURL(qrPayload, { width: 440, margin: 1, errorCorrectionLevel: "M" })
      .then(setQr)
      .catch(() => setQr(""));
  }, [qrPayload]);

  const guest = view?.guest ?? null;
  const pending = guest ? ["waiting", "paid", "fulfilling"].includes(guest.status) : false;
  const amount = guest
    ? guest.channel === "alipay"
      ? `¥${formatPrice(guest.payableCents / 100)}`
      : `${guest.usdtAmount} USDT`
    : `¥${formatPrice(view?.money ?? 0)}`;

  function downloadTxt() {
    if (!view) return;
    if (view.cards.length === 0) {
      Message.warning("暂无卡券可导出");
      return;
    }
    const blob = new Blob([view.cards.map(cardLine).join("\n")], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `order-${view.orderNumber || "cards"}.txt`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    Message.success("已导出 TXT");
  }

  async function voidCard(cardId: string) {
    try {
      const j = await fetch("/api/card/void", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cardId }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("卡券作废成功");
        await load("manual");
      } else {
        Message.error(j.message ?? "作废失败");
      }
    } catch {
      Message.error("作废失败，请稍后重试");
    }
  }

  function rebuy() {
    if (guest?.goodsId) clearGuestToken(guest.goodsId);
    navigate("/");
  }

  return (
    <div className="min-h-screen bg-[#f7f8fa]">
      <header className="sticky top-0 z-20 border-b border-[#e5e6eb] bg-white">
        <div className="mx-auto flex h-14 max-w-[860px] items-center justify-between gap-3 px-4">
          <Link to="/" className="flex shrink-0 items-center gap-2 text-[16px] font-semibold">
            <img src="/favicon.svg" alt="HShop" className="h-7 w-7 rounded-md" />
            HShop
            <span className="text-[13px] font-normal text-[#86909c]">订单详情</span>
          </Link>
          <div className="flex items-center gap-2">
            <Button size="small" onClick={() => navigate("/orders")}>
              订单查询
            </Button>
            <Button size="small" icon={<IconArrowLeft />} onClick={() => navigate("/")}>
              返回商城
            </Button>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-[860px] px-4 py-5">
        {loading ? (
          <div className="flex justify-center py-20">
            <Spin />
          </div>
        ) : error || !view ? (
          <Card bordered={false}>
            <div className="py-10 text-center">
              <div className="text-[15px] font-medium">订单不可用</div>
              <div className="mt-2 text-[12px] text-[#86909c]">{error}</div>
              <Button className="mt-4" type="primary" onClick={() => navigate("/orders")}>
                去订单查询
              </Button>
            </div>
          </Card>
        ) : (
          <Card bordered={false}>
            <div className="flex flex-col gap-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <Tag color={view.statusColor}>{view.statusText}</Tag>
                  {guest ? (
                    <Tag color={guest.channel === "alipay" ? "arcoblue" : "orange"}>
                      {guest.channel === "alipay" ? "支付宝" : "USDT"}
                    </Tag>
                  ) : null}
                  {view.orderNumber ? (
                    <span className="font-mono text-[12px] text-[#4e5969]">{view.orderNumber}</span>
                  ) : null}
                </div>
                <div className="flex items-center gap-2">
                  <Button size="small" icon={<IconRefresh />} loading={refreshing} onClick={() => load("manual")}>
                    刷新
                  </Button>
                </div>
              </div>

              {pending && guest ? (
                <div className="rounded-lg border border-[#e5e6eb] px-4 py-3">
                  <div className="text-center">
                    <div className="text-[12px] text-[#86909c]">
                      {guest.channel === "alipay" ? "支付宝扫码支付" : "按以下地址转账"}
                    </div>
                    <div className="mt-2 text-[34px] font-semibold leading-none text-[#f53f3f]">{amount}</div>
                    {guest.expiresAt ? (
                      <div className="mt-1 text-[11px] text-[#86909c]">有效期至 {fmtTime(guest.expiresAt)}</div>
                    ) : null}
                  </div>

                  {guest.status === "waiting" ? (
                    <div className="mt-3 flex justify-center">
                      {qr ? (
                        <img src={qr} alt="支付二维码" className="h-[220px] w-[220px] rounded-lg border border-[#e5e6eb]" />
                      ) : (
                        <div className="h-[220px] w-[220px] animate-pulse rounded-lg bg-[#f7f8fa]" />
                      )}
                    </div>
                  ) : (
                    <div className="mt-3 rounded border border-[#bedaff] bg-[#e8f3ff] px-3 py-3 text-center text-[12px] text-[#165dff]">
                      支付已确认，正在自动发货，请稍候…
                    </div>
                  )}

                  {guest.channel === "usdt" ? (
                    <div className="mt-3 rounded bg-[#f7f8fa] px-3 py-2">
                      <div className="text-[12px] text-[#86909c]">充值地址（{guest.chainLabel}）</div>
                      <div className="mt-1 flex items-center gap-2">
                        <span className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap font-mono text-[12px]">
                          {guest.address}
                        </span>
                        <Button size="mini" icon={<IconCopy />} onClick={() => copy(guest.address)} />
                      </div>
                    </div>
                  ) : null}
                </div>
              ) : null}

              {guest?.status === "fulfilled" ? (
                <div className="rounded border border-[#aff0b5] bg-[#e8ffea] px-3 py-2 text-[12px] text-[#00b42a]">
                  支付成功，已自动发货。
                </div>
              ) : null}
              {guest?.status === "failed" ? (
                <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
                  支付已到账，但自动发货失败：{guest.error || "请稍后刷新或联系管理员"}。请勿重复支付。
                </div>
              ) : null}
              {guest && ["expired", "closed", "cancelled"].includes(guest.status) ? (
                <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
                  订单已结束，未完成支付。
                </div>
              ) : null}
              {guest?.status === "disputed" ? (
                <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
                  支付关联存在争议，请联系管理员核对，请勿重复支付。
                </div>
              ) : null}

              <div>
                <div className="mb-1 text-[13px] font-medium">订单信息</div>
                <div className="rounded-lg bg-[#f7f8fa] px-3">
                  <InfoRow label="订单号" value={view.orderNumber} />
                  {user?.role === "admin" && view.upstreamOrderNo ? (
                    <InfoRow label="上游单号" value={view.upstreamOrderNo} />
                  ) : null}
                  <InfoRow label="商品" value={`${view.goodsName} × ${view.count}`} wrap />
                  <InfoRow label="金额" value={`¥${formatPrice(view.money)}`} />
                  <InfoRow label="状态" value={view.statusText} />
                  <InfoRow label="下单时间" value={fmtTime(view.createdAt)} />
                  {view.contact ? <InfoRow label="联系方式" value={view.contact} /> : null}
                  {view.remark ? <InfoRow label="备注" value={view.remark} /> : null}
                </div>
              </div>

              <div>
                <div className="mb-2 flex items-center justify-between">
                  <span className="text-[13px] font-medium">卡券（{view.cards.length}）</span>
                  {view.cards.length > 0 ? (
                    <Space size={8}>
                      <Button
                        size="mini"
                        icon={<IconCopy />}
                        onClick={() => copy(view.cards.map(cardLine).join("\n"))}
                      >
                        复制全部
                      </Button>
                      <Button size="mini" icon={<IconDownload />} onClick={downloadTxt}>
                        下载 TXT
                      </Button>
                    </Space>
                  ) : null}
                </div>
                {view.cards.length === 0 ? (
                  <div className="rounded border border-[#e5e6eb] px-3 py-4 text-center text-[12px] text-[#86909c]">
                    暂无卡券，未支付或上游还在发货，请稍后刷新。
                  </div>
                ) : (
                  <div className="flex flex-col gap-2">
                    {/* 卡券统一放在一个只读文本框里，默认一屏 10 行，避免每行一个容器 */}
                    <Input.TextArea
                      readOnly
                      value={view.cards.slice(0, 10).map(cardLine).join("\n")}
                      style={{
                        height: 220,
                        resize: "none",
                        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                        fontSize: 12,
                        lineHeight: "20px",
                      }}
                    />
                    {view.cards.length > 10 ? (
                      <div className="text-[11px] leading-relaxed text-[#d25f00]">
                        共 {view.cards.length} 条，文本框仅展示前 10 条；完整内容请点上方「下载 TXT」查看。
                      </div>
                    ) : null}
                    {(() => {
                      const cancelable = view.cards.filter(
                        (c) => user && c.cancelRequest === 1 && c.status !== 2,
                      );
                      if (cancelable.length === 0) return null;
                      return (
                        <div className="flex flex-wrap items-center gap-2 text-[11px] text-[#86909c]">
                          可作废卡券：
                          {cancelable.map((c) => (
                            <Button
                              key={c.cardId}
                              size="mini"
                              status="danger"
                              onClick={() => voidCard(c.cardId)}
                            >
                              {c.cardNo || c.cardId}
                            </Button>
                          ))}
                        </div>
                      );
                    })()}
                  </div>
                )}
              </div>

              <div className="flex justify-center gap-2">
                <Button onClick={rebuy}>{guest ? "重新购买" : "再买一次"}</Button>
                <Button type="primary" onClick={() => navigate("/orders")}>
                  订单查询
                </Button>
              </div>
            </div>
          </Card>
        )}
      </main>
    </div>
  );
}
