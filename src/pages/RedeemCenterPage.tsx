import { Button, Input, Message, Result, Space, Tag } from "@arco-design/web-react";
import { IconCopy, IconDownload } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";

import { StoreHeader } from "@/components/store/StoreHeader";
import { formatPrice } from "@/lib/format";
import { ORDER_STATUS } from "@/lib/status";
import type { StoreHome } from "@/lib/api-types";
import type { StoreProduct } from "@/lib/types";
import { useSession } from "@/src/hooks/useSession";
import { isSoldOut } from "@/components/store/selfStock";
import { productImg, productTitle } from "@/components/store/productView";

interface RedeemCard {
  cardNo?: string;
  cardPwd?: string;
}

/** 兑换成功后返回的订单（与订单详情页同源：model.LocalOrder）。 */
interface RedeemOrder {
  orderNumber: string;
  goodsId?: number;
  goodsImg?: string;
  goodsName?: string;
  count?: number;
  money?: number;
  saleMoney?: number;
  status?: number;
  createdAt?: number;
  cards?: RedeemCard[];
}

/** 一条卡密展示成一行，与订单详情页保持完全一致。 */
function cardLine(c: RedeemCard): string {
  return [c.cardNo, c.cardPwd].filter(Boolean).join("----");
}

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

/** 一行一个字段，样式与订单详情页的 InfoRow 相同。 */
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

/** 「随便看看」不按板块拆分：直接从有货板块里随机抽一批商品，两行展示。 */
const PICK_BOARDS = 4;
const PICK_TOTAL = 12;
/** 自营板块 ID（store.SelfCategoryID），其商品永远展示在推荐位 */
const SELF_BOARD_ID = 900001;

/** 从数组里无放回地随机取 n 个元素（不修改原数组）。 */
function sample<T>(arr: T[], n: number): T[] {
  const pool = arr.slice();
  for (let i = pool.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [pool[i], pool[j]] = [pool[j], pool[i]];
  }
  return pool.slice(0, n);
}

/** 兑换中心：未登录也可使用，输入兑换码即自动下单取件。 */
export function RedeemCenterPage() {
  const user = useSession();
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [order, setOrder] = useState<RedeemOrder | null>(null);
  const [message, setMessage] = useState("");
  const [balanceAmount, setBalanceAmount] = useState(0);
  /** 兑换商品的图片：订单接口只回 goods_id，图片由后端随兑换结果一起下发。 */
  const [goodsImg, setGoodsImg] = useState("");
  /** 兑换是否已成功：余额码不会返回订单，用独立标记区分「还没兑换」和「兑换成功」。 */
  const [done, setDone] = useState(false);
  const [picks, setPicks] = useState<StoreProduct[]>([]);
  const [picksLoading, setPicksLoading] = useState(true);

  async function submit() {
    const v = code.trim();
    if (!v) {
      Message.warning("请输入兑换码");
      return;
    }
    setLoading(true);
    // 上一次的兑换结果不再保留，避免新兑换失败时误导用户
    setDone(false);
    setOrder(null);
    setMessage("");
    setBalanceAmount(0);
    setGoodsImg("");
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
      setBalanceAmount(Number(j.amount) || 0);
      setGoodsImg(String(j.goodsImg ?? ""));
      setDone(true);
      Message.success(j.message ?? "兑换成功");
    } finally {
      setLoading(false);
    }
  }

  function copyCards() {
    const cards = order?.cards ?? [];
    if (cards.length === 0) {
      Message.warning("暂无卡券可复制");
      return;
    }
    void navigator.clipboard?.writeText(cards.map(cardLine).join("\n"));
    Message.success("已复制全部卡密");
  }

  function downloadTxt() {
    const cards = order?.cards ?? [];
    if (cards.length === 0) {
      Message.warning("暂无卡券可导出");
      return;
    }
    const blob = new Blob([cards.map(cardLine).join("\n")], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `order-${order?.orderNumber || "cards"}.txt`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    Message.success("已导出 TXT");
  }

  // 推荐位：自营商品永远排在前面（有货优先），其余位置从其他有货板块随机补足。
  const loadPicks = useCallback(async () => {
    setPicksLoading(true);
    try {
      const home = await fetch("/api/store/home").then((r) => r.json());
      if (!home.ok) return;
      const boards = ((home as StoreHome).boards ?? []).filter((b) => b.total > 0);
      if (boards.length === 0) return;

      // 1) 自营板块单独取全量，全部纳入推荐（这是本站主推，不能被随机挤掉）
      const selfBoard = boards.find((b) => b.id === SELF_BOARD_ID);
      let selfPicks: StoreProduct[] = [];
      if (selfBoard) {
        try {
          const sp = new URLSearchParams({
            rootId: String(selfBoard.id),
            page: "1",
            pageSize: "60",
            sort: "stock-desc",
          });
          const j = await fetch(`/api/store/products?${sp}`).then((r) => r.json());
          if (j.ok) {
            const list = (j.items as StoreProduct[]).filter((p) => p.status === 1);
            // 自营内部：有货的排前面
            selfPicks = list.slice().sort((a, b) => Number(isSoldOut(a)) - Number(isSoldOut(b)));
          }
        } catch {
          selfPicks = [];
        }
      }

      const chosen = sample(boards, PICK_BOARDS);
      const lists = await Promise.all(
        chosen.map(async (b) => {
          try {
            const sp = new URLSearchParams({
              rootId: String(b.id),
              page: "1",
              pageSize: "30",
              sort: "stock-desc",
            });
            const j = await fetch(`/api/store/products?${sp}`).then((r) => r.json());
            if (!j.ok) return [];
            return (j.items as StoreProduct[]).filter((p) => p.status === 1 && !isSoldOut(p));
          } catch {
            return [];
          }
        }),
      );
      const pool = lists.flat();
      // 同一商品只出现一次，且自营的已优先占位
      const unique = Array.from(new Map(pool.map((p) => [p.goodsId, p])).values());
      const selfIds = new Set(selfPicks.map((p) => p.goodsId));
      const others = unique.filter((p) => !selfIds.has(p.goodsId));
      const rest = sample(others, Math.max(0, PICK_TOTAL - selfPicks.length));
      setPicks([...selfPicks, ...rest]);
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
      <StoreHeader user={user ?? null} announcements={[]} active="redeem" />
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

          {done && (
            <div className="mt-6 flex flex-col gap-4">
              {order ? (
                <>
                  <div>
                    <div className="mb-1 flex items-center gap-2">
                      <span className="text-[13px] font-medium">订单信息</span>
                      <Tag size="small" color="green">
                        {message}
                      </Tag>
                    </div>
                    <div className="rounded-lg bg-[#f7f8fa] px-3">
                      <InfoRow label="订单号" value={order.orderNumber} />
                      {/* 商品这一行直接渲染成缩略卡片：图 + 名称 + 数量，点击可跳到该商品 */}
                      <div className="flex items-start gap-3 border-b border-[#f2f3f5] py-2 text-[12px]">
                        <span className="w-[76px] shrink-0 leading-5 text-[#86909c]">商品</span>
                        <div className="min-w-0 flex-1">
                          <Link
                            to={order.goodsId ? `/?goods=${order.goodsId}` : "/"}
                            className="group flex items-center gap-2.5 rounded-md border border-[#e5e6eb] bg-white p-2 transition hover:border-[#165dff] hover:shadow-sm"
                          >
                            <div className="h-[44px] w-[44px] shrink-0 overflow-hidden rounded bg-[#f7f8fa]">
                              {goodsImg ? (
                                <img
                                  src={goodsImg}
                                  alt={order.goodsName ?? ""}
                                  className="h-full w-full object-cover"
                                  onError={(e) => {
                                    e.currentTarget.style.display = "none";
                                  }}
                                />
                              ) : (
                                <div className="flex h-full w-full items-center justify-center text-[10px] text-[#c9cdd4]">
                                  无图
                                </div>
                              )}
                            </div>
                            <div className="min-w-0 flex-1">
                              <div
                                className="clamp-2 text-[12px] leading-snug text-[#1d2129] group-hover:text-[#165dff]"
                                title={order.goodsName ?? ""}
                              >
                                {order.goodsName || "—"}
                              </div>
                              <div className="mt-0.5 text-[11px] text-[#86909c]">
                                数量 × {order.count ?? 0}
                              </div>
                            </div>
                            <span className="shrink-0 text-[11px] text-[#86909c] group-hover:text-[#165dff]">
                              查看 →
                            </span>
                          </Link>
                        </div>
                      </div>
                      <InfoRow
                        label="状态"
                        value={ORDER_STATUS[Number(order.status ?? 0)] ?? "—"}
                      />
                      <InfoRow label="下单时间" value={fmtTime(Number(order.createdAt ?? 0))} />
                    </div>
                  </div>

                  <div>
                    <div className="mb-2 flex items-center justify-between">
                      <span className="text-[13px] font-medium">卡券（{cards.length}）</span>
                      {cards.length > 0 ? (
                        <Space size={8}>
                          <Button size="mini" icon={<IconCopy />} onClick={copyCards}>
                            复制全部
                          </Button>
                          <Button size="mini" icon={<IconDownload />} onClick={downloadTxt}>
                            下载 TXT
                          </Button>
                        </Space>
                      ) : null}
                    </div>
                    {cards.length === 0 ? (
                      <div className="rounded border border-[#e5e6eb] px-3 py-4 text-center text-[12px] text-[#86909c]">
                        暂无卡券，上游还在发货，请稍后重新兑换或联系客服。
                      </div>
                    ) : (
                      <div className="flex flex-col gap-2">
                        {/* 与订单详情页一致：只读文本框展示前 10 条，完整内容走下载 */}
                        <Input.TextArea
                          readOnly
                          value={cards.slice(0, 10).map(cardLine).join("\n")}
                          style={{
                            height: 220,
                            resize: "none",
                            fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                            fontSize: 12,
                            lineHeight: "20px",
                          }}
                        />
                        {cards.length > 10 ? (
                          <div className="text-[11px] leading-relaxed text-[#d25f00]">
                            共 {cards.length} 条，文本框仅展示前 10 条；完整内容请点上方「下载
                            TXT」查看。
                          </div>
                        ) : null}
                      </div>
                    )}
                  </div>
                </>
              ) : (
                <Result
                  status="success"
                  title={message || "兑换成功"}
                  subTitle={`${balanceAmount > 0 ? `已充入余额 ¥${formatPrice(balanceAmount)}，` : ""}请到「个人中心 - 我的余额」查看。`}
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
              <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
                {picks.map((p) => {
                  const img = productImg(p);
                  return (
                    <Link
                      key={p.goodsId}
                      // 带上 goods 参数，首页据此自动打开该商品的购买弹窗
                      to={`/?goods=${p.goodsId}`}
                      className="group flex flex-col gap-2 rounded-lg border border-[#e5e6eb] bg-white p-2 shadow-sm transition hover:shadow-md"
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
            )}
          </div>
        )}
      </main>
    </div>
  );
}
