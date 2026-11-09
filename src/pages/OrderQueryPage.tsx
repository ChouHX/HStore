import { Button, Card, Empty, Input, Message, Pagination, Space, Spin, Tag } from "@arco-design/web-react";
import { IconArrowLeft, IconSearch } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { formatPrice } from "@/lib/format";
import { StoreHeader } from "@/components/store/StoreHeader";
import { useSession } from "@/src/hooks/useSession";
import { loadSavedGuestOrders, loadSavedOrders } from "@/lib/orderStorage";

interface OrderItem {
  kind: "order" | "guest";
  orderNumber: string;
  goodsId?: number;
  goodsName: string;
  goodsImg?: string;
  count: number;
  money: number;
  status: number;
  statusText: string;
  contact: string;
  token?: string;
  createdAt: number;
  cardCount: number;
  error?: string;
}

const PAGE_SIZE = 10;

const STATUS_COLOR: Record<number, string> = {
  0: "gray",
  1: "orange",
  2: "orange",
  3: "green",
  4: "gray",
  5: "gray",
  7: "red",
  8: "red",
};

function fmtTime(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "";
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

// 兑换码形如 XXXX-XXXX-XXXX；订单号形如 HS + 时间 + 混淆串（不含短横线），两者不会混淆。
function looksLikeRedeemCode(v: string): boolean {
  const s = v.trim();
  return /^[0-9A-Za-z]{4}-[0-9A-Za-z]{4}-[0-9A-Za-z]{4}$/.test(s);
}

/**
 * 公开的订单查询页：凭订单号或下单时的联系方式查。
 * 本机存过的订单会直接列出来，不用再输入。
 */
export function OrderQueryPage() {
  const navigate = useNavigate();
  const user = useSession();
  const [keyword, setKeyword] = useState("");
  const [items, setItems] = useState<OrderItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const [page, setPage] = useState(1);

  const loadLocal = useCallback(async () => {
    const local = loadSavedOrders();
    const guests = loadSavedGuestOrders();
    if (local.length === 0 && guests.length === 0) return;
    setLoading(true);
    try {
      // 余额订单按订单号回填；游客单还没进 orders 表，按 token 回填
      const [orderRes, guestRes] = await Promise.all([
        local.length
          ? fetch(
              `/api/orders/query?orderNumbers=${encodeURIComponent(
                local.map((o) => o.orderNumber).slice(0, 50).join(","),
              )}`,
            ).then((r) => r.json())
          : Promise.resolve(null),
        guests.length
          ? fetch(
              `/api/orders/query?tokens=${encodeURIComponent(
                guests.map((o) => o.token).slice(0, 50).join(","),
              )}`,
            ).then((r) => r.json())
          : Promise.resolve(null),
      ]);
      const orderItems: OrderItem[] = orderRes?.ok ? (orderRes.items ?? []) : [];
      const guestItems: OrderItem[] = guestRes?.ok ? (guestRes.items ?? []) : [];
      const orderNos = new Set(orderItems.map((it) => it.orderNumber).filter(Boolean));
      // 游客单发货后会同时出现在 orders 表里，以订单记录为准，避免重复
      const merged = [
        ...orderItems,
        ...guestItems.filter((it) => !it.orderNumber || !orderNos.has(it.orderNumber)),
      ].sort((a, b) => b.createdAt - a.createdAt);
      if (orderItems.length > 0 || guestItems.length > 0) {
        setItems(merged);
        setSearched(true);
        setPage(1);
      }
    } catch {
      /* 静默失败，用户仍可手动查询 */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadLocal();
  }, [loadLocal]);

  async function search(kw?: string) {
    const v = (kw ?? keyword).trim();
    if (!v) {
      Message.warning("请输入订单号、兑换码或联系方式");
      return;
    }
    setLoading(true);
    try {
      const j = await fetch(`/api/orders/query?keyword=${encodeURIComponent(v)}`).then((r) => r.json());
      if (!j.ok) {
        Message.error(j.message ?? "查询失败");
        setItems([]);
      } else {
        setItems(j.items ?? []);
        if ((j.items ?? []).length === 0) Message.info("没有找到对应订单，请确认订单号、兑换码或联系方式");
      }
      setSearched(true);
      setPage(1);
    } catch {
      Message.error("查询失败，请稍后重试");
    } finally {
      setLoading(false);
    }
  }

  function openDetail(it: OrderItem) {
    if (it.kind === "guest" && it.token) {
      navigate(`/g/${encodeURIComponent(it.token)}`);
      return;
    }
    // 兑换码取件单没有联系方式，要把用户输入的兑换码带过去当第二重凭证。
    const params = new URLSearchParams();
    if (it.contact) params.set("contact", it.contact);
    else if (looksLikeRedeemCode(keyword)) params.set("code", keyword.trim());
    const qs = params.toString() ? `?${params.toString()}` : "";
    navigate(`/order/${encodeURIComponent(it.orderNumber)}${qs}`);
  }

  const pageItems = useMemo(() => items.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE), [items, page]);

  return (
    <div className="min-h-screen bg-[#f7f8fa]">
      <StoreHeader user={user ?? null} active="orders" announcements={[]} />

      <main className="mx-auto flex w-full max-w-[1000px] flex-col gap-4 px-4 py-5">
        <Card bordered={false}>
          <div className="flex flex-col gap-3">
            <div className="text-center">
              <div className="text-[15px] font-medium text-[#1d2129]">查订单</div>
              <div className="mt-1 text-[12px] text-[#86909c]">
                输入订单号、兑换码或下单时填写的联系方式（手机 / 邮箱 / QQ），游客也能查。
              </div>
            </div>
            <div className="mx-auto flex w-full max-w-[520px] flex-col gap-2 sm:flex-row">
              <Input
                allowClear
                size="large"
                value={keyword}
                onChange={setKeyword}
                onPressEnter={() => search()}
                placeholder="订单号 或 联系方式"
              />
              <Button type="primary" size="large" icon={<IconSearch />} loading={loading} onClick={() => search()}>
                查询
              </Button>
            </div>
          </div>
        </Card>

        <Card
          bordered={false}
          title={<span className="text-[14px]">订单列表</span>}
          extra={<span className="text-[12px] text-[#86909c]">共 {items.length} 笔</span>}
        >
          <Spin loading={loading} style={{ width: "100%" }}>
            {items.length === 0 ? (
              <Empty
                description={
                  searched ? "没有查到订单，换个订单号或联系方式试试" : "输入订单号或联系方式后点查询"
                }
              />
            ) : (
              <div className="flex flex-col gap-3">
                {pageItems.map((it) => (
                  <div
                    key={`${it.kind}-${it.orderNumber}-${it.token ?? ""}-${it.createdAt}`}
                    className="flex flex-col gap-2 rounded-lg border border-[#e5e6eb] bg-white p-3 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="flex min-w-0 flex-1 items-center gap-3">
                      {/* 商品缩略卡片：图 + 名称，点击可查看该商品 */}
                      <Link
                        to={it.goodsId ? `/?goods=${it.goodsId}` : "/"}
                        className="group flex h-[52px] w-[52px] shrink-0 items-center justify-center overflow-hidden rounded-md bg-[#f7f8fa]"
                        title={it.goodsName}
                      >
                        {it.goodsImg ? (
                          <img
                            src={it.goodsImg}
                            alt={it.goodsName}
                            loading="lazy"
                            className="h-full w-full object-cover"
                            onError={(e) => {
                              e.currentTarget.style.display = "none";
                            }}
                          />
                        ) : (
                          <span className="text-[10px] text-[#c9cdd4]">无图</span>
                        )}
                      </Link>
                      <div className="flex min-w-0 flex-col gap-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Link
                            to={it.goodsId ? `/?goods=${it.goodsId}` : "/"}
                            className="break-words text-[13px] font-medium text-[#1d2129] hover:text-[#165dff]"
                          >
                            {it.goodsName}
                          </Link>
                          <Tag size="small" color={STATUS_COLOR[it.status] ?? "gray"}>
                            {it.statusText}
                          </Tag>
                          {it.kind === "guest" ? (
                            <Tag size="small" color="arcoblue">
                              游客单
                            </Tag>
                          ) : null}
                        </div>
                        <div className="flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-[#86909c]">
                          <span className="font-mono">{it.orderNumber || it.token}</span>
                          <span>× {it.count}</span>
                          <span>¥{formatPrice(it.money)}</span>
                          <span>{fmtTime(it.createdAt)}</span>
                          {it.contact ? <span>联系方式 {it.contact}</span> : null}
                        </div>
                        {it.error ? <div className="text-[11px] text-[#f53f3f]">{it.error}</div> : null}
                      </div>
                    </div>
                    <Space>
                      {it.kind === "guest" && it.token && it.status !== 3 ? (
                        <Button size="small" onClick={() => navigate(`/g/${it.token}`)}>
                          去支付
                        </Button>
                      ) : null}
                      <Button size="small" type="primary" onClick={() => openDetail(it)}>
                        查看详情
                      </Button>
                    </Space>
                  </div>
                ))}
                {items.length > PAGE_SIZE ? (
                  <div className="flex justify-end pt-1">
                    <Pagination
                      size="small"
                      current={page}
                      pageSize={PAGE_SIZE}
                      total={items.length}
                      showTotal
                      onChange={setPage}
                    />
                  </div>
                ) : null}
              </div>
            )}
          </Spin>
        </Card>

      </main>
    </div>
  );
}
