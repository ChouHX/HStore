"use client";
import { formatPrice } from "@/lib/format";

import {
  Button,
  Message,
  Pagination,
  Space,
  Table,
  Tag,
  Tree,
} from "@arco-design/web-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import type { CategoryNode } from "@/lib/queries";
import type { RemoteProduct } from "@/lib/types";

interface CatalogItem extends RemoteProduct {
  imported?: boolean;
}

export function CatalogSection() {
  const [tree, setTree] = useState<CategoryNode[]>([]);
  const [dirId, setDirId] = useState<number | null>(null);
  const [dirName, setDirName] = useState("");
  const [items, setItems] = useState<CatalogItem[]>([]);
  const [page, setPage] = useState(1);
  const [allPage, setAllPage] = useState(1);
  const [allCount, setAllCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<(string | number)[]>([]);


  useEffect(() => {
    fetch("/api/admin/catalog/tree")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setTree(j.tree as CategoryNode[]);
        else Message.error(j.message);
      })
      .catch(() => Message.error("加载分类失败"));
  }, []);

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

  function onSelectDir(id: number, name: string) {
    setDirId(id);
    setDirName(name);
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
        <div className="mb-2 text-[13px] font-medium">上游分类</div>
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
              const findName = (nodes: CategoryNode[]): string | null => {
                for (const n of nodes) {
                  if (n.id === id) return n.title;
                  if (n.children) {
                    const r = findName(n.children);
                    if (r) return r;
                  }
                }
                return null;
              };
              onSelectDir(id, findName(tree) ?? "");
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
          </Space>
        </div>

        <div className="mb-2 text-[11px] text-[#86909c]">
          同步后可在「商品与上下架」中编辑本地信息（标题/描述/图片）、调价、下架或删除。
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
