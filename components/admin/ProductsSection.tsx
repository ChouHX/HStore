"use client";

import {
  Button,
  Checkbox,
  Dropdown,
  Input,
  InputNumber,
  Menu,
  Message,
  Modal,
  Popconfirm,
  Popover,
  Space,
  Table,
  Tag,
} from "@arco-design/web-react";
import { IconFilter } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useState } from "react";
import type { AdminProduct, ProductFilterOptions } from "@/lib/api-types";
import { formatPrice } from "@/lib/format";

const PAGE_SIZE = 15;

interface Option {
  id: number;
  name: string;
  count?: number;
}

function MultiFilter({
  title,
  options,
  selected,
  onChange,
}: {
  title: string;
  options: Option[];
  selected: number[];
  onChange: (ids: number[]) => void;
}) {
  const all = options.length > 0 && selected.length === options.length;
  const some = selected.length > 0 && selected.length < options.length;
  return (
    <div style={{ width: 260 }}>
      <div className="mb-2 flex items-center justify-between">
        <span className="text-[12px] text-[#4e5969]">{title}</span>
        <span className="text-[11px] text-[#86909c]">
          已选 {selected.length} / {options.length}
        </span>
      </div>
      <div className="mb-2 flex items-center justify-between">
        <Checkbox
          checked={all}
          indeterminate={some}
          onChange={(c) => onChange(c ? options.map((o) => o.id) : [])}
        >
          全选
        </Checkbox>
        <Button size="mini" type="text" onClick={() => onChange([])}>
          清空
        </Button>
      </div>
      <div style={{ maxHeight: 260, overflowY: "auto", display: "flex", flexDirection: "column", gap: 6 }}>
        <Checkbox.Group value={selected} onChange={(vals) => onChange(vals as number[])}>
          {options.map((o) => (
            <div key={o.id} style={{ display: "flex", alignItems: "center" }}>
              <Checkbox value={o.id}>
                <span className="inline-block max-w-[190px] truncate align-middle" title={o.name}>
                  {o.name}
                  {o.count != null && <span className="ml-1 text-[#86909c]">({o.count})</span>}
                </span>
              </Checkbox>
            </div>
          ))}
        </Checkbox.Group>
        {options.length === 0 && <span className="text-[12px] text-[#86909c]">暂无可选值</span>}
      </div>
    </div>
  );
}

function FilterHeader({
  label,
  count,
  children,
}: {
  label: string;
  count: number;
  children: React.ReactNode;
}) {
  return (
    <span className="inline-flex items-center gap-1">
      <span>{label}</span>
      <Popover trigger="click" position="bl" content={children}>
        <Button
          size="mini"
          type={count > 0 ? "primary" : "text"}
          icon={<IconFilter />}
          style={{ padding: "0 2px", height: 20, minWidth: 20 }}
        />
      </Popover>
    </span>
  );
}

export function ProductsSection() {
  const [items, setItems] = useState<AdminProduct[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [keyword, setKeyword] = useState("");
  const [rootIds, setRootIds] = useState<number[]>([]);
  const [subIds, setSubIds] = useState<number[]>([]);
  const [enabledList, setEnabledList] = useState<number[]>([]);
  const [options, setOptions] = useState<ProductFilterOptions>({ roots: [], subs: [], status: [] });
  const [selected, setSelected] = useState<(string | number)[]>([]);
  const [loading, setLoading] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [priceModal, setPriceModal] = useState<{ goodsId: number; name: string; price: number; locked: boolean } | null>(null);
  const [editTarget, setEditTarget] = useState<{
    goodsId: number;
    name: string;
    desc: string;
    img: string;
    custom: boolean;
    loading: boolean;
    upstreamName: string;
    upstreamDesc: string;
    upstreamImg: string;
  } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const sp = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
      if (keyword) sp.set("keyword", keyword);
      if (rootIds.length) sp.set("rootIds", rootIds.join(","));
      if (subIds.length) sp.set("subIds", subIds.join(","));
      if (enabledList.length) sp.set("enabledList", enabledList.join(","));
      const j = await fetch(`/api/admin/products?${sp}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as AdminProduct[]);
        setTotal(j.total as number);
        setSelected([]);
      } else Message.error(j.message);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, keyword, rootIds, subIds, enabledList]);

  useEffect(() => {
    load();
  }, [load]);

  const loadOptions = useCallback(async () => {
    const j = await fetch("/api/admin/product-filters").then((r) => r.json());
    if (j.ok) setOptions({ roots: j.roots, subs: j.subs, status: j.status });
  }, []);

  useEffect(() => {
    loadOptions();
  }, [loadOptions]);

  const subOptions =
    rootIds.length > 0
      ? options.subs.filter((s) => s.rootId != null && rootIds.includes(s.rootId))
      : options.subs;

  function applyRoots(ids: number[]) {
    setRootIds(ids);
    if (subIds.length) {
      const allowed = new Set(
        options.subs
          .filter((s) => ids.length === 0 || (s.rootId != null && ids.includes(s.rootId)))
          .map((s) => s.id),
      );
      setSubIds(subIds.filter((id) => allowed.has(id)));
    }
    setPage(1);
  }

  function resetFilters() {
    setKeyword("");
    setRootIds([]);
    setSubIds([]);
    setEnabledList([]);
    setPage(1);
  }

  async function post(body: unknown, ok: string) {
    const j = await fetch("/api/admin/products", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success(ok);
      load();
    } else Message.error(j.message ?? "操作失败");
  }

  /**
   * 同步「当前商品」的上游信息（价格/库存/状态等）。
   * 有勾选时只同步勾选项，否则同步当前筛选结果（无筛选即全部本地商品）。
   */
  async function sync() {
    setSyncing(true);
    try {
      const filtered = Boolean(keyword || rootIds.length || subIds.length || enabledList.length);
      let ids: number[] = selected.map(Number).filter(Boolean);
      let scopeLabel = "选中";

      if (ids.length === 0) {
        const sp = new URLSearchParams({ idsOnly: "1" });
        if (keyword) sp.set("keyword", keyword);
        if (rootIds.length) sp.set("rootIds", rootIds.join(","));
        if (subIds.length) sp.set("subIds", subIds.join(","));
        if (enabledList.length) sp.set("enabledList", enabledList.join(","));
        const j = await fetch(`/api/admin/products?${sp}`).then((r) => r.json());
        if (!j.ok) {
          Message.error(j.message ?? "获取商品列表失败");
          return;
        }
        ids = (j.ids as number[]) ?? [];
        scopeLabel = filtered ? "筛选结果内" : "全部";
      }

      if (ids.length === 0) {
        Message.warning("没有可同步的商品");
        return;
      }

      const CHUNK = 40;
      let updated = 0;
      let failed = 0;
      let missing = 0;
      for (let i = 0; i < ids.length; i += CHUNK) {
        const chunk = ids.slice(i, i + CHUNK);
        Message.loading({
          id: "sync-upstream",
          duration: 0,
          content: `同步${scopeLabel}商品 ${Math.min(i + chunk.length, ids.length)}/${ids.length}…`,
        });
        const r = await fetch("/api/sync", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ scope: "current", ids: chunk }),
        }).then((res) => res.json());
        if (!r.ok) {
          failed += chunk.length;
          continue;
        }
        updated += (r.updated as number) ?? 0;
        failed += (r.failed as number) ?? 0;
        missing += (r.missing as number[])?.length ?? 0;
      }

      const parts = [`已更新 ${updated} 个商品的上游信息`];
      if (failed) parts.push(`${failed} 个拉取失败`);
      if (missing) parts.push(`${missing} 个已失效`);
      Message.success({ id: "sync-upstream", duration: 3000, content: parts.join("，") });
      load();
      loadOptions();
    } catch {
      Message.error({ id: "sync-upstream", duration: 3000, content: "同步失败，请稍后重试" });
    } finally {
      setSyncing(false);
    }
  }

  const openEdit = useCallback((r: AdminProduct) => {
    setEditTarget({
      goodsId: r.goodsId,
      name: r.customName ?? r.name,
      desc: r.customDesc ?? "",
      img: r.customImg ?? r.imgUrl ?? "",
      custom: Boolean(r.customName || r.customDesc || r.customImg),
      loading: true,
      upstreamName: r.name,
      upstreamDesc: "",
      upstreamImg: r.imgUrl ?? "",
    });
    fetch(`/api/goods/detail?id=${r.goodsId}`)
      .then((res) => res.json())
      .then((j) => {
        if (!j.ok) throw new Error(j.message);
        const d = j.detail as { name?: string; goodsDescribe?: string; goodsDetail?: string; imgUrl?: string };
        const strip = (v?: string) =>
          (v ?? "").replace(/<[^>]+>/g, " ").replace(/&nbsp;/g, " ").replace(/\s+/g, " ").trim();
        const upName = d.name?.trim() || r.name;
        const upDesc = d.goodsDescribe?.trim() || strip(d.goodsDetail);
        const upImg = d.imgUrl?.trim() || r.imgUrl || "";
        setEditTarget((t) =>
          t && t.goodsId === r.goodsId
            ? {
                ...t,
                loading: false,
                upstreamName: upName,
                upstreamDesc: upDesc,
                upstreamImg: upImg,
                name: r.customName ?? upName,
                desc: r.customDesc ?? upDesc,
                img: r.customImg ?? upImg,
              }
            : t,
        );
      })
      .catch(() => setEditTarget((t) => (t && t.goodsId === r.goodsId ? { ...t, loading: false } : t)));
  }, []);

  const saveCustom = useCallback(async () => {
    if (!editTarget) return;
    const name = editTarget.name.trim();
    const desc = editTarget.desc.trim();
    const img = editTarget.img.trim();
    // 只保存与上游不同的字段（与上游一致则视为不覆盖）
    const nameOverride = name && name !== editTarget.upstreamName ? name : "";
    const descOverride = desc && desc !== editTarget.upstreamDesc ? desc : "";
    const imgOverride = img && img !== editTarget.upstreamImg ? img : "";
    const j = await fetch("/api/admin/product-info", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        action: "save",
        goodsId: editTarget.goodsId,
        name: nameOverride,
        desc: descOverride,
        img: imgOverride,
      }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success(nameOverride || descOverride || imgOverride ? "已保存" : "已使用上游信息");
      setEditTarget(null);
      load();
    } else Message.error(j.message ?? "保存失败");
  }, [editTarget, load]);

  const clearCustom = useCallback(async () => {
    if (!editTarget) return;
    const j = await fetch("/api/admin/product-info", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "clear", goodsId: editTarget.goodsId }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已清除");
      setEditTarget(null);
      load();
    } else Message.error(j.message ?? "清除失败");
  }, [editTarget, load]);

  const hasFilter = Boolean(keyword) || rootIds.length > 0 || subIds.length > 0 || enabledList.length > 0;

  const columns = [
    {
      title: "商品",
      dataIndex: "name",
      render: (_: unknown, r: AdminProduct) => (
        <div className="flex flex-col">
          <span className="line-clamp-1 max-w-[320px]">{r.name}</span>
          <span className="font-mono text-[11px] text-[#86909c]">#{r.goodsId}</span>
        </div>
      ),
    },
    {
      title: (
        <FilterHeader label="板块" count={rootIds.length}>
          <MultiFilter title="板块多选" options={options.roots} selected={rootIds} onChange={applyRoots} />
        </FilterHeader>
      ),
      dataIndex: "rootName",
      width: 190,
      render: (_: unknown, r: AdminProduct) => (
        <span className="line-clamp-1 max-w-[170px]" title={r.rootName}>
          {r.rootName}
        </span>
      ),
    },
    {
      title: (
        <FilterHeader label="分类" count={subIds.length}>
          <MultiFilter
            title="分类多选"
            options={subOptions}
            selected={subIds}
            onChange={(ids) => {
              setSubIds(ids);
              setPage(1);
            }}
          />
        </FilterHeader>
      ),
      dataIndex: "subName",
      width: 190,
      render: (_: unknown, r: AdminProduct) =>
        r.subName ? (
          <span className="line-clamp-1 max-w-[170px] text-[#4e5969]" title={r.subName}>
            {r.subName}
          </span>
        ) : (
          <span className="text-[#c9cdd4]">-</span>
        ),
    },
    {
      title: "成本",
      dataIndex: "costPrice",
      width: 100,
      align: "right" as const,
      render: (_: unknown, r: AdminProduct) => `¥${formatPrice(r.costPrice)}`,
    },
    {
      title: "售价",
      dataIndex: "finalPrice",
      width: 110,
      align: "right" as const,
      render: (_: unknown, r: AdminProduct) => (
        <span className="font-semibold text-[#f53f3f]">¥{formatPrice(r.finalPrice)}</span>
      ),
    },
    {
      title: "库存",
      dataIndex: "stockCount",
      width: 90,
      align: "right" as const,
      render: (_: unknown, r: AdminProduct) => (r.stockCount < 0 ? "-" : r.stockCount),
    },
    {
      title: (
        <FilterHeader label="状态" count={enabledList.length}>
          <MultiFilter
            title="状态"
            options={options.status}
            selected={enabledList}
            onChange={(ids) => {
              setEnabledList(ids);
              setPage(1);
            }}
          />
        </FilterHeader>
      ),
      dataIndex: "enabled",
      width: 110,
      render: (_: unknown, r: AdminProduct) => (
        <Tag color={r.enabled === 1 ? "green" : "red"} size="small">
          {r.enabled === 1 ? "上架" : "下架"}
        </Tag>
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 90,
      fixed: "right" as const,
      render: (_: unknown, r: AdminProduct) => (
        <Dropdown
          trigger="click"
          position="br"
          droplist={
            <Menu
              onClickMenuItem={(key) => {
                if (key === "toggle") {
                  post(
                    { action: "toggle", ids: [r.goodsId], enabled: r.enabled !== 1 },
                    r.enabled === 1 ? "已下架" : "已上架",
                  );
                } else if (key === "edit") {
                  openEdit(r);
                } else if (key === "price") {
                  setPriceModal({
                    goodsId: r.goodsId,
                    name: r.name,
                    price: r.manualPrice ?? r.finalPrice,
                    locked: r.priceLocked === 1,
                  });
                } else if (key === "delete") {
                  Modal.confirm({
                    title: "删除本地商品",
                    content: "删除后商城不再展示，可重新同步 / 上架。",
                    okButtonProps: { status: "danger" },
                    onOk: () => post({ action: "delete", ids: [r.goodsId] }, "已删除"),
                  });
                }
              }}
            >
              <Menu.Item key="toggle">{r.enabled === 1 ? "下架" : "上架"}</Menu.Item>
              <Menu.Item key="edit">编辑信息</Menu.Item>
              <Menu.Item key="price">锁价</Menu.Item>
              <Menu.Item key="delete" style={{ color: "#f53f3f" }}>
                删除
              </Menu.Item>
            </Menu>
          }
        >
          <Button size="small">操作</Button>
        </Dropdown>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space wrap>
        <Input.Search
          allowClear
          placeholder="名称 / 商品编号"
          style={{ width: 220 }}
          onSearch={(v) => {
            setKeyword(v);
            setPage(1);
          }}
        />
        {hasFilter && (
          <Button size="small" onClick={resetFilters}>
            重置筛选
          </Button>
        )}
        <Button
          size="small"
          loading={syncing}
          onClick={sync}
          title="只刷新当前商品在上游的价格、库存、状态等信息，不改变上下架与自定义内容"
        >
          同步上游信息{selected.length > 0 ? `（${selected.length}）` : ""}
        </Button>
        <Button
          size="small"
          type="primary"
          disabled={selected.length === 0}
          onClick={() => post({ action: "toggle", ids: selected.map(Number), enabled: true }, "已批量上架")}
        >
          批量上架
        </Button>
        <Button
          size="small"
          status="danger"
          disabled={selected.length === 0}
          onClick={() => post({ action: "toggle", ids: selected.map(Number), enabled: false }, "已批量下架")}
        >
          批量下架
        </Button>
        <Popconfirm
          title={`确认删除选中的 ${selected.length} 个本地商品？`}
          onOk={() => post({ action: "delete", ids: selected.map(Number) }, `已删除 ${selected.length} 个商品`)}
        >
          <Button size="small" status="danger" disabled={selected.length === 0}>
            批量删除
          </Button>
        </Popconfirm>
        <Button size="small" onClick={() => post({ action: "recompute" }, "已重算售价")}>
          重算售价
        </Button>
      </Space>

      <Table
        rowKey="goodsId"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 1300 }}
        rowSelection={{ selectedRowKeys: selected, onChange: (keys) => setSelected(keys) }}
        pagination={{
          current: page,
          pageSize,
          total,
          showTotal: true,
          sizeCanChange: true,
          onChange: (p, ps) => {
            setPage(p);
            setPageSize(ps);
          },
        }}
      />

      <Modal
        title="手动锁价"
        visible={priceModal !== null}
        onCancel={() => setPriceModal(null)}
        onOk={() => {
          if (!priceModal) return;
          post({ action: "price", goodsId: priceModal.goodsId, price: priceModal.price, locked: true }, "已锁定售价");
          setPriceModal(null);
        }}
        okText="保存锁价"
        style={{ width: 420 }}
        unmountOnExit
      >
        {priceModal && (
          <div className="flex flex-col gap-3">
            <div className="text-[12px] text-[#86909c]">
              #{priceModal.goodsId} {priceModal.name}
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">锁定售价（元）</div>
              <InputNumber
                value={priceModal.price}
                min={0}
                step={0.01}
                style={{ width: "100%" }}
                onChange={(v) => setPriceModal((t) => (t ? { ...t, price: Number(v) || 0 } : t))}
              />
            </div>
            {priceModal.locked && (
              <Button
                size="small"
                status="danger"
                onClick={() => {
                  post({ action: "price", goodsId: priceModal.goodsId, price: null, locked: false }, "已解除锁价");
                  setPriceModal(null);
                }}
              >
                解除锁价
              </Button>
            )}
          </div>
        )}
      </Modal>

      <Modal
        title={editTarget ? `编辑商品信息 #${editTarget.goodsId}` : ""}
        visible={editTarget !== null}
        onCancel={() => setEditTarget(null)}
        footer={
          <Space>
            <Button onClick={() => setEditTarget(null)}>取消</Button>
            {editTarget?.custom && (
              <Button status="danger" onClick={clearCustom}>
                清除自定义
              </Button>
            )}
            <Button type="primary" onClick={saveCustom}>
              保存
            </Button>
          </Space>
        }
        style={{ width: 480 }}
        unmountOnExit
      >
        {editTarget && (
          <div className="flex flex-col gap-3">
            <div className="rounded bg-[#f7f8fa] px-2 py-1.5 text-[11px] text-[#86909c]">
              已从上游载入商品信息，可直接基于上游修改；仅保存与上游不同的字段，保存后只影响本站展示。
            </div>
            {editTarget.loading && <div className="text-[11px] text-[#165dff]">正在读取上游商品信息…</div>}
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">商品标题</div>
              <Input
                value={editTarget.name}
                onChange={(v) => setEditTarget({ ...editTarget, name: v })}
                placeholder="与上游相同则使用上游标题"
              />
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">商品描述</div>
              <Input.TextArea
                value={editTarget.desc}
                onChange={(v) => setEditTarget({ ...editTarget, desc: v })}
                autoSize={{ minRows: 2, maxRows: 4 }}
                placeholder="与上游相同则使用上游描述"
              />
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">商品图片 URL</div>
              <Input
                value={editTarget.img}
                onChange={(v) => setEditTarget({ ...editTarget, img: v })}
                placeholder="与上游相同则使用上游图片"
              />
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
