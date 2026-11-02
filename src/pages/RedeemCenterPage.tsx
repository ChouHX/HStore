import { Button, Input, Message, Result, Space, Tag } from "@arco-design/web-react";
import { useState } from "react";
import { Link } from "react-router-dom";

import { StoreHeader } from "@/components/store/StoreHeader";
import { useSession } from "@/src/hooks/useSession";

interface RedeemCard {
  cardId: string;
  cardNo: string;
  cardPwd: string;
}

interface RedeemOrder {
  orderNumber: string;
  goodsName?: string;
  count?: number;
  cards?: RedeemCard[];
}

/** 兑换中心：未登录也可使用，输入兑换码即自动下单取件。 */
export function RedeemCenterPage() {
  const user = useSession();
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [order, setOrder] = useState<RedeemOrder | null>(null);
  const [message, setMessage] = useState("");

  async function submit() {
    const v = code.trim();
    if (!v) {
      Message.warning("请输入兑换码");
      return;
    }
    setLoading(true);
    try {
      const j = await fetch("/api/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code: v }),
      }).then((r) => r.json());
      if (!j.ok) {
        Message.error(j.message ?? "兑换失败");
        return;
      }
      setMessage(j.message ?? "兑换成功");
      setOrder((j.order as RedeemOrder) ?? null);
      Message.success(j.message ?? "兑换成功");
    } finally {
      setLoading(false);
    }
  }

  function copyCards() {
    const cards = order?.cards ?? [];
    if (cards.length === 0) return;
    const text = cards.map((c) => [c.cardNo, c.cardPwd].filter(Boolean).join("----")).join("\n");
    void navigator.clipboard?.writeText(text);
    Message.success("已复制全部卡密");
  }

  const cards = order?.cards ?? [];

  return (
    <div className="min-h-screen bg-[#f5f6f8]">
      <StoreHeader user={user ?? null} announcements={[]} />
      <main className="mx-auto w-full max-w-[720px] px-4 py-8">
        <div className="rounded-lg bg-white p-6 shadow-sm">
          <div className="mb-1 text-[16px] font-semibold text-[#1d2129]">兑换中心</div>
          <div className="mb-5 text-[12px] text-[#86909c]">
            输入兑换码即可自动发货，未登录也可使用；兑换记录可在「订单查询」中查看。
          </div>

          <Space className="w-full">
            <Input
              value={code}
              onChange={setCode}
              placeholder="请输入兑换码，例如 XXXX-XXXX-XXXX-XXXX"
              style={{ width: 360 }}
              onPressEnter={submit}
              allowClear
            />
            <Button type="primary" loading={loading} onClick={submit}>
              立即兑换
            </Button>
          </Space>

          {order && (
            <div className="mt-6 rounded-md border border-[#e5e6eb] p-4">
              <div className="mb-2 flex flex-wrap items-center gap-2">
                <Tag size="small" color="green">
                  {message}
                </Tag>
                <span className="text-[12px] text-[#4e5969]">
                  订单号 <span className="font-mono">{order.orderNumber}</span>
                </span>
                {order.goodsName && (
                  <span className="text-[12px] text-[#4e5969]">商品：{order.goodsName}</span>
                )}
                <span className="text-[12px] text-[#4e5969]">共 {cards.length} 条卡密</span>
              </div>

              {cards.length > 0 ? (
                <>
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-[12px] text-[#86909c]">卡密内容</span>
                    <Space size="small">
                      <Button size="mini" onClick={copyCards}>
                        复制全部
                      </Button>
                      <Link to={`/order/${order.orderNumber}`}>
                        <Button size="mini" type="primary">
                          查看订单
                        </Button>
                      </Link>
                    </Space>
                  </div>
                  <div className="max-h-[360px] overflow-auto rounded bg-[#f7f8fa] p-3">
                    {cards.map((c) => (
                      <div key={c.cardId} className="flex items-center gap-2 py-0.5 font-mono text-[12px]">
                        <span className="text-[#86909c]">{c.cardId}</span>
                        <span className="select-all">
                          {[c.cardNo, c.cardPwd].filter(Boolean).join("----")}
                        </span>
                      </div>
                    ))}
                  </div>
                </>
              ) : (
                <Result
                  status="success"
                  title="兑换成功"
                  subTitle="兑换码已充入余额，请到「个人中心 - 我的余额」查看。"
                />
              )}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
