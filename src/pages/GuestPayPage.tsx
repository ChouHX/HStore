import { Button, Card, Message, Tag } from "@arco-design/web-react";
import { IconArrowLeft, IconCopy } from "@arco-design/web-react/icon";
import QRCode from "qrcode";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { formatPrice } from "@/lib/format";
import { saveOrder } from "@/lib/orderStorage";

interface GuestOrderView {
  token: string;
  status: string;
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
  chainLabel: string;
  order: {
    orderNumber: string;
    goodsName: string;
    count: number;
    cards: { cardId: string; cardNo: string; cardPwd: string; status: number }[];
  } | null;
  error?: string;
}

const STATUS_TEXT: Record<string, { color: string; text: string }> = {
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

function fmtTime(ms: number) {
  if (!ms) return "";
  return new Date(ms).toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** 游客支付页：凭 token 打开，关掉购买弹窗后也能继续扫码/取件。 */
export function GuestPayPage() {
  const { token = "" } = useParams();
  const navigate = useNavigate();
  const [view, setView] = useState<GuestOrderView | null>(null);
  const [error, setError] = useState("");
  const [qr, setQr] = useState("");
  const [loading, setLoading] = useState(true);
  const stopped = useRef(false);

  const load = useCallback(async () => {
    try {
      const j = await fetch(`/api/guest/order?token=${encodeURIComponent(token)}`).then((r) => r.json());
      if (j.ok && j.guest) {
        setView(j.guest as GuestOrderView);
        setError("");
      } else {
        setError(j.message ?? "订单不存在或凭证已失效");
      }
    } catch {
      setError("网络错误，请稍后重试");
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    load();
  }, [load]);

  // 链式轮询，避免请求叠加
  useEffect(() => {
    if (!view || !["waiting", "paid", "fulfilling"].includes(view.status)) return;
    stopped.current = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const tick = async () => {
      await load();
      if (!stopped.current) timer = setTimeout(tick, 4000);
    };
    timer = setTimeout(tick, 4000);
    return () => {
      stopped.current = true;
      if (timer) clearTimeout(timer);
    };
  }, [view?.status, load]);

  useEffect(() => {
    const payload = view ? (view.channel === "alipay" ? view.collectionCode : view.address) : "";
    if (!payload) {
      setQr("");
      return;
    }
    QRCode.toDataURL(payload, { width: 440, margin: 1, errorCorrectionLevel: "M" })
      .then(setQr)
      .catch(() => setQr(""));
  }, [view?.collectionCode, view?.address, view?.channel]);

  useEffect(() => {
    if (view?.status === "fulfilled" && view.order?.orderNumber) {
      saveOrder({
        orderNumber: view.order.orderNumber,
        contact: "",
        goodsName: view.productName,
        amount: view.amountCny,
      });
    }
  }, [view?.status, view?.order?.orderNumber, view?.productName, view?.amountCny]);

  const status = view ? (STATUS_TEXT[view.status] ?? { color: "gray", text: view.status }) : null;
  const amount = view ? (view.channel === "alipay" ? `¥${formatPrice(view.payableCents / 100)}` : `${view.usdtAmount} USDT`) : "";

  return (
    <div className="min-h-screen bg-[#f7f8fa]">
      <header className="sticky top-0 z-20 border-b border-[#e5e6eb] bg-white">
        <div className="mx-auto flex h-14 max-w-[720px] items-center justify-between gap-3 px-4">
          <Link to="/" className="flex shrink-0 items-center gap-2 text-[16px] font-semibold">
            <img src="/favicon.svg" alt="HShop" className="h-7 w-7 rounded-md" />
            HShop
            <span className="text-[13px] font-normal text-[#86909c]">游客订单</span>
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

      <main className="mx-auto max-w-[720px] px-4 py-5">
        {loading ? (
          <div className="py-16 text-center text-[13px] text-[#86909c]">加载中…</div>
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
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center gap-2">
                <Tag color={status?.color}>{status?.text}</Tag>
                <Tag color={view.channel === "alipay" ? "arcoblue" : "orange"}>
                  {view.channel === "alipay" ? "支付宝" : "USDT"}
                </Tag>
                <span className="text-[12px] text-[#86909c]">
                  {view.productName} × {view.goodsCount}
                </span>
              </div>

              {["waiting", "paid", "fulfilling"].includes(view.status) ? (
                <>
                  <div className="text-center">
                    <div className="text-[12px] text-[#86909c]">
                      {view.channel === "alipay" ? "支付宝扫码支付" : "按以下地址转账"}
                    </div>
                    <div className="mt-2 text-[34px] font-semibold leading-none text-[#f53f3f]">{amount}</div>
                    {view.expiresAt ? (
                      <div className="mt-1 text-[11px] text-[#86909c]">有效期至 {fmtTime(view.expiresAt)}</div>
                    ) : null}
                  </div>

                  {view.status === "waiting" ? (
                    <div className="flex justify-center">
                      {qr ? (
                        <img
                          src={qr}
                          alt="支付二维码"
                          className="h-[240px] w-[240px] rounded-lg border border-[#e5e6eb]"
                        />
                      ) : (
                        <div className="h-[240px] w-[240px] animate-pulse rounded-lg bg-[#f7f8fa]" />
                      )}
                    </div>
                  ) : (
                    <div className="rounded border border-[#bedaff] bg-[#e8f3ff] px-3 py-3 text-center text-[12px] text-[#165dff]">
                      支付已确认，正在自动发货，请稍候…
                    </div>
                  )}

                  {view.channel === "usdt" ? (
                    <div className="rounded bg-[#f7f8fa] px-3 py-2">
                      <div className="text-[12px] text-[#86909c]">充值地址（{view.chainLabel}）</div>
                      <div className="mt-1 flex items-center gap-2">
                        <span className="break-all font-mono text-[12px]">{view.address}</span>
                        <Button
                          size="mini"
                          icon={<IconCopy />}
                          onClick={() => {
                            navigator.clipboard?.writeText(view.address);
                            Message.success("已复制");
                          }}
                        />
                      </div>
                    </div>
                  ) : null}
                </>
              ) : null}

              {view.status === "fulfilled" ? (
                <div className="flex flex-col gap-2">
                  <div className="rounded border border-[#aff0b5] bg-[#e8ffea] px-3 py-2 text-[12px] text-[#00b42a]">
                    支付成功，已自动发货。
                  </div>
                  {view.order?.orderNumber ? (
                    <div className="flex items-center justify-between text-[12px]">
                      <span className="text-[#86909c]">订单号</span>
                      <span className="font-mono">{view.order.orderNumber}</span>
                    </div>
                  ) : null}
                  {(view.order?.cards ?? []).map((c) => (
                    <div key={c.cardId} className="rounded border border-[#e5e6eb] px-3 py-2">
                      <div className="break-all font-mono text-[12px]">{c.cardNo}</div>
                      {c.cardPwd ? (
                        <div className="mt-1 break-all font-mono text-[12px] text-[#86909c]">{c.cardPwd}</div>
                      ) : null}
                      <div className="mt-2 flex justify-end">
                        <Button
                          size="mini"
                          icon={<IconCopy />}
                          onClick={() => {
                            navigator.clipboard?.writeText([c.cardNo, c.cardPwd].filter(Boolean).join("----"));
                            Message.success("已复制");
                          }}
                        >
                          复制
                        </Button>
                      </div>
                    </div>
                  ))}
                  {(view.order?.cards ?? []).length === 0 ? (
                    <div className="text-[12px] text-[#86909c]">发货完成但暂无卡券，请联系管理员。</div>
                  ) : null}
                </div>
              ) : null}

              {view.status === "failed" ? (
                <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
                  支付已到账，但自动发货失败：{view.error || "请稍后刷新或联系管理员"}。请勿重复支付。
                </div>
              ) : null}

              {["expired", "closed", "cancelled"].includes(view.status) ? (
                <div className="rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
                  订单已结束，未完成支付。
                </div>
              ) : null}

              <div className="flex justify-center gap-2">
                <Button loading={loading} onClick={load}>
                  刷新状态
                </Button>
                <Button onClick={() => navigate("/orders")}>订单查询</Button>
              </div>
            </div>
          </Card>
        )}
      </main>
    </div>
  );
}
