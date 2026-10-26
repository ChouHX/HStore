"use client";
import { formatPrice } from "@/lib/format";

import {
  Button,
  Message,
  Pagination,
  Popconfirm,
  Space,
  Table,
  Tag,
  Tree,
} from "@arco-design/web-react";
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
    if (!dirId) return;
    const chosen = items.filter((i) => selected.map(String).includes(String(i.goodsId)));
    if (chosen.length === 0) {
      Message.warning("请选择要同步的商品");
      return;
    }
    const j = await fetch("/api/admin/catalog/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ dirId, items: chosen }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success(`已同步 ${j.count} 个商品`);
      loadProducts(dirId, page);
    } else Message.error(j.message ?? "同步失败");
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

  return (
    <div className="flex gap-4">
      <div className="w-[300px] shrink-0 rounded border border-[#e5e6eb] bg-white p-3">
        <div className="mb-2 flex items-center justify-between">
          <div className="text-[13px] font-medium">
            上游板块分类
            {tree.length > 0 && <span className="ml-1 text-[11px] text-[#86909c]">{tree.length} 个板块</span>}
          </div>
          <Button size="mini" loading={treeLoading} onClick={loadTree}>
            刷新
          </Button>
        </div>
        {treeSource === "cache" && (
          <div className="mb-2 text-[11px] text-[#ff7d00]">上游不可达，当前为本地缓存</div>
        )}
        {tree.length === 0 && !treeLoading && (
          <div className="py-6 text-center text-[12px] text-[#86909c]">暂无分类，点击「刷新」从上游拉取</div>
        )}
        <div className="max-h-[70vh] overflow-auto">
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
          <Space>
            <Button size="small" type="primary" disabled={!dirId || selected.length === 0} onClick={importSelected}>
              同步选中（{selected.length}）
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
          </Space>
        </div>

        <div className="mb-2 text-[11px] text-[#86909c]">
          「同步选中」把上游商品导入本地；导入后可在「商品与上下架」编辑信息、调价、上下架，或用「同步上游信息」刷新价格/库存。
        </div>

        <Table
          rowKey="goodsId"
          loading={loading}
          columns={columns}
          data={items}
          scroll={{ x: 760 }}
          rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys) }}
          pagination={false}
        />

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
