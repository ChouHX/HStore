"use client";

import {
  Button,
  Dropdown,
  Input,
  Menu,
  Modal,
  Pagination,
  Select,
  Space,
  Tag,
  Tooltip,
} from "@arco-design/web-react";
import {
  IconApps,
  IconLayout,
  IconSort,
  IconUnorderedList,
} from "@arco-design/web-react/icon";
import { useCallback, useEffect, useState } from "react";
import type { Product, User } from "@/lib/types";
import type { StoreBoard, StoreSubCategory } from "@/lib/api-types";
import type { Announcement } from "@/lib/api-types";
import { formatPrice } from "@/lib/format";
import { PurchaseModal } from "./PurchaseModal";
import { HeroSection } from "./store/HeroSection";
import { StoreHeader } from "./store/StoreHeader";
import { PopupAnnouncement } from "./store/PopupAnnouncement";

interface Props {
  boards: StoreBoard[];
  configured: boolean;
  lastSync: number | null;
  user: User | null;
  discount: number;
  announcements: Announcement[];
  popup: Announcement | null;
}

type SortKey = "name-asc" | "price-asc" | "price-desc" | "stock-desc";
const PAGE_SIZE = 20;

const SORTS: { value: SortKey; label: string }[] = [
  { value: "name-asc", label: "名称排序" },
  { value: "price-asc", label: "价格从低到高" },
  { value: "price-desc", label: "价格从高到低" },
  { value: "stock-desc", label: "库存优先" },
];

function catImg(img: string | undefined | null): string | null {
  if (!img) return null;
  // 后端已把上游图片换成本地缓存地址；不再把上游 URL 透传给浏览器
  return img.startsWith("http") ? null : img;
}
function cleanBoardName(name: string): string {
  const cleaned = name.replace(/[（(][^（）()]*[）)]\s*$/, "").trim();
  return cleaned || name;
}
function productTitle(p: Product): string {
  return p.customName || p.name;
}
function productImg(p: Product): string | null {
  return catImg(p.customImg || p.imgUrl);
}
function stockTag(p: Product) {
  if (p.stockCount < 0) return <Tag size="small">库存 -</Tag>;
  if (p.stockCount === 0) return <Tag size="small" color="red">缺货</Tag>;
  return <Tag size="small" color="green">库存 {p.stockCount}</Tag>;
}

function CategoryChips({
  items,
  dirFilter,
  onPick,
}: {
  items: StoreSubCategory[];
  dirFilter: number;
  onPick: (id: number) => void;
}) {
  const cls = (active: boolean) =>
    `flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-3 py-1 text-[12px] transition ${
      active
        ? "border-[#165dff] bg-[#e8f3ff] text-[#165dff]"
        : "border-[#e5e6eb] bg-white text-[#4e5969] hover:border-[#c9cdd4]"
    }`;
  return (
    <div className="flex flex-wrap gap-2">
      <button type="button" onClick={() => onPick(0)} className={cls(dirFilter === 0)}>
        全部商品
      </button>
      {items.map((c) => (
        <button key={c.id} type="button" onClick={() => onPick(c.id)} className={cls(dirFilter === c.id)}>
          {catImg(c.img) && (
            <img src={catImg(c.img)!} alt="" className="h-4 w-4 shrink-0 rounded-full object-cover" loading="lazy" />
          )}
          {c.name}
        </button>
      ))}
    </div>
  );
}

function ProductCard({
  p,
  discount,
  onBuy,
  mode = "grid",
}: {
  p: Product;
  discount: number;
  onBuy: (p: Product) => void;
  mode?: "grid" | "list";
}) {
  const outOfStock = p.stockCount === 0;
  const disabled = p.status !== 1 || outOfStock;
  const price = Number((p.finalPrice * discount).toFixed(4));
  const imgSrc = productImg(p);

  if (mode === "list") {
    return (
      <div className="flex items-center gap-3 rounded border border-[#e5e6eb] bg-white p-3 transition hover:shadow-md">
        <div className="h-12 w-12 shrink-0 overflow-hidden rounded bg-[#f7f8fa]">
          {imgSrc && (
            <img
              src={imgSrc}
              alt=""
              loading="lazy"
              className="h-full w-full object-cover"
              onError={(e) => {
                e.currentTarget.style.display = "none";
              }}
            />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="clamp-2 text-[13px] leading-snug" title={productTitle(p)}>
            {productTitle(p)}
          </div>
          {p.customDesc && <div className="clamp-2 mt-1 text-[11px] text-[#86909c]">{p.customDesc}</div>}
          <div className="mt-1 flex flex-wrap gap-1">
            {p.goodsType === 3 ? (
              <Tag size="small">直充</Tag>
            ) : (
              <span className="hidden sm:inline-flex">
                <Tag size="small">卡券</Tag>
              </span>
            )}
            {stockTag(p)}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <div className="text-[18px] font-semibold text-[#f53f3f]">
            <span className="text-[12px]">¥</span>
            {formatPrice(price)}
          </div>
          <Button type="primary" size="small" disabled={disabled} onClick={() => onBuy(p)}>
            {outOfStock ? "缺货" : "购买"}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2 rounded border border-[#e5e6eb] bg-white p-3 transition hover:shadow-md">
      <div className="flex gap-2">
        <div className="h-12 w-12 shrink-0 overflow-hidden rounded bg-[#f7f8fa]">
          {imgSrc && (
            <img
              src={imgSrc}
              alt=""
              loading="lazy"
              className="h-full w-full object-cover"
              onError={(e) => {
                e.currentTarget.style.display = "none";
              }}
            />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="clamp-2 text-[13px] leading-snug" title={productTitle(p)}>
            {productTitle(p)}
          </div>
          <div className="mt-1 flex flex-wrap gap-1">
            {p.goodsType === 3 ? (
              <Tag size="small">直充</Tag>
            ) : (
              <span className="hidden sm:inline-flex">
                <Tag size="small">卡券</Tag>
              </span>
            )}
            {stockTag(p)}
          </div>
        </div>
      </div>
      {p.customDesc && <div className="clamp-2 text-[11px] text-[#86909c]">{p.customDesc}</div>}
      <div className="mt-auto flex items-end justify-between pt-1">
        <div className="text-[18px] font-semibold text-[#f53f3f]">
          <span className="text-[12px]">¥</span>
          {formatPrice(price)}
        </div>
        <Button type="primary" size="small" disabled={disabled} onClick={() => onBuy(p)}>
          {outOfStock ? "缺货" : "购买"}
        </Button>
      </div>
    </div>
  );
}

export function ProductBrowser({ boards, configured, lastSync, user, discount, announcements, popup }: Props) {
  const [purchaseProduct, setPurchaseProduct] = useState<Product | null>(null);
  const [boardId, setBoardId] = useState<number | null>(boards[0]?.id ?? null);
  const [dirFilter, setDirFilter] = useState(0);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<SortKey>("name-asc");
  const [layout, setLayout] = useState<"list" | "2" | "4">("4");
  const [page, setPage] = useState(1);
  const [items, setItems] = useState<Product[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [stockSyncing, setStockSyncing] = useState(false);
  const [isDesktop, setIsDesktop] = useState(true);

  useEffect(() => {
    const mq = window.matchMedia("(min-width: 1024px)");
    const sync = () => setIsDesktop(mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);

  const activeBoard = boards.find((b) => b.id === boardId) ?? null;
  const searching = query.trim().length > 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const load = useCallback(async () => {
    const searching = query.trim().length > 0;
    if (!searching && !boardId) {
      setItems([]);
      setTotal(0);
      return;
    }
    setLoading(true);
    try {
      const sp = new URLSearchParams({
        page: String(page),
        pageSize: String(PAGE_SIZE),
        sort,
      });
      if (searching) {
        sp.set("keyword", query.trim());
      } else {
        sp.set("rootId", String(boardId));
        if (dirFilter) sp.set("dirId", String(dirFilter));
      }
      const j = await fetch(`/api/store/products?${sp}`).then((r) => r.json());
      if (j.ok) {
        const list = j.items as Product[];
        setItems(list);
        setTotal(j.total as number);
        if (page > 1 && list.length === 0) setPage(1);
        // 后台同步上游库存（不阻塞列表渲染）
        const ids = list.map((p) => p.goodsId);
        if (ids.length > 0) {
          setStockSyncing(true);
          fetch(`/api/store/stock?ids=${ids.join(",")}`)
            .then((r) => r.json())
            .then((st) => {
              if (!st.ok) return;
              const stocks = st.stocks as Record<string, number>;
              setItems((prev) =>
                prev.map((p) => (stocks[p.goodsId] != null ? { ...p, stockCount: stocks[p.goodsId] } : p)),
              );
            })
            .catch(() => {})
            .finally(() => setStockSyncing(false));
        }
      }
    } finally {
      setLoading(false);
    }
  }, [boardId, dirFilter, query, sort, page]);

  useEffect(() => {
    load();
  }, [load]);

  function pickBoard(id: number) {
    setBoardId(id);
    setDirFilter(0);
    setPage(1);
  }
  function pickDir(id: number) {
    setDirFilter(id);
    setPage(1);
  }
  function handleBuy(p: Product) {
    // 购买按钮始终打开购买弹窗：历史订单只从「订单查询 / 我的订单」进入，
    // 避免买过一次后再次购买被自动跳转到旧订单详情。
    setPurchaseProduct(p);
  }

  return (
    <div className="min-h-screen">
      <StoreHeader
        user={user}
        searchQuery={query}
        onSearch={(q) => {
          setQuery(q);
          setPage(1);
        }}
        announcements={announcements}
      />

      <main className="mx-auto max-w-[1200px] px-4 py-4">
        {!configured && (
          <div className="mb-3 rounded border border-[#ffcf8b] bg-[#fff7e8] px-3 py-2 text-[12px] text-[#d25f00]">
            上游接口未配置，请让管理员在「管理后台 → 系统设置」填写 AppID 与密钥。
          </div>
        )}

        <HeroSection announcements={announcements} />

        {boards.length === 0 ? (
          <div className="flex h-40 items-center justify-center text-[13px] text-[#86909c]">
            暂无上架商品，请管理员在「商品同步」中导入。
          </div>
        ) : (
          <>
            {/* 板块卡片 */}
            {!searching && (
              <>
            <div id="store-products" className="mb-1 grid grid-cols-4 gap-1 sm:flex sm:flex-wrap sm:gap-1">
              {boards.map((b) => {
                const active = b.id === boardId;
                return (
                  <button
                    key={b.id}
                    type="button"
                    onClick={() => pickBoard(b.id)}
                    className={`flex w-full flex-col items-center gap-0 rounded-md px-1 py-1.5 text-center transition sm:w-auto sm:flex-row sm:gap-2 sm:px-2 sm:py-1.5 sm:text-left ${
                      active ? "bg-[#e8f3ff] text-[#165dff]" : "text-[#1d2129] hover:bg-[#f2f3f5]"
                    }`}
                  >
                    {catImg(b.img) ? (
                      <img
                        src={catImg(b.img)!}
                        alt=""
                        className="mb-1 h-9 w-9 shrink-0 rounded-full object-cover sm:mb-0 sm:h-7 sm:w-7"
                        loading="lazy"
                      />
                    ) : (
                      <div className="mb-1 h-9 w-9 shrink-0 rounded-full bg-[#f2f3f5] sm:mb-0 sm:h-7 sm:w-7" />
                    )}
                    <span className="flex w-full min-w-0 flex-col items-center leading-tight sm:w-auto sm:flex-row sm:items-center sm:gap-2">
                      <span
                        className="w-full break-words px-0.5 text-[12px] leading-snug sm:line-clamp-1 sm:w-auto sm:px-0 sm:text-[13px]"
                        title={b.name}
                      >
                        {cleanBoardName(b.name)}
                      </span>
                      <span
                        className={`hidden text-[10px] sm:inline sm:text-[11px] ${active ? "text-[#165dff]" : "text-[#86909c]"}`}
                      >
                        {b.total}
                      </span>
                    </span>
                  </button>
                );
              })}
            </div>
              </>
            )}


            {!searching && activeBoard && (
              <div className="mt-3">
                <CategoryChips items={activeBoard.children} dirFilter={dirFilter} onPick={pickDir} />
              </div>
            )}

            {activeBoard && (
              <div className="mt-3">
                <div className="min-w-0">
                  {searching && (
                    <div className="mb-3 flex items-center justify-between gap-2">
                      <div className="text-[13px]">
                        搜索 “<span className="font-medium">{query}</span>” 的结果
                        <span className="ml-2 text-[12px] text-[#86909c]">共 {total} 件</span>
                      </div>
                      <Button
                        size="small"
                        onClick={() => {
                          setQuery("");
                          setPage(1);
                        }}
                      >
                        清除搜索
                      </Button>
                    </div>
                  )}

                  <div className="mb-3 flex items-center justify-between gap-2">
                    <div className="flex min-w-0 flex-1 items-center gap-2">
                      <span className="hidden md:inline-block">
                        <Select
                          value={sort}
                          onChange={(v) => {
                            setSort(v as SortKey);
                            setPage(1);
                          }}
                          style={{ width: 140 }}
                        >
                          {SORTS.map((s) => (
                            <Select.Option key={s.value} value={s.value}>
                              {s.label}
                            </Select.Option>
                          ))}
                        </Select>
                      </span>

                      <span className="md:hidden">
                        <Dropdown
                          trigger="click"
                          position="bl"
                          droplist={
                            <Menu
                              onClickMenuItem={(k) => {
                                setSort(k as SortKey);
                                setPage(1);
                              }}
                            >
                              {SORTS.map((s) => (
                                <Menu.Item key={s.value}>{s.label}</Menu.Item>
                              ))}
                            </Menu>
                          }
                        >
                          <Button icon={<IconSort />} aria-label="排序">
                            排序
                          </Button>
                        </Dropdown>
                      </span>
                    </div>

                    <div className="flex shrink-0 items-center gap-2">
                      <span className="hidden text-[12px] text-[#86909c] lg:inline">
                        共 {total} 件
                        {stockSyncing && <span className="ml-3 text-[#165dff]">库存同步中…</span>}
                        {lastSync && <span className="ml-3">更新于 {new Date(lastSync).toLocaleString("zh-CN")}</span>}
                      </span>
                      <Space size={2}>
                        <Tooltip content="列表">
                          <Button
                            size="small"
                            aria-label="列表"
                            type={layout === "list" ? "primary" : "text"}
                            icon={<IconUnorderedList />}
                            onClick={() => setLayout("list")}
                          />
                        </Tooltip>
                        <Tooltip content="两列">
                          <Button
                            size="small"
                            aria-label="两列"
                            type={layout === "2" || (!isDesktop && layout === "4") ? "primary" : "text"}
                            icon={<IconApps />}
                            onClick={() => setLayout("2")}
                          />
                        </Tooltip>
                        <span className="hidden lg:inline-flex">
                          <Tooltip content="四列">
                            <Button
                              size="small"
                              aria-label="四列"
                              type={layout === "4" ? "primary" : "text"}
                              icon={<IconLayout />}
                              onClick={() => setLayout("4")}
                            />
                          </Tooltip>
                        </span>
                      </Space>
                    </div>
                  </div>

                  {loading ? (
                    <div className="flex h-40 items-center justify-center text-[13px] text-[#86909c]">加载中…</div>
                  ) : items.length === 0 ? (
                    <div className="flex h-40 items-center justify-center text-[13px] text-[#86909c]">
                      该分类暂无商品
                    </div>
                  ) : (
                    <div
                      className={
                        layout === "list"
                          ? "grid grid-cols-1 gap-2"
                          : layout === "2"
                            ? "grid grid-cols-2 gap-3"
                            : "grid grid-cols-2 gap-3 lg:grid-cols-4"
                      }
                    >
                      {items.map((p) => (
                        <ProductCard
                          key={p.goodsId}
                          p={p}
                          discount={discount}
                          onBuy={handleBuy}
                          mode={layout === "list" ? "list" : "grid"}
                        />
                      ))}
                    </div>
                  )}

                  {totalPages > 1 && (
                    <div className="mt-4 flex justify-center">
                      <Pagination
                        total={total}
                        current={Math.min(page, totalPages)}
                        pageSize={PAGE_SIZE}
                        showTotal
                        onChange={setPage}
                      />
                    </div>
                  )}
                </div>
              </div>
            )}
          </>
        )}
      </main>


      <PopupAnnouncement popup={popup} />

      <PurchaseModal
        product={purchaseProduct}
        open={purchaseProduct !== null}
        discount={discount}
        user={user}
        onOpenChange={(o) => {
          if (!o) setPurchaseProduct(null);
        }}
        onDone={() => load()}
      />
    </div>
  );
}
