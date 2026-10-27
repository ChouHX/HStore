import { Button, Card, Empty, Input, Message, Pagination, Space, Spin, Tag } from "@arco-design/web-react";
import { IconArrowLeft, IconSearch } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { formatPrice } from "@/lib/format";
import { loadSavedOrders } from "@/lib/orderStorage";

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

  const loadLocal = useCallback(async () => {
    const local = loadSavedOrders();
    if (local.length === 0) return;
    setLoading(true);
    try {
      const nums = local.map((o) => o.orderNumber).slice(0, 50);
      const j = await fetch(`/api/orders/query?orderNumbers=${encodeURIComponent(nums.join(","))}`).then((r) =>
        r.json(),
      );
      if (j.ok) {
        setItems(j.items ?? []);
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

  function openDetail(it: OrderItem) {
    if (it.kind === "guest" && it.token) {
      navigate(`/g/${encodeURIComponent(it.token)}`);
      return;
    }
    const qs = it.contact ? `?contact=${encodeURIComponent(it.contact)}` : "";
    navigate(`/order/${encodeURIComponent(it.orderNumber)}${qs}`);
  }

  const pageItems = useMemo(() => items.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE), [items, page]);

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

      <main className="mx-auto flex w-full max-w-[820px] flex-col gap-4 px-4 py-5">
        <Card bordered={false}>
          <div className="flex flex-col gap-3">
            <div className="text-center">
              <div className="text-[15px] font-medium text-[#1d2129]">查订单</div>
              <div className="mt-1 text-[12px] text-[#86909c]">
                输入订单号或下单时填写的联系方式（手机 / 邮箱 / QQ），游客也能查。
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
                    <div className="flex min-w-0 flex-col gap-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="break-words text-[13px] font-medium text-[#1d2129]">{it.goodsName}</span>
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

      </main>
    </div>
  );
}
