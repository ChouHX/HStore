"use client";
import { formatPrice } from "@/lib/format";

import {
  Button,
  DatePicker,
  Input,
  InputNumber,
  Message,
  Modal,
  Popconfirm,
  Radio,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
} from "@arco-design/web-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { RedeemCode } from "@/lib/types";

const PAGE_SIZE = 15;

/** 把码值列表拼成可粘贴的文本（每行一个）。 */
function codesToText(codes: string[]): string {
  return codes.join("\n");
}

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // 非安全上下文（http）下 clipboard 不可用，退回 execCommand
    try {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      const ok = document.execCommand("copy");
      document.body.removeChild(ta);
      return ok;
    } catch {
      return false;
    }
  }
}

/** 触发浏览器下载，用于导出本批码值。 */
function downloadCodes(codes: string[], filename: string) {
  const blob = new Blob([codesToText(codes)], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

const STATUS_TAG: Record<string, { color: string; label: string }> = {
  unused: { color: "green", label: "未使用" },
  redeeming: { color: "orange", label: "核销中" },
  used: { color: "gray", label: "已使用" },
  voided: { color: "red", label: "已作废" },
};

interface ProductOption {
  goodsId: number;
  name: string;
  finalPrice: number;
}

function validity(c: RedeemCode): string {
  if (!c.startAt && !c.endAt) return "永久";
  const s = c.startAt ? new Date(c.startAt).toLocaleDateString("zh-CN") : "立即";
  const e = c.endAt ? new Date(c.endAt).toLocaleDateString("zh-CN") : "永久";
  return `${s} ~ ${e}`;
}

/** 商品选择器：远程搜索后台商品 */
function ProductPicker({
  value,
  onChange,
  scope = "all",
}: {
  value: ProductOption | null;
  onChange: (v: ProductOption | null) => void;
  /** all = 全部后台商品；codeGoods = 仅兑换码实际关联到的商品（筛选用） */
  scope?: "all" | "codeGoods";
}) {
  const [options, setOptions] = useState<ProductOption[]>([]);
  const [loading, setLoading] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [allOptions, setAllOptions] = useState<ProductOption[]>([]);

  // 兑换码筛选场景：只拉一次「有兑换码的商品」，本地搜索即可。
  const loadCodeGoods = useCallback(async () => {
    setLoading(true);
    try {
      const j = await fetch("/api/admin/redeem/products").then((r) => r.json());
      if (j.ok) {
        const list = (j.items as ProductOption[]).map((p) => ({
          goodsId: p.goodsId,
          name: p.name,
          finalPrice: p.finalPrice,
        }));
        setAllOptions(list);
        setOptions(list);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  const search = useCallback(async (kw: string) => {
    setLoading(true);
    try {
      const sp = new URLSearchParams({ page: "1", pageSize: "20" });
      if (kw) sp.set("keyword", kw);
      const j = await fetch(`/api/admin/products?${sp}`).then((r) => r.json());
      if (j.ok) {
        setOptions(
          (j.items as ProductOption[]).map((p) => ({
            goodsId: p.goodsId,
            name: p.name,
            finalPrice: p.finalPrice,
          })),
        );
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (scope === "codeGoods") loadCodeGoods();
    else search("");
  }, [scope, loadCodeGoods, search]);

  return (
    <Select
      showSearch
      allowClear
      filterOption={false}
      loading={loading}
      placeholder={scope === "codeGoods" ? "选择有兑换码的商品" : "搜索商品名称 / 编号"}
      value={value?.goodsId}
      onChange={(v) => {
        const id = Number(v);
        const pool = scope === "codeGoods" ? allOptions : options;
        onChange(pool.find((o) => o.goodsId === id) ?? null);
      }}
      onSearch={(kw) => {
        if (scope === "codeGoods") {
          // 本地过滤，不发请求
          const k = kw.trim().toLowerCase();
          setOptions(
            k
              ? allOptions.filter(
                  (o) => o.name.toLowerCase().includes(k) || String(o.goodsId).includes(k),
                )
              : allOptions,
          );
          return;
        }
        if (timer.current) clearTimeout(timer.current);
        timer.current = setTimeout(() => search(kw), 300);
      }}
      style={{ width: "100%" }}
    >
      {options.map((o) => (
        <Select.Option key={o.goodsId} value={o.goodsId}>
          <span className="text-[12px]">
            <span className="font-mono text-[#86909c]">{o.goodsId}</span> {o.name}
            <span className="ml-2 text-[#f53f3f]">¥{formatPrice(o.finalPrice)}</span>
          </span>
        </Select.Option>
      ))}
    </Select>
  );
}

export function RedeemSection() {
  const [items, setItems] = useState<RedeemCode[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");
  // 商品筛选：只显示关联到某个商品的兑换码
  const [filterGoodsId, setFilterGoodsId] = useState<number>(0);
  const [filterProduct, setFilterProduct] = useState<ProductOption | null>(null);
  // 批次筛选：从「批量管理」点「查看」时设置
  const [batchFilter, setBatchFilter] = useState<number>(0);
  const hasFilter = keyword !== "" || status !== "all" || filterGoodsId > 0 || batchFilter > 0;
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  // 生成成功后弹出：展示本次生成的码值
  const [created, setCreated] = useState<{ batchId: number; codes: string[] } | null>(null);

  // 批量管理
  const [selected, setSelected] = useState<number[]>([]);
  const [batchMode, setBatchMode] = useState(false);
  const [batches, setBatches] = useState<
    { batchId: number; total: number; unused: number; used: number; voided: number; faceValue: number; goodsId: number | null; goodsName: string; goodsCount: number; createdAt: number }[]
  >([]);
  const [batchPage, setBatchPage] = useState(1);
  const [batchTotal, setBatchTotal] = useState(0);
  const [batchLoading, setBatchLoading] = useState(false);

  const [kind, setKind] = useState<"balance" | "product">("balance");
  const [product, setProduct] = useState<ProductOption | null>(null);
  const [form, setForm] = useState({
    faceValue: 10,
    goodsCount: 1,
    count: 10,
    startAt: "",
    endAt: "",
  });

  // 「修改」弹窗：调整兑换码关联的商品 / 面值
  const [editing, setEditing] = useState<RedeemCode | null>(null);
  const [editProduct, setEditProduct] = useState<ProductOption | null>(null);
  const [editCount, setEditCount] = useState(1);
  const [editFace, setEditFace] = useState(10);

  // 已使用 / 核销中的兑换码只能查看明细，不允许改关联。
  const readOnly = editing !== null && editing.status !== "unused" && editing.status !== "voided";

  // 「批量修改」弹窗：把当前筛选条件（或选中项）作为范围，统一改关联/面值/有效期
  const [bulkOpen, setBulkOpen] = useState(false);
  const [bulkScope, setBulkScope] = useState<"filter" | "selected">("filter");
  const [bulkType, setBulkType] = useState<"keep" | "goods" | "face">("keep");
  const [bulkDates, setBulkDates] = useState<"keep" | "set" | "clear">("keep");
  const [bulkProduct, setBulkProduct] = useState<ProductOption | null>(null);
  const [bulkCount, setBulkCount] = useState(1);
  const [bulkFace, setBulkFace] = useState(10);
  const [bulkStart, setBulkStart] = useState("");
  const [bulkEnd, setBulkEnd] = useState("");
  const [preview, setPreview] = useState<{ total: number; updatable: number } | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);

  const query = useCallback(() => {
    const sp = new URLSearchParams();
    if (status !== "all") sp.set("status", status);
    if (keyword) sp.set("keyword", keyword);
    if (filterGoodsId > 0) sp.set("goodsId", String(filterGoodsId));
    if (batchFilter > 0) sp.set("batchId", String(batchFilter));
    return sp;
  }, [status, keyword, filterGoodsId, batchFilter]);

  // 批量修改接口要的是筛选条件对象（字段与列表查询参数一致）。
  const filterBody = useCallback(() => {
    const f: Record<string, unknown> = {};
    if (status !== "all") f.status = status;
    if (keyword) f.keyword = keyword;
    if (filterGoodsId > 0) f.goodsId = filterGoodsId;
    if (batchFilter > 0) f.batchId = batchFilter;
    return f;
  }, [status, keyword, filterGoodsId, batchFilter]);

  const filterText = useCallback(() => {
    const parts: string[] = [];
    if (keyword) parts.push(`关键词“${keyword}”`);
    if (status !== "all") parts.push(`状态 ${STATUS_TAG[status]?.label ?? status}`);
    if (filterProduct) parts.push(`商品 ${filterProduct.name || filterProduct.goodsId}`);
    if (batchFilter > 0) parts.push(`批次 ${batchFilter}`);
    return parts.length > 0 ? parts.join("、") : "全部兑换码";
  }, [keyword, status, filterProduct, batchFilter]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const sp = query();
      sp.set("page", String(page));
      sp.set("pageSize", String(pageSize));
      const j = await fetch(`/api/admin/redeem?${sp}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as RedeemCode[]);
        setTotal(j.total as number);
      } else Message.error(j.message);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, query]);

  useEffect(() => {
    load();
  }, [load]);

  function resetForm() {
    setKind("balance");
    setProduct(null);
    setForm({ faceValue: 10, goodsCount: 1, count: 10, startAt: "", endAt: "" });
  }

  async function create() {
    if (kind === "product" && !product) {
      Message.warning("请选择兑换码关联的商品");
      return;
    }
    if (kind === "balance" && form.faceValue <= 0) {
      Message.warning("面值必须大于 0");
      return;
    }
    setSaving(true);
    try {
      const j = await fetch("/api/admin/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          action: "create",
          kind,
          faceValue: kind === "balance" ? form.faceValue : 0,
          goodsId: kind === "product" ? product?.goodsId : null,
          goodsCount: kind === "product" ? form.goodsCount : 0,
          count: form.count,
          startAt: form.startAt ? new Date(form.startAt).getTime() : null,
          endAt: form.endAt ? new Date(form.endAt).getTime() : null,
        }),
      }).then((r) => r.json());
      if (j.ok) {
        const codes = (j.codes as string[]) ?? [];
        Message.success(`已生成 ${j.count} 张兑换码`);
        setOpen(false);
        resetForm();
        setSelected([]);
        load();
        if (codes.length > 0) {
          setCreated({ batchId: Number(j.batchId) || 0, codes });
        }
      } else Message.error(j.message ?? "生成失败");
    } finally {
      setSaving(false);
    }
  }

  async function voidCode(codeId: number) {    const j = await fetch("/api/admin/redeem", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "voidCode", codeId }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已作废");
      load();
    } else Message.error(j.message ?? "作废失败");
  }

  async function deleteCode(codeId: number) {
    const j = await fetch("/api/admin/redeem", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "deleteCode", codeId }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已删除");
      load();
    } else Message.error(j.message ?? "删除失败");
  }

  async function batchAction(action: "voidCodes" | "deleteCodes" | "voidBatch" | "deleteBatch", payload: { codeIds?: number[]; batchId?: number }) {
    setSaving(true);
    try {
      const j = await fetch("/api/admin/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action, ...payload }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(`已处理 ${j.count} 张兑换码`);
        setSelected([]);
        load();
        if (batchMode) loadBatches();
      } else Message.error(j.message ?? "操作失败");
    } finally {
      setSaving(false);
    }
  }

  // 打开批量修改弹窗时重置表单，并按默认范围拉一次预览。
  const openBulk = useCallback((scope: "filter" | "selected") => {
    setBulkScope(scope);
    setBulkType("keep");
    setBulkDates("keep");
    setBulkProduct(null);
    setBulkCount(1);
    setBulkFace(10);
    setBulkStart("");
    setBulkEnd("");
    setPreview(null);
    setBulkOpen(true);
  }, []);

  const loadPreview = useCallback(async () => {
    setPreviewLoading(true);
    try {
      const sp = query();
      const j = await fetch(`/api/admin/redeem/count?${sp}`).then((r) => r.json());
      if (j.ok) {
        setPreview({ total: Number(j.total) || 0, updatable: Number(j.updatable) || 0 });
      } else Message.error(j.message ?? "统计失败");
    } finally {
      setPreviewLoading(false);
    }
  }, [query]);

  useEffect(() => {
    if (!bulkOpen || bulkScope !== "filter") return;
    void loadPreview();
  }, [bulkOpen, bulkScope, loadPreview]);

  // 批量修改：只改未使用 / 已作废的码，已使用与核销中的码后端会自动跳过。
  async function saveBulk() {
    const update: Record<string, unknown> = {};
    if (bulkType === "goods") {
      if (!bulkProduct) {
        Message.warning("请选择要关联的商品");
        return;
      }
      update.goodsId = bulkProduct.goodsId;
      update.goodsCount = bulkCount > 0 ? bulkCount : 1;
    } else if (bulkType === "face") {
      if (bulkFace <= 0) {
        Message.warning("面值必须大于 0");
        return;
      }
      update.faceValue = bulkFace;
    }
    if (bulkDates === "set") {
      if (!bulkStart && !bulkEnd) {
        Message.warning("请选择生效或截止时间");
        return;
      }
      const startMS = bulkStart ? new Date(bulkStart).getTime() : 0;
      const endMS = bulkEnd ? new Date(bulkEnd).getTime() : 0;
      if (startMS && endMS && startMS > endMS) {
        Message.warning("开始时间不能晚于结束时间");
        return;
      }
      if (startMS) update.startAt = startMS;
      if (endMS) update.endAt = endMS;
    } else if (bulkDates === "clear") {
      update.clearDates = true;
    }
    if (Object.keys(update).length === 0) {
      Message.warning("请选择要修改的内容");
      return;
    }
    if (bulkScope === "selected" && selected.length === 0) {
      Message.warning("请先勾选要修改的兑换码");
      return;
    }
    setSaving(true);
    try {
      const j = await fetch("/api/admin/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          action: "updateCodes",
          // 按选中项修改时不叠加筛选条件，否则勾选了又改筛选会静默漏改。
          filter: bulkScope === "selected" ? {} : filterBody(),
          codeIds: bulkScope === "selected" ? selected : undefined,
          update,
        }),
      }).then((r) => r.json());
      if (j.ok) {
        const n = Number(j.count) || 0;
        Message.success(
          n > 0
            ? `已修改 ${n} 张兑换码`
            : "没有可修改的兑换码（已使用 / 核销中的不会被修改）",
        );
        setBulkOpen(false);
        setSelected([]);
        load();
        if (batchMode) loadBatches();
      } else Message.error(j.message ?? "批量修改失败");
    } finally {
      setSaving(false);
    }
  }

  const loadBatches = useCallback(async () => {
    setBatchLoading(true);
    try {
      const j = await fetch(`/api/admin/redeem/batches?page=${batchPage}&pageSize=10`).then((r) =>
        r.json(),
      );
      if (j.ok) {
        setBatches(j.items);
        setBatchTotal(j.total as number);
      } else Message.error(j.message);
    } finally {
      setBatchLoading(false);
    }
  }, [batchPage]);

  useEffect(() => {
    if (batchMode) loadBatches();
  }, [batchMode, loadBatches]);

  async function saveEdit() {
    if (!editing) return;
    if (readOnly) {
      setEditing(null);
      return;
    }
    if (!editProduct && editFace <= 0) {
      Message.warning("面值必须大于 0");
      return;
    }
    setSaving(true);
    try {
      const j = await fetch("/api/admin/redeem", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          action: "updateGoods",
          codeId: editing.id,
          newGoodsId: editProduct?.goodsId ?? null,
          goodsCount: editProduct ? editCount : 0,
          faceValue: editProduct ? 0 : editFace,
        }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("已保存");
        setEditing(null);
        load();
      } else Message.error(j.message ?? "保存失败");
    } finally {
      setSaving(false);
    }
  }

  const columns = [
    {
      title: "状态",
      dataIndex: "status",
      width: 90,
      render: (_: unknown, c: RedeemCode) => (
        <Tag size="small" color={STATUS_TAG[c.status]?.color}>
          {STATUS_TAG[c.status]?.label ?? c.status}
        </Tag>
      ),
    },
    {
      title: "兑换码",
      dataIndex: "code",
      // 兑换码是最需要看清的一列：占满剩余空间，不再被固定列宽挤压。
      render: (_: unknown, c: RedeemCode) => (
        <span className="whitespace-nowrap font-mono select-all text-[13px]">{c.code}</span>
      ),
    },
    {
      title: "商品ID",
      dataIndex: "goodsId",
      width: 120,
      render: (_: unknown, c: RedeemCode) =>
        c.goodsId ? (
          <Tooltip content={c.goodsName || `商品 ${c.goodsId}`}>
            <span className="cursor-help font-mono text-[12px] text-[#165dff] underline decoration-dotted">
              {c.goodsId}
              {c.goodsCount && c.goodsCount > 1 ? ` ×${c.goodsCount}` : ""}
            </span>
          </Tooltip>
        ) : (
          <Tooltip content="面值码：兑换后充入余额">
            <span className="cursor-help text-[12px] text-[#f53f3f]">
              ¥{formatPrice(c.faceValue)}
            </span>
          </Tooltip>
        ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 160,
      fixed: "right" as const,
      render: (_: unknown, c: RedeemCode) => {
        // 已使用/核销中的码不允许再改关联，但仍可删除（清理历史数据）。
        const editable = c.status === "unused" || c.status === "voided";
        return (
          <Space size="small">
            <Button
              size="mini"
              onClick={() => {
                setEditing(c);
                setEditProduct(
                  c.goodsId ? { goodsId: c.goodsId, name: c.goodsName || "", finalPrice: 0 } : null,
                );
                setEditCount(c.goodsCount || 1);
                setEditFace(c.faceValue || 10);
              }}
            >
              {editable ? "修改" : "详情"}
            </Button>
            <Popconfirm
              title={
                c.status === "used"
                  ? `该兑换码已使用（订单 ${c.orderNumber || "-"}），删除只影响兑换码记录，不影响已生成的订单。确认删除？`
                  : "确认删除该兑换码？删除后不可恢复。"
              }
              onOk={() => deleteCode(c.id)}
            >
              <Button size="mini" status="danger">
                删除
              </Button>
            </Popconfirm>
          </Space>
        );
      },
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space wrap>
        <Input.Search
          allowClear
          value={keyword}
          placeholder="搜索兑换码"
          style={{ width: 220 }}
          // 兑换码搜索不需要浏览器历史/密码管理器介入，关闭自动填充
          autoComplete="off"
          name="redeem-code-search"
          onChange={(v) => {
            setKeyword(v);
            if (!v) setPage(1);
          }}
          onSearch={(v) => {
            setKeyword(v);
            setPage(1);
          }}
        />
        <Select
          value={status}
          onChange={(v) => {
            setStatus(String(v));
            setPage(1);
          }}
          style={{ width: 140 }}
        >
          <Select.Option value="all">全部状态</Select.Option>
          <Select.Option value="unused">未使用</Select.Option>
          <Select.Option value="used">已使用</Select.Option>
          <Select.Option value="voided">已作废</Select.Option>
        </Select>
        <div style={{ width: 260 }}>
          <ProductPicker
            scope="codeGoods"
            value={filterProduct}
            onChange={(v) => {
              setFilterProduct(v);
              setFilterGoodsId(v?.goodsId ?? 0);
              setPage(1);
            }}
          />
        </div>
        {hasFilter && (
          <Button
            size="small"
            onClick={() => {
              setKeyword("");
              setStatus("all");
              setFilterGoodsId(0);
              setFilterProduct(null);
              setBatchFilter(0);
              setPage(1);
            }}
          >
            重置筛选
          </Button>
        )}
        <Button
          size="small"
          onClick={() => window.open(`/api/admin/redeem?export=1&${query().toString()}`, "_blank")}
        >
          导出 CSV
        </Button>
        {!batchMode && (
          <Button size="small" onClick={() => openBulk("filter")}>
            批量修改
          </Button>
        )}
        <Button size="small" type="primary" onClick={() => setOpen(true)}>
          生成兑换码
        </Button>
        <Button
          size="small"
          type={batchMode ? "primary" : "secondary"}
          onClick={() => {
            setBatchMode((v) => !v);
            setSelected([]);
          }}
        >
          {batchMode ? "返回码列表" : "批量管理"}
        </Button>
      </Space>

      {batchMode ? (
        <Table
          rowKey="batchId"
          loading={batchLoading}
          data={batches}
          scroll={{ x: "100%" }}
          pagination={{
            current: batchPage,
            pageSize: 10,
            total: batchTotal,
            showTotal: true,
            onChange: (p) => setBatchPage(p),
          }}
          columns={[
            {
              title: "批次号",
              dataIndex: "batchId",
              width: 150,
              render: (v: number) => <span className="font-mono text-[12px]">{v}</span>,
            },
            {
              title: "类型",
              dataIndex: "goodsId",
              width: 180,
              render: (_: unknown, b: (typeof batches)[number]) =>
                b.goodsId ? (
                  <Tooltip content={b.goodsName || `商品 ${b.goodsId}`}>
                    <span className="text-[12px]">
                      {b.goodsName || `商品 ${b.goodsId}`}
                      {b.goodsCount > 1 ? ` ×${b.goodsCount}` : ""}
                    </span>
                  </Tooltip>
                ) : (
                  <span className="text-[12px] text-[#f53f3f]">¥{formatPrice(b.faceValue)}</span>
                ),
            },
            {
              title: "总量",
              dataIndex: "total",
              width: 80,
              render: (v: number) => <b>{v}</b>,
            },
            {
              title: "状态分布",
              dataIndex: "dist",
              render: (_: unknown, b: (typeof batches)[number]) => (
                <Space size="mini" wrap>
                  <Tag size="small" color="green">
                    未用 {b.unused}
                  </Tag>
                  <Tag size="small" color="gray">
                    已用 {b.used}
                  </Tag>
                  {b.voided > 0 && (
                    <Tag size="small" color="red">
                      作废 {b.voided}
                    </Tag>
                  )}
                </Space>
              ),
            },
            {
              title: "生成时间",
              dataIndex: "createdAt",
              width: 160,
              render: (v: number) => (
                <span className="text-[12px] text-[#86909c]">
                  {new Date(v).toLocaleString("zh-CN")}
                </span>
              ),
            },
            {
              title: "操作",
              dataIndex: "op",
              width: 230,
              fixed: "right" as const,
              render: (_: unknown, b: (typeof batches)[number]) => (
                <Space size="mini">
                  <Button
                    size="mini"
                    onClick={() => {
                      setBatchMode(false);
                      setPage(1);
                      // 复用码列表页，按批次筛选
                      setFilterGoodsId(0);
                      setFilterProduct(null);
                      setKeyword("");
                      setStatus("all");
                      setBatchFilter(b.batchId);
                    }}
                  >
                    查看
                  </Button>
                  <Popconfirm
                    title={`作废批次 ${b.batchId} 中全部未使用兑换码（${b.unused} 张）？`}
                    onOk={() => batchAction("voidBatch", { batchId: b.batchId })}
                    disabled={b.unused === 0}
                  >
                    <Button size="mini" status="warning" disabled={b.unused === 0}>
                      整批作废
                    </Button>
                  </Popconfirm>
                  <Popconfirm
                    title={`删除批次 ${b.batchId} 中未使用/已作废的兑换码？已使用的会保留。`}
                    onOk={() => batchAction("deleteBatch", { batchId: b.batchId })}
                  >
                    <Button size="mini" status="danger">
                      整批删除
                    </Button>
                  </Popconfirm>
                </Space>
              ),
            },
          ]}
        />
      ) : (
      <>
      {selected.length > 0 && (
        <Space wrap>
          <span className="text-[12px] text-[#86909c]">已选 {selected.length} 张</span>
          <Popconfirm
            title={`作废选中的 ${selected.length} 张兑换码？（仅未使用/核销中的会生效）`}
            onOk={() => batchAction("voidCodes", { codeIds: selected })}
          >
            <Button size="small" status="warning" loading={saving}>
              批量作废
            </Button>
          </Popconfirm>
          <Popconfirm
            title={`删除选中的 ${selected.length} 张兑换码？已使用的会保留。`}
            onOk={() => batchAction("deleteCodes", { codeIds: selected })}
          >
            <Button size="small" status="danger" loading={saving}>
              批量删除
            </Button>
          </Popconfirm>
          <Button size="small" onClick={() => openBulk("selected")}>
            批量修改
          </Button>
          <Button
            size="small"
            onClick={() => {
              const codes = items
                .filter((c) => selected.includes(c.id))
                .map((c) => c.code);
              void copyText(codesToText(codes)).then((ok) =>
                ok ? Message.success(`已复制 ${codes.length} 张兑换码`) : Message.error("复制失败"),
              );
            }}
          >
            复制选中
          </Button>
          <Button size="small" onClick={() => setSelected([])}>
            取消选择
          </Button>
        </Space>
      )}

      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: "100%" }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (keys) => setSelected(keys as number[]),
        }}
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
      </>
      )}

      {/* 批量修改：按当前筛选条件（或选中项）统一改关联商品 / 面值 / 有效期 */}
      <Modal
        title="批量修改兑换码"
        visible={bulkOpen}
        onCancel={() => setBulkOpen(false)}
        onOk={saveBulk}
        confirmLoading={saving}
        okText="确认修改"
        style={{ width: 560 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">修改范围</div>
            <Radio.Group
              type="button"
              value={bulkScope}
              onChange={(v) => setBulkScope(v as "filter" | "selected")}
            >
              <Radio value="filter">当前筛选结果</Radio>
              <Radio value="selected" disabled={selected.length === 0}>
                已选 {selected.length} 张
              </Radio>
            </Radio.Group>
            <div className="mt-1 text-[12px] text-[#86909c]">
              {bulkScope === "filter" ? (
                <>
                  筛选条件：{filterText()}
                  {previewLoading
                    ? "，统计中…"
                    : preview
                      ? `，共 ${preview.total} 张，其中可修改 ${preview.updatable} 张`
                      : ""}
                </>
              ) : (
                <>将对勾选的 {selected.length} 张兑换码生效</>
              )}
            </div>
          </div>

          <div className="rounded bg-[#fff7e8] px-3 py-2 text-[11px] leading-relaxed text-[#86909c]">
            已使用与核销中的兑换码不会被修改（保留原关联以便追溯订单）；
            已作废的兑换码会一并修改，若需要重新可用请另外处理状态。
          </div>

          {bulkScope === "filter" && !hasFilter && (
            <div className="rounded bg-[#ffece8] px-3 py-2 text-[11px] leading-relaxed text-[#f53f3f]">
              当前没有任何筛选条件，确认后会作用于全部兑换码。建议先按状态 / 商品 /
              批次缩小范围。
            </div>
          )}

          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">兑换码类型</div>
            <Radio.Group
              type="button"
              value={bulkType}
              onChange={(v) => setBulkType(v as "keep" | "goods" | "face")}
            >
              <Radio value="keep">不修改</Radio>
              <Radio value="goods">改为商品码</Radio>
              <Radio value="face">改为面值码</Radio>
            </Radio.Group>
          </div>

          {bulkType === "goods" && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="sm:col-span-2">
                <div className="mb-1 text-[12px] text-[#4e5969]">关联商品</div>
                <ProductPicker value={bulkProduct} onChange={setBulkProduct} />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">每码兑换件数</div>
                <InputNumber
                  value={bulkCount}
                  min={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setBulkCount(Number(v) || 1)}
                />
              </div>
            </div>
          )}

          {bulkType === "face" && (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">面值（元）</div>
              <InputNumber
                value={bulkFace}
                min={0.01}
                step={1}
                style={{ width: "100%" }}
                onChange={(v) => setBulkFace(Number(v) || 0)}
              />
            </div>
          )}

          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">有效期</div>
            <Radio.Group
              type="button"
              value={bulkDates}
              onChange={(v) => setBulkDates(v as "keep" | "set" | "clear")}
            >
              <Radio value="keep">不修改</Radio>
              <Radio value="set">重新设置</Radio>
              <Radio value="clear">清空（永久有效）</Radio>
            </Radio.Group>
          </div>

          {bulkDates === "set" && (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">生效时间（空=立即）</div>
                <DatePicker
                  showTime
                  style={{ width: "100%" }}
                  value={bulkStart}
                  onChange={(s) => setBulkStart(s)}
                />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">截止时间（空=永久）</div>
                <DatePicker
                  showTime
                  style={{ width: "100%" }}
                  value={bulkEnd}
                  onChange={(s) => setBulkEnd(s)}
                />
              </div>
            </div>
          )}
        </div>
      </Modal>

      <Modal
        title="生成兑换码"
        visible={open}
        onCancel={() => setOpen(false)}
        onOk={create}
        confirmLoading={saving}
        okText="生成"
        style={{ width: 520 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">兑换码类型</div>
            <Radio.Group
              type="button"
              value={kind}
              onChange={(v) => setKind(v as "balance" | "product")}
            >
              <Radio value="balance">面值码（充余额）</Radio>
              <Radio value="product">商品码（自动下单取件）</Radio>
            </Radio.Group>
          </div>

          {kind === "product" ? (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="sm:col-span-2">
                <div className="mb-1 text-[12px] text-[#4e5969]">关联商品</div>
                <ProductPicker value={product} onChange={setProduct} />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">每码兑换件数</div>
                <InputNumber
                  value={form.goodsCount}
                  min={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setForm({ ...form, goodsCount: Number(v) || 1 })}
                />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">生成数量</div>
                <InputNumber
                  value={form.count}
                  min={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setForm({ ...form, count: Number(v) || 1 })}
                />
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">面值（元）</div>
                <InputNumber
                  value={form.faceValue}
                  min={0.01}
                  step={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setForm({ ...form, faceValue: Number(v) || 0 })}
                />
              </div>
              <div>
                <div className="mb-1 text-[12px] text-[#4e5969]">生成数量</div>
                <InputNumber
                  value={form.count}
                  min={1}
                  style={{ width: "100%" }}
                  onChange={(v) => setForm({ ...form, count: Number(v) || 1 })}
                />
              </div>
            </div>
          )}

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">生效时间（空=立即）</div>
              <DatePicker
                showTime
                style={{ width: "100%" }}
                value={form.startAt}
                onChange={(s) => setForm({ ...form, startAt: s })}
              />
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">截止时间（空=永久）</div>
              <DatePicker
                showTime
                style={{ width: "100%" }}
                value={form.endAt}
                onChange={(s) => setForm({ ...form, endAt: s })}
              />
            </div>
          </div>

          {kind === "product" && (
            <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[11px] leading-relaxed text-[#86909c]">
              用户核销商品码时会自动调用上游下单接口取货，兑换码与生成的订单号绑定，
              用户可在「我的订单」中查看卡密，或直接打开邮箱取件。
            </div>
          )}
        </div>
      </Modal>

      <Modal
        title={editing ? `修改兑换码 ${editing.code}` : "修改兑换码"}
        visible={editing !== null}
        onCancel={() => setEditing(null)}
        onOk={saveEdit}
        confirmLoading={saving}
        okText="保存"
        okButtonProps={{ style: readOnly ? { display: "none" } : undefined }}
        cancelText={readOnly ? "关闭" : "取消"}
        style={{ width: 480 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          {editing && editing.status === "used" && (
            <div className="rounded bg-[#f7f8fa] px-3 py-2 text-[12px] text-[#4e5969]">
              该兑换码已于
              {editing.usedAt ? ` ${new Date(editing.usedAt).toLocaleString("zh-CN")} ` : " "}
              被使用，关联订单 <span className="font-mono">{editing.orderNumber || "-"}</span>，
              已使用的兑换码不能修改关联关系。
            </div>
          )}
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">
              关联商品（清空则改为面值码；已是自营商品请到「自营商品」页导入卡密）
            </div>
            <ProductPicker value={editProduct} onChange={readOnly ? () => {} : setEditProduct} />
          </div>
          {editProduct ? (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">每码兑换件数</div>
              <InputNumber
                value={editCount}
                min={1}
                disabled={readOnly}
                style={{ width: "100%" }}
                onChange={(v) => setEditCount(Number(v) || 1)}
              />
            </div>
          ) : (
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">面值（元）</div>
              <InputNumber
                value={editFace}
                min={0.01}
                step={1}
                disabled={readOnly}
                style={{ width: "100%" }}
                onChange={(v) => setEditFace(Number(v) || 0)}
              />
            </div>
          )}
        </div>
      </Modal>

      {/* 生成成功后展示本次生成的码值，便于立即分发 */}
      <Modal
        title={created ? `本次生成 ${created.codes.length} 张兑换码` : "生成结果"}
        visible={created !== null}
        onCancel={() => setCreated(null)}
        footer={
          <Space>
            <Button onClick={() => setCreated(null)}>关闭</Button>
            <Button
              onClick={() => {
                if (!created) return;
                downloadCodes(created.codes, `redeem-${created.batchId || "codes"}.txt`);
              }}
            >
              下载 TXT
            </Button>
            <Button
              type="primary"
              onClick={() => {
                if (!created) return;
                void copyText(codesToText(created.codes)).then((ok) =>
                  ok
                    ? Message.success(`已复制 ${created.codes.length} 张兑换码`)
                    : Message.error("复制失败，请手动选择"),
                );
              }}
            >
              复制全部
            </Button>
          </Space>
        }
        style={{ width: 560 }}
        unmountOnExit
      >
        {created && (
          <div className="flex flex-col gap-2">
            <div className="text-[12px] text-[#86909c]">
              批次号 <span className="font-mono">{created.batchId}</span>
              {created.batchId > 0 && "，可在「批量管理」中整批作废或删除"}
            </div>
            <div
              className="max-h-[320px] overflow-auto rounded border border-[#e5e6eb] bg-[#f7f8fa] p-3 font-mono text-[13px] leading-relaxed select-all"
            >
              {created.codes.map((c) => (
                <div key={c}>{c}</div>
              ))}
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
