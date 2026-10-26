"use client";

import { Button, Tag } from "@arco-design/web-react";
import QRCode from "qrcode";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { formatPrice } from "@/lib/format";

interface CheckoutView {
  orderNo: string;
  productName: string;
  collectionCode: string;
  requestedCents: number;
  payableCents: number;
  paymentStatus: "UNPAID" | "CONFIRMED" | "DISPUTED";
  checkoutStatus: "OPEN" | "EXPIRED" | "CLOSED";
  status: "waiting" | "paid" | "expired" | "closed" | "disputed";
  receivedCents: number | null;
  credited: number | null;
  expiresAt: number;
  createdAt: number;
  paidAt: number | null;
}

function Countdown({ expiresAt }: { expiresAt: number }) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const remain = Math.max(0, expiresAt - now);
  const minutes = Math.floor(remain / 60000);
  const seconds = Math.floor((remain % 60000) / 1000);
  return (
    <span className="font-mono font-semibold text-[#f53f3f]">
      {minutes}:{String(seconds).padStart(2, "0")}
    </span>
  );
}

export function CheckoutPage() {
  const { token = "" } = useParams();
  const navigate = useNavigate();
  const [checkout, setCheckout] = useState<CheckoutView | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [qr, setQr] = useState("");

  const load = useCallback(async () => {
    try {
      const res = await fetch(`/api/alipay/checkout/${encodeURIComponent(token)}`);
      const j = await res.json();
      if (j.ok && j.checkout) {
        setCheckout(j.checkout as CheckoutView);
        setError("");
      } else {
        setError(j.message ?? "订单不存在或链接已失效");
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

  useEffect(() => {
    if (!checkout || checkout.status !== "waiting") return;
    const timer = setInterval(load, 4000);
    return () => clearInterval(timer);
  }, [checkout, load]);

  useEffect(() => {
    if (!checkout?.collectionCode) {
      setQr("");
      return;
    }
    QRCode.toDataURL(checkout.collectionCode, { width: 440, margin: 1, errorCorrectionLevel: "M" })
      .then(setQr)
      .catch(() => setQr(""));
  }, [checkout?.collectionCode]);

  const amount = useMemo(() => {
    if (!checkout) return "0.00";
    return formatPrice(checkout.payableCents / 100);
  }, [checkout]);

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[#f5f6f8] text-[13px] text-[#86909c]">
        正在加载收银台…
      </div>
    );
  }

  if (error || !checkout) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[#f5f6f8] px-4">
        <div className="w-full max-w-[420px] rounded-xl bg-white p-8 text-center shadow-lg">
          <div className="text-[16px] font-medium text-[#1d2129]">收银台不可用</div>
          <div className="mt-2 text-[13px] text-[#86909c]">{error || "订单不存在"}</div>
          <Button className="mt-5" type="primary" onClick={() => navigate("/account/recharge")}>
            返回充值页
          </Button>
        </div>
      </div>
    );
  }

  const isSuccess = checkout.status === "paid";
  const isWaiting = checkout.status === "waiting";

  return (
    <div className="flex min-h-screen items-center justify-center bg-[#f5f6f8] px-4 py-8">
      <div className="w-full max-w-[520px] overflow-hidden rounded-xl bg-white shadow-lg">
        <div className="border-b border-[#f2f3f5] px-6 py-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="text-[15px] font-semibold text-[#1d2129]">支付宝经营码收银台</div>
              <div className="mt-1 text-[12px] text-[#86909c]">{checkout.productName}</div>
            </div>
            <Tag color={isSuccess ? "green" : isWaiting ? "orange" : "gray"}>
              {isSuccess ? "支付成功" : isWaiting ? "等待支付" : checkout.status === "disputed" ? "支付争议" : "订单结束"}
            </Tag>
          </div>
        </div>

        {isSuccess ? (
          <div className="px-6 py-10 text-center">
            <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-[#e8ffea] text-[28px] text-[#00b42a]">
              ✓
            </div>
            <div className="mt-4 text-[15px] font-medium text-[#1d2129]">支付已确认</div>
            <div className="mt-2 text-[32px] font-semibold text-[#00b42a]">
              ¥{formatPrice(checkout.credited ?? checkout.requestedCents / 100)}
            </div>
            <div className="mt-2 text-[12px] text-[#86909c]">
              订单号 <span className="font-mono">{checkout.orderNo}</span>
            </div>
            <div className="mt-6 flex justify-center gap-2">
              <Button
                type="primary"
                onClick={() => navigate(`/account/recharge?alipay=${encodeURIComponent(checkout.orderNo)}`)}
              >
                返回充值页
              </Button>
              <Button onClick={() => navigate("/account/ledger")}>查看余额明细</Button>
            </div>
          </div>
        ) : isWaiting ? (
          <div className="px-6 py-6">
            <div className="text-center">
              <div className="text-[12px] text-[#86909c]">请使用支付宝扫码，并手动输入以下金额</div>
              <div className="mt-2 text-[38px] font-semibold leading-none text-[#f53f3f]">¥{amount}</div>
              <div className="mt-2 text-[12px] text-[#d25f00]">
                必须支付 ¥{amount}，多付或少付都不会自动入账
              </div>
            </div>

            <div className="mt-5 flex flex-col items-center">
              {qr ? (
                <img src={qr} alt="支付宝经营码" className="h-[260px] w-[260px] rounded-lg border border-[#e5e6eb]" />
              ) : (
                <div className="h-[260px] w-[260px] animate-pulse rounded-lg bg-[#f7f8fa]" />
              )}
              <div className="mt-3 text-[12px] text-[#86909c]">
                支付剩余 <Countdown expiresAt={checkout.expiresAt} />
              </div>
            </div>

            <div className="mt-5 rounded-lg bg-[#f7f8fa] px-3 py-2 text-[11px] leading-relaxed text-[#4e5969]">
              打开支付宝「扫一扫」→ 扫描上方经营码 → 输入 <b>¥{amount}</b> → 确认付款。
              支付后本页会自动检测到账，无需手动刷新。
            </div>

            <div className="mt-4 flex justify-center gap-2">
              <Button loading={loading} onClick={load}>
                刷新状态
              </Button>
              <Button onClick={() => navigate("/account/recharge")}>返回充值页</Button>
            </div>

            <div className="mt-4 text-center text-[11px] text-[#86909c]">
              订单号 <span className="font-mono">{checkout.orderNo}</span>
            </div>
          </div>
        ) : (
          <div className="px-6 py-10 text-center">
            <div className="text-[15px] font-medium text-[#1d2129]">
              {checkout.status === "expired" ? "订单已过期" : checkout.status === "disputed" ? "支付存在争议" : "订单已关闭"}
            </div>
            <div className="mt-2 text-[12px] text-[#86909c]">
              如果已经付款但未入账，请保留订单号联系管理员核对。
            </div>
            <div className="mt-5 flex justify-center gap-2">
              <Button type="primary" onClick={() => navigate("/account/recharge")}>
                重新充值
              </Button>
              <Button onClick={load}>刷新状态</Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
