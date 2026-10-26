import {
  Button,
  Card,
  Drawer,
  Empty,
  Input,
  Message,
  Pagination,
  Space,
  Spin,
  Tag,
} from "@arco-design/web-react";
import { IconArrowLeft, IconCopy, IconFile, IconSearch } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { formatPrice } from "@/lib/format";
import { loadLastContact, loadSavedOrders, removeSavedOrder, type SavedOrder } from "@/lib/orderStorage";

interface OrderItem {
  kind: "order" | "guest";
  orderNumber: string;
  goodsName: string;
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

interface CardItem {
  cardId: string;
  cardNo: string;
  cardPwd: string;
  status: number;
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

/**
 * 公开的订单查询页：凭订单号或下单时的联系方式查。
 * 本机存过的订单会直接列出来，不用再输入。
 */
export function OrderQueryPage() {
  const navigate = useNavigate();
  const [keyword, setKeyword] = useState("");
  const [items, setItems] = useState<OrderItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const [page, setPage] = useState(1);
  const [fromLocal, setFromLocal] = useState(false);
  const [saved, setSaved] = useState<SavedOrder[]>([]);

  const [detail, setDetail] = useState<OrderItem | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailOrder, setDetailOrder] = useState<{
    orderNumber: string;
    goodsName: string;
    count: number;
    money: number;
    statusText: string;
    createdAt: number;
    contact: string;
    cards: CardItem[];
    remark?: string;
  } | null>(null);

  const loadLocal = useCallback(async () => {
    const local = loadSavedOrders();
    setSaved(local);
    if (local.length === 0) return;
    setLoading(true);
    try {
      const nums = local.map((o) => o.orderNumber).slice(0, 50);
      const j = await fetch(`/api/orders/query?orderNumbers=${encodeURIComponent(nums.join(","))}`).then((r) =>
        r.json(),
      );
      if (j.ok) {
        setItems(j.items ?? []);
        setFromLocal(true);
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
      Message.warning("请输入订单号或联系方式");
      return;
    }
    setLoading(true);
    setFromLocal(false);
    try {
      const j = await fetch(`/api/orders/query?keyword=${encodeURIComponent(v)}`).then((r) => r.json());
      if (!j.ok) {
        Message.error(j.message ?? "查询失败");
        setItems([]);
      } else {
        setItems(j.items ?? []);
        if ((j.items ?? []).length === 0) Message.info("没有找到对应订单，请确认订单号或联系方式");
      }
      setSearched(true);
      setPage(1);
    } catch {
      Message.error("查询失败，请稍后重试");
    } finally {
      setLoading(false);
    }
  }

  async function openDetail(it: OrderItem) {
    setDetail(it);
    setDetailOpen(true);
    setDetailOrder(null);
    setDetailLoading(true);
    try {
      if (it.kind === "guest" && it.token) {
        const j = await fetch(`/api/guest/order?token=${encodeURIComponent(it.token)}`).then((r) => r.json());
        if (j.ok && j.guest) {
          const g = j.guest;
          setDetailOrder({
            orderNumber: g.order?.orderNumber || it.orderNumber || "",
            goodsName: g.productName,
            count: g.goodsCount,
            money: g.amountCny,
            statusText: it.statusText,
            createdAt: it.createdAt,
            contact: it.contact,
            cards: g.order?.cards ?? [],
            remark: g.error,
          });
        } else {
          Message.error(j.message ?? "订单详情加载失败");
        }
      } else {
        const qs = new URLSearchParams({ orderNumber: it.orderNumber, contact: it.contact || "" });
        const j = await fetch(`/api/orders/query/detail?${qs.toString()}`).then((r) => r.json());
        if (j.ok && j.order) {
          setDetailOrder({
            orderNumber: j.order.orderNumber,
            goodsName: j.order.goodsName,
            count: j.order.count,
            money: j.order.saleMoney || j.order.money,
            statusText: it.statusText,
            createdAt: j.order.createdAt,
            contact: j.order.contact,
            cards: j.order.cards ?? [],
            remark: j.order.result,
          });
        } else {
          Message.error(j.message ?? "订单详情加载失败");
        }
      }
    } catch {
      Message.error("订单详情加载失败");
    } finally {
      setDetailLoading(false);
    }
  }

  const pageItems = useMemo(() => items.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE), [items, page]);

  function forget(orderNumber: string) {
    removeSavedOrder(orderNumber);
    setSaved(loadSavedOrders());
  }

  return (
    <div className="min-h-screen bg-[#f7f8fa]">
      <header className="sticky top-0 z-20 border-b border-[#e5e6eb] bg-white">
        <div className="mx-auto flex h-14 max-w-[1000px] items-center justify-between gap-3 px-4">
          <Link to="/" className="flex shrink-0 items-center gap-2 text-[16px] font-semibold">
            <img src="/favicon.svg" alt="HShop" className="h-7 w-7 rounded-md" />
            HShop
            <span className="text-[13px] font-normal text-[#86909c]">订单查询</span>
          </Link>
          <Button size="small" icon={<IconArrowLeft />} onClick={() => navigate("/")}>
            返回商城
          </Button>
        </div>
      </header>

      <main className="mx-auto flex max-w-[1000px] flex-col gap-4 px-4 py-5">
        <Card bordered={false}>
          <div className="flex flex-col gap-3">
            <div>
              <div className="text-[15px] font-medium text-[#1d2129]">查订单</div>
              <div className="mt-1 text-[12px] text-[#86909c]">
                输入订单号或下单时填写的联系方式（手机 / 邮箱 / QQ），游客也能查。
              </div>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row">
              <Input
                allowClear
                size="large"
                value={keyword}
                onChange={setKeyword}
                onPressEnter={() => search()}
                placeholder="订单号 或 联系方式"
                style={{ maxWidth: 460 }}
              />
              <Button type="primary" size="large" icon={<IconSearch />} loading={loading} onClick={() => search()}>
                查询
              </Button>
            </div>
            {saved.length > 0 ? (
              <div className="flex flex-wrap items-center gap-2 text-[12px] text-[#86909c]">
                <span>本机记录 {saved.length} 笔</span>
                <Button size="mini" onClick={loadLocal}>
                  重新载入
                </Button>
                {loadLastContact() ? (
                  <Button size="mini" onClick={() => search(loadLastContact())}>
                    按联系方式 {loadLastContact()} 查
                  </Button>
                ) : null}
              </div>
            ) : null}
          </div>
        </Card>

        <Card
          bordered={false}
          title={
            <span className="text-[14px]">
              订单列表
              {fromLocal ? <span className="ml-2 text-[12px] font-normal text-[#86909c]">来自本机记录</span> : null}
            </span>
          }
          extra={<span className="text-[12px] text-[#86909c]">共 {items.length} 笔</span>}
        >
          <Spin loading={loading} style={{ width: "100%" }}>
            {items.length === 0 ? (
              <Empty
                description={
                  searched ? "没有查到订单，换个订单号或联系方式试试" : "输入订单号或联系方式后点查询；本机买过的订单会自动出现在这里"
                }
              />
            ) : (
              <div className="flex flex-col gap-3">
                {pageItems.map((it) => (
                  <div
                    key={`${it.kind}-${it.orderNumber}-${it.token ?? ""}-${it.createdAt}`}
                    className="flex flex-col gap-2 rounded-lg border border-[#e5e6eb] bg-white p-3 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="flex min-w-0 flex-col gap-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate text-[13px] font-medium text-[#1d2129]">{it.goodsName}</span>
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

        {saved.length > 0 ? (
          <Card bordered={false} title={<span className="text-[14px]">本机保存的订单号</span>}>
            <div className="flex flex-wrap gap-2">
              {saved.map((o) => (
                <Tag
                  key={o.orderNumber}
                  closable
                  onClose={(e) => {
                    e.preventDefault();
                    forget(o.orderNumber);
                  }}
                  onClick={() => search(o.orderNumber)}
                  style={{ cursor: "pointer" }}
                >
                  {o.orderNumber}
                </Tag>
              ))}
            </div>
            <div className="mt-2 text-[11px] text-[#86909c]">
              这些订单号只存在当前浏览器，清理浏览器数据会丢失；建议同时记住联系方式。
            </div>
          </Card>
        ) : null}
      </main>

      <Drawer
        width={520}
        title="订单详情"
        visible={detailOpen}
        onCancel={() => setDetailOpen(false)}
        footer={null}
      >
        {detailLoading ? (
          <div className="py-10 text-center text-[13px] text-[#86909c]">加载中…</div>
        ) : detailOrder ? (
          <div className="flex flex-col gap-3">
            <div className="rounded-lg bg-[#f7f8fa] p-3">
              <div className="flex items-center justify-between text-[12px]">
                <span className="text-[#86909c]">订单号</span>
                <span className="font-mono">{detailOrder.orderNumber || "—"}</span>
              </div>
              <div className="mt-1 flex items-center justify-between text-[12px]">
                <span className="text-[#86909c]">状态</span>
                <Tag size="small" color={STATUS_COLOR[detail?.status ?? 1] ?? "gray"}>
                  {detailOrder.statusText}
                </Tag>
              </div>
              <div className="mt-1 flex items-center justify-between text-[12px]">
                <span className="text-[#86909c]">商品</span>
                <span className="max-w-[300px] truncate text-right">{detailOrder.goodsName}</span>
              </div>
              <div className="mt-1 flex items-center justify-between text-[12px]">
                <span className="text-[#86909c]">数量 / 金额</span>
                <span>
                  {detailOrder.count} / ¥{formatPrice(detailOrder.money)}
                </span>
              </div>
              <div className="mt-1 flex items-center justify-between text-[12px]">
                <span className="text-[#86909c]">下单时间</span>
                <span>{fmtTime(detailOrder.createdAt)}</span>
              </div>
              {detailOrder.contact ? (
                <div className="mt-1 flex items-center justify-between text-[12px]">
                  <span className="text-[#86909c]">联系方式</span>
                  <span>{detailOrder.contact}</span>
                </div>
              ) : null}
            </div>

            {detailOrder.remark ? (
              <div className="rounded border border-[#e5e6eb] px-3 py-2 text-[12px] text-[#4e5969]">
                {detailOrder.remark}
              </div>
            ) : null}

            <div className="text-[13px] font-medium">卡密</div>
            {detailOrder.cards.length === 0 ? (
              <div className="rounded border border-[#e5e6eb] px-3 py-4 text-center text-[12px] text-[#86909c]">
                暂无卡密。未支付或上游还在发货，请稍后再查。
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                {detailOrder.cards.map((c) => (
                  <div key={c.cardId} className="rounded border border-[#e5e6eb] px-3 py-2">
                    <div className="break-all font-mono text-[12px]">{c.cardNo || "(无卡号)"}</div>
                    {c.cardPwd ? (
                      <div className="mt-1 break-all font-mono text-[12px] text-[#86909c]">{c.cardPwd}</div>
                    ) : null}
                    <div className="mt-2 flex justify-end">
                      <Button
                        size="mini"
                        icon={<IconCopy />}
                        onClick={() => copy([c.cardNo, c.cardPwd].filter(Boolean).join("----"))}
                      >
                        复制
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}

            <div className="text-[11px] leading-relaxed text-[#86909c]">
              <IconFile /> 卡密涉及账号资产，请勿转发他人。查询仅凭订单号 + 联系方式，请妥善保存。
            </div>
          </div>
        ) : (
          <Empty description="暂无详情" />
        )}
      </Drawer>
    </div>
  );
}
