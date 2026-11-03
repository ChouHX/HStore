import { Button, Input, Message, Result, Space, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";

import { StoreHeader } from "@/components/store/StoreHeader";
import { formatPrice } from "@/lib/format";
import type { StoreHome } from "@/lib/api-types";
import type { Product } from "@/lib/types";
import { useSession } from "@/src/hooks/useSession";
import { isSoldOut } from "@/components/store/selfStock";
import { productImg, productTitle } from "@/components/store/productView";

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

/** 随机推荐区块的数量与每个板块取几件商品。 */
const PICK_BOARDS = 3;
const PICK_PER_BOARD = 6;

/** 从数组里无放回地随机取 n 个元素（不修改原数组）。 */
function sample<T>(arr: T[], n: number): T[] {
  const pool = arr.slice();
  for (let i = pool.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [pool[i], pool[j]] = [pool[j], pool[i]];
  }
  return pool.slice(0, n);
}

interface BoardPick {
  boardId: number;
  boardName: string;
  products: Product[];
}

/** 兑换中心：未登录也可使用，输入兑换码即自动下单取件。 */
export function RedeemCenterPage() {
  const user = useSession();
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [order, setOrder] = useState<RedeemOrder | null>(null);
  const [message, setMessage] = useState("");
  const [picks, setPicks] = useState<BoardPick[]>([]);
  const [picksLoading, setPicksLoading] = useState(true);

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

  // 每次进入页面都重新随机：随机挑若干个有货板块，各自随机取几件商品。
  const loadPicks = useCallback(async () => {
    setPicksLoading(true);
    try {
      const home = await fetch("/api/store/home").then((r) => r.json());
      if (!home.ok) return;
      const boards = ((home as StoreHome).boards ?? []).filter((b) => b.total > 0);
      if (boards.length === 0) return;
      const chosen = sample(boards, PICK_BOARDS);
      const out = await Promise.all(
        chosen.map(async (b) => {
          try {
            const sp = new URLSearchParams({
              rootId: String(b.id),
              page: "1",
              pageSize: "30",
              sort: "stock-desc",
            });
            const j = await fetch(`/api/store/products?${sp}`).then((r) => r.json());
            if (!j.ok) return null;
            const list = (j.items as Product[]).filter((p) => p.status === 1 && !isSoldOut(p));
            if (list.length === 0) return null;
            return { boardId: b.id, boardName: b.name, products: sample(list, PICK_PER_BOARD) };
          } catch {
            return null;
          }
        }),
      );
      setPicks(out.filter((x): x is BoardPick => x !== null));
    } catch {
      // 推荐位失败不影响兑换主流程
    } finally {
      setPicksLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadPicks();
  }, [loadPicks]);

  const cards = order?.cards ?? [];

  return (
    <div className="min-h-screen bg-[#f7f8fa]">
      <StoreHeader user={user ?? null} announcements={[]} />
      <main className="mx-auto flex w-full max-w-[1000px] flex-col gap-4 px-4 py-5">
        <div className="rounded-lg bg-white p-6 shadow-sm">
          <div className="flex flex-col gap-3">
            <div className="text-center">
              <div className="text-[15px] font-medium text-[#1d2129]">兑换中心</div>
              <div className="mt-1 text-[12px] text-[#86909c]">
                输入兑换码即可自动发货，未登录也可使用；兑换记录可在「订单查询」中查看。
              </div>
            </div>
            <div className="mx-auto flex w-full max-w-[520px] flex-col gap-2 sm:flex-row">
              <Input
                allowClear
                size="large"
                value={code}
                onChange={setCode}
                placeholder="请输入兑换码，例如 XXXX-XXXX-XXXX-XXXX"
                onPressEnter={submit}
              />
              <Button type="primary" size="large" loading={loading} onClick={submit}>
                立即兑换
              </Button>
            </div>
          </div>

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
                      {/* 兑换码取件单没有联系方式，带上兑换码才能在详情页校验通过 */}
                      <Link to={`/order/${order.orderNumber}?code=${encodeURIComponent(code.trim())}`}>
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

        {/* 随机推荐：每次进入页面都换一批板块与商品 */}
        {(picksLoading || picks.length > 0) && (
          <div className="mt-6">
            <div className="mb-3 flex items-center justify-between">
              <div className="text-[14px] font-semibold text-[#1d2129]">随便看看</div>
              <Button size="mini" loading={picksLoading} onClick={() => void loadPicks()}>
                换一批
              </Button>
            </div>

            {picksLoading && picks.length === 0 ? (
              <div className="rounded-lg bg-white p-8 text-center text-[13px] text-[#86909c] shadow-sm">
                正在挑选商品…
              </div>
            ) : (
              <div className="flex flex-col gap-4">
                {picks.map((g) => (
                  <div key={g.boardId} className="rounded-lg bg-white p-4 shadow-sm">
                    <div className="mb-3 flex items-center gap-2">
                      <span className="text-[13px] font-semibold text-[#1d2129]">{g.boardName}</span>
                      <Link to="/" className="text-[12px] text-[#165dff] hover:underline">
                        去商城逛逛
                      </Link>
                    </div>
                    <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
                      {g.products.map((p) => {
                        const img = productImg(p);
                        return (
                          <Link
                            key={p.goodsId}
                            to="/"
                            className="group flex flex-col gap-2 rounded border border-[#e5e6eb] bg-white p-2 transition hover:shadow-md"
                          >
                            <div className="h-[72px] w-full shrink-0 overflow-hidden rounded bg-[#f7f8fa]">
                              {img && (
                                <img
                                  src={img}
                                  alt=""
                                  loading="lazy"
                                  className="h-full w-full object-contain"
                                  onError={(e) => {
                                    e.currentTarget.style.display = "none";
                                  }}
                                />
                              )}
                            </div>
                            <div
                              className="clamp-2 text-[12px] leading-snug text-[#1d2129] group-hover:text-[#165dff]"
                              title={productTitle(p)}
                            >
                              {productTitle(p)}
                            </div>
                            <div className="mt-auto text-[14px] font-semibold text-[#f53f3f]">
                              <span className="text-[11px]">¥</span>
                              {formatPrice(p.finalPrice)}
                            </div>
                          </Link>
                        );
                      })}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </main>
    </div>
  );
}
