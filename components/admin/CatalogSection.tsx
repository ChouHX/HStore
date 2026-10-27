"use client";
import { formatPrice } from "@/lib/format";

import {
  Button,
  Checkbox,
  Message,
  Pagination,
  Popconfirm,
  Table,
  Tag,
  Tree,
} from "@arco-design/web-react";
import { IconDown } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useMemo, useState } from "react";
import type { CategoryNode } from "@/lib/api-types";
import type { RemoteProduct } from "@/lib/types";

interface CatalogItem extends RemoteProduct {
  imported?: boolean;
}

export function CatalogSection() {
  const [tree, setTree] = useState<CategoryNode[]>([]);
  const [treeSource, setTreeSource] = useState<"remote" | "cache" | "local" | null>(null);
  const [treeLoading, setTreeLoading] = useState(false);
  const [dirId, setDirId] = useState<number | null>(null);
  const [dirName, setDirName] = useState("");
  const [dirHasChildren, setDirHasChildren] = useState(false);
  const [items, setItems] = useState<CatalogItem[]>([]);
  const [page, setPage] = useState(1);
  const [allPage, setAllPage] = useState(1);
  const [allCount, setAllCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<(string | number)[]>([]);
  const [fullSyncing, setFullSyncing] = useState(false);
  const [importing, setImporting] = useState(false);
  const [treeOpen, setTreeOpen] = useState(false);

  const loadTree = useCallback(async () => {
    setTreeLoading(true);
    try {
      const j = await fetch("/api/admin/catalog/tree").then((r) => r.json());
      if (j.ok) {
        setTree(j.tree as CategoryNode[]);
        setTreeSource((j.source as typeof treeSource) ?? null);
        if (j.source !== "remote" && j.message) Message.warning(j.message);
      } else Message.error(j.message ?? "加载分类失败");
    } catch {
      Message.error("加载上游板块分类失败");
    } finally {
      setTreeLoading(false);
    }
  }, []);

  useEffect(() => {
    loadTree();
  }, [loadTree]);

  const loadProducts = useCallback(async (id: number, p: number) => {
    setLoading(true);
    try {
      const j = await fetch(`/api/admin/catalog/products?dirId=${id}&page=${p}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as CatalogItem[]);
        setPage(j.page as number);
        setAllPage(j.allPage as number);
        setAllCount(j.allCount as number);
        setSelected([]);
      } else Message.error(j.message);
    } finally {
      setLoading(false);
    }
  }, []);

  function onSelectDir(id: number, name: string, hasChildren = false) {
    setDirId(id);
    setDirName(name);
    setDirHasChildren(hasChildren);
    loadProducts(id, 1);
  }

  const pageSize = allCount > 0 && allPage > 0 ? Math.max(1, Math.ceil(allCount / allPage)) : 20;

  async function importSelected() {
    if (!dirId || importing) return;
    const chosen = items.filter((i) => selected.map(String).includes(String(i.goodsId)));
    if (chosen.length === 0) {
      Message.warning("请选择要同步的商品");
      return;
    }
    setImporting(true);
    Message.loading({
      id: "catalog-import",
      duration: 0,
      content: `正在同步 ${chosen.length} 个商品，请稍候…`,
    });
    try {
      const j = await fetch("/api/admin/catalog/import", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ dirId, items: chosen }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success({
          id: "catalog-import",
          duration: 3000,
          content: `已同步 ${j.count} 个商品`,
        });
        await loadProducts(dirId, page);
        loadTree();
      } else {
        Message.error({
          id: "catalog-import",
          duration: 4000,
          content: j.message ?? "同步失败",
        });
      }
    } catch {
      Message.error({
        id: "catalog-import",
        duration: 4000,
        content: "同步请求失败，请检查网络后重试",
      });
    } finally {
      setImporting(false);
    }
  }



  /** 全量同步：按上游板块重新拉取微软/谷歌商品（会覆盖本地同名商品的上下架状态） */
  async function fullSync() {
    setFullSyncing(true);
    try {
      const j = await fetch("/api/sync", { method: "POST" }).then((r) => r.json());
      if (j.ok) {
        Message.success(j.message ?? "全量同步完成");
        loadTree();
      } else Message.error(j.message ?? "全量同步失败");
    } catch {
      Message.error("全量同步失败");
    } finally {
      setFullSyncing(false);
    }
  }

  const columns = [
    { title: "编号", dataIndex: "goodsId", width: 90, render: (_: unknown, r: CatalogItem) => <span className="font-mono">#{r.goodsId}</span> },
    {
      title: "名称",
      dataIndex: "name",
      render: (_: unknown, r: CatalogItem) => (
        <span className="line-clamp-1 max-w-[380px]" title={r.name}>
          {r.name}
        </span>
      ),
    },
    { title: "成本", dataIndex: "salesPrice", width: 100, align: "right" as const, render: (_: unknown, r: CatalogItem) => `¥${formatPrice(r.salesPrice)}` },
    {
      title: "库存",
      dataIndex: "stockCount",
      width: 90,
      align: "right" as const,
      render: (_: unknown, r: CatalogItem) => {
        const stock = r.stockCount ?? -1;
        if (stock < 0) return <span className="text-[12px] text-[#86909c]">未知</span>;
        return (
          <span className={stock === 0 ? "font-medium text-[#f53f3f]" : "font-medium text-[#00b42a]"}>
            {stock}
          </span>
        );
      },
    },
    {
      title: "状态",
      dataIndex: "imported",
      width: 90,
      render: (_: unknown, r: CatalogItem) =>
        r.imported ? (
          <Tag size="small" color="green">已同步</Tag>
        ) : (
          <Tag size="small" color="gray">未同步</Tag>
        ),
    },
  ];

  const treeData = useMemo(
    () =>
      tree.map((n) => ({
        key: n.key,
        title: (
          <span className="text-[12px]">
            {n.title}
            {n.productCount > 0 && <span className="ml-1 text-[#86909c]">({n.productCount})</span>}
          </span>
        ),
        children: n.children?.map((c) => ({
          key: c.key,
          title: <span className="text-[12px]">{c.title}</span>,
        })),
      })),
    [tree],
  );

  function toggleOne(id: number, checked: boolean) {
    setSelected((prev) => (checked ? [...prev, id] : prev.filter((k) => Number(k) !== id)));
  }

  const selectedIds = new Set(selected.map(Number));
  const pageAllSelected = items.length > 0 && items.every((it) => selectedIds.has(it.goodsId));

  return (
    <div className="flex flex-col gap-3 md:flex-row md:gap-4">
      <div className="w-full shrink-0 rounded border border-[#e5e6eb] bg-white p-3 md:w-[300px]">
        <div className="mb-2 flex items-center justify-between">
          <button
            type="button"
            className="flex items-center gap-1 text-left text-[13px] font-medium"
            onClick={() => setTreeOpen((o) => !o)}
          >
            <IconDown
              className="transition-transform md:hidden"
              style={{ transform: treeOpen ? "rotate(0deg)" : "rotate(-90deg)" }}
            />
            上游板块分类
            {tree.length > 0 && <span className="ml-1 text-[11px] text-[#86909c]">{tree.length} 个板块</span>}
          </button>
          <Button size="mini" loading={treeLoading} onClick={loadTree}>
            刷新
          </Button>
        </div>
        {dirName && !treeOpen && (
          <div className="mb-1 text-[12px] text-[#86909c] md:hidden">
            当前：<span className="text-[#1d2129]">{dirName}</span>
          </div>
        )}
        {treeSource === "cache" && (
          <div className="mb-2 text-[11px] text-[#ff7d00]">上游不可达，当前为本地缓存</div>
        )}
        {tree.length === 0 && !treeLoading && (
          <div className="py-6 text-center text-[12px] text-[#86909c]">暂无分类，点击「刷新」从上游拉取</div>
        )}
        <div className={`max-h-[40vh] overflow-auto md:max-h-[70vh] ${treeOpen ? "block" : "hidden"} md:block`}>
          <Tree
            treeData={treeData}
            blockNode
            showLine
            selectedKeys={dirId ? [String(dirId)] : []}
            onSelect={(keys) => {
              if (keys.length === 0) return;
              const id = Number(keys[0]);
              // 找到名称
              const findNode = (nodes: CategoryNode[]): CategoryNode | null => {
                for (const n of nodes) {
                  if (n.id === id) return n;
                  if (n.children) {
                    const r = findNode(n.children);
                    if (r) return r;
                  }
                }
                return null;
              };
              const node = findNode(tree);
              onSelectDir(id, node?.title ?? "", Boolean(node?.children?.length));
              setTreeOpen(false);
            }}
          />
        </div>
      </div>

      <div className="min-w-0 flex-1">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div className="text-[13px]">
            {dirId ? (
              <>
                当前分类：<span className="font-medium">{dirName}</span>
                <span className="ml-2 text-[12px] text-[#86909c]">共 {allCount} 个商品</span>
              </>
            ) : (
              <span className="text-[#86909c]">请选择左侧分类查看上游商品</span>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="small"
              type="primary"
              loading={importing}
              disabled={!dirId || selected.length === 0 || importing}
              onClick={importSelected}
            >
              {importing ? "同步中…" : `同步选中（${selected.length}）`}
            </Button>
            <Button size="small" disabled={!dirId} onClick={() => dirId && loadProducts(dirId, page)}>
              刷新
            </Button>
            <Popconfirm
              title="将从上游重新拉取「微软/谷歌」板块的全部商品并覆盖本地同编号商品（上下架状态会被重置为上架），确定继续？"
              onOk={fullSync}
            >
              <Button size="small" loading={fullSyncing}>
                全量同步
              </Button>
            </Popconfirm>
          </div>
        </div>

        <div className="mb-2 text-[11px] text-[#86909c]">
          「同步选中」把上游商品导入本地；导入后可在「商品与上下架」编辑信息、调价、上下架，或用「同步上游信息」刷新价格/库存。
        </div>

        {importing && (
          <div className="mb-2 rounded border border-[#bedaff] bg-[#e8f3ff] px-3 py-2 text-[12px] text-[#165dff]">
            正在同步 {selected.length} 个商品，请勿关闭页面…
          </div>
        )}

        <div className="hidden md:block">
          <Table
            rowKey="goodsId"
            loading={loading || importing}
            columns={columns}
            data={items}
            scroll={{ x: 850 }}
            rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys) }}
            pagination={false}
          />
        </div>

        {/* 移动端：表格列太多会挤压，改成卡片列表 */}
        <div className="flex flex-col gap-2 md:hidden">
          <div className="flex items-center justify-between text-[12px] text-[#86909c]">
            <Checkbox
              checked={pageAllSelected}
              indeterminate={!pageAllSelected && items.some((it) => selectedIds.has(it.goodsId))}
              onChange={(checked) =>
                setSelected(checked ? items.map((it) => it.goodsId) : [])
              }
            >
              全选本页
            </Checkbox>
            <span>已选 {selected.length} 个</span>
          </div>
          {items.map((it) => {
            const stock = it.stockCount ?? -1;
            return (
              <label
                key={it.goodsId}
                className="flex cursor-pointer items-start gap-2 rounded border border-[#e5e6eb] bg-white p-3"
              >
                <Checkbox
                  checked={selectedIds.has(it.goodsId)}
                  onChange={(checked) => toggleOne(it.goodsId, checked)}
                />
                <div className="min-w-0 flex-1">
                  <div className="text-[13px] leading-snug">{it.name}</div>
                  <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-[#86909c]">
                    <span className="font-mono">#{it.goodsId}</span>
                    <span>成本 ¥{formatPrice(it.salesPrice)}</span>
                    <span>
                      库存{" "}
                      {stock < 0 ? (
                        "未知"
                      ) : (
                        <span className={stock === 0 ? "font-medium text-[#f53f3f]" : "font-medium text-[#00b42a]"}>
                          {stock}
                        </span>
                      )}
                    </span>
                    {it.imported ? (
                      <Tag size="small" color="green">已同步</Tag>
                    ) : (
                      <Tag size="small" color="gray">未同步</Tag>
                    )}
                  </div>
                </div>
              </label>
            );
          })}
          {loading && (
            <div className="py-4 text-center text-[12px] text-[#86909c]">加载中…</div>
          )}
        </div>

        {dirId && !loading && items.length === 0 && (
          <div className="mt-3 rounded bg-[#f7f8fa] px-3 py-2 text-[12px] text-[#86909c]">
            {dirHasChildren
              ? "该板块的商品挂在子分类下，请展开左侧分类选择子分类查看。"
              : "该分类下暂无上游商品。"}
          </div>
        )}

        {allCount > 0 && (
          <div className="mt-3 flex justify-end">
            <Pagination
              total={allCount}
              current={page}
              pageSize={pageSize}
              showTotal
              onChange={(p) => dirId && loadProducts(dirId, p)}
            />
          </div>
        )}
      </div>

    </div>
  );
}
