"use client";
import { formatPrice } from "@/lib/format";

import {
  Button,
  Empty,
  Form,
  Input,
  InputNumber,
  Message,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { SelfStockFileUploader } from "@/components/admin/SelfStockFileUploader";

const PAGE_SIZE = 20;

interface SelfProduct {
  goodsId: number;
  name: string;
  description: string;
  imgUrl: string;
  finalPrice: number;
  costPrice: number;
  enabled: number;
  updatedAt: number;
  stockAvailable: number;
  stockSold: number;
}

interface SelfStock {
  id: number;
  goodsId: number;
  goodsName: string;
  content: string;
  status: string;
  orderNumber: string;
  source: string;
  createdAt: number;
  soldAt: number;
}

const STOCK_TAG: Record<string, { color: string; label: string }> = {
  available: { color: "green", label: "未售出" },
  reserved: { color: "orange", label: "锁定中" },
  sold: { color: "gray", label: "已售出" },
  voided: { color: "red", label: "已作废" },
};

// ===== 商品管理 =====

function ProductsTab({
  items,
  loading,
  onRefresh,
  onOpenImportFor,
}: {
  items: SelfProduct[];
  loading: boolean;
  onRefresh: () => void;
  onOpenImportFor: (goodsId: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editing, setEditing] = useState<SelfProduct | null>(null);
  const [form] = Form.useForm();

  function openCreate() {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ price: undefined, costPrice: undefined, enabled: true });
    setOpen(true);
  }

  function openEdit(row: SelfProduct) {
    setEditing(row);
    form.setFieldsValue({
      name: row.name,
      price: row.finalPrice,
      costPrice: row.costPrice,
      description: row.description,
      imgUrl: row.imgUrl,
      enabled: row.enabled === 1,
    });
    setOpen(true);
  }

  async function save() {
    let values: Record<string, unknown>;
    try {
      values = await form.validate();
    } catch {
      return;
    }
    setSaving(true);
    try {
      const body = {
        action: editing ? "update" : "create",
        goodsId: editing?.goodsId ?? 0,
        name: values.name,
        price: Number(values.price) || 0,
        costPrice: values.costPrice,
        description: values.description ?? "",
        imgUrl: values.imgUrl ?? "",
        enabled: values.enabled !== false,
      };
      const j = await fetch("/api/admin/self/products", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(editing ? "已保存" : `已创建，商品编号 ${j.goodsId}`);
        setOpen(false);
        onRefresh();
      } else Message.error(j.message ?? "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function toggle(row: SelfProduct) {
    const j = await fetch("/api/admin/self/products", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "toggle", goodsId: row.goodsId, enabled: row.enabled !== 1 }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success(row.enabled === 1 ? "已下架" : "已上架");
      onRefresh();
    } else Message.error(j.message ?? "操作失败");
  }

  async function remove(row: SelfProduct) {
    const j = await fetch("/api/admin/self/products", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "delete", goodsId: row.goodsId }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success(j.message || "已删除");
      onRefresh();
    } else Message.error(j.message ?? "删除失败");
  }

  const columns = [
    {
      title: "商品名称",
      dataIndex: "name",
      render: (v: string, row: SelfProduct) => (
        <div className="flex min-w-0 items-center gap-2">
          {row.imgUrl ? (
            <img
              src={row.imgUrl}
              alt=""
              loading="lazy"
              className="h-9 w-9 shrink-0 rounded border border-[#e5e6eb] object-cover"
              onError={(e) => {
                (e.currentTarget as HTMLImageElement).style.visibility = "hidden";
              }}
            />
          ) : (
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded border border-[#e5e6eb] bg-[#f7f8fa] text-[11px] text-[#c9cdd4]">
              无图
            </div>
          )}
          <div className="min-w-0">
            <div className="break-words">{v}</div>
            <div className="mt-0.5 font-mono text-[11px] text-[#86909c]">编号 {row.goodsId}</div>
          </div>
        </div>
      ),
    },
    {
      title: "售价",
      dataIndex: "finalPrice",
      width: 120,
      render: (v: number) => <span className="text-[#f53f3f]">¥{formatPrice(v)}</span>,
    },
    {
      title: "可用库存",
      dataIndex: "stockAvailable",
      width: 110,
      render: (v: number) =>
        v > 0 ? <span className="font-medium text-[#00b42a]">{v}</span> : <span className="text-[#f53f3f]">0（需补货）</span>,
    },
    { title: "已售", dataIndex: "stockSold", width: 80 },
    {
      title: "状态",
      dataIndex: "enabled",
      width: 90,
      render: (v: number) =>
        v === 1 ? (
          <Tag size="small" color="green">
            已上架
          </Tag>
        ) : (
          <Tag size="small" color="gray">
            已下架
          </Tag>
        ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 260,
      fixed: "right" as const,
      render: (_: unknown, row: SelfProduct) => (
        <Space size="mini">
          <Button size="mini" type="primary" onClick={() => onOpenImportFor(row.goodsId)}>
            导入卡密
          </Button>
          <Button size="mini" onClick={() => openEdit(row)}>
            编辑
          </Button>
          <Button size="mini" onClick={() => toggle(row)}>
            {row.enabled === 1 ? "下架" : "上架"}
          </Button>
          <Popconfirm
            title="确认删除该自营商品？有订单的商品将改为下架。"
            onOk={() => remove(row)}
          >
            <Button size="mini" status="danger">
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space>
        <Button size="small" type="primary" onClick={openCreate}>
          新建自营商品
        </Button>
        <Button size="small" loading={loading} onClick={onRefresh}>
          刷新
        </Button>
      </Space>

      <Table
        rowKey="goodsId"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 940 }}
        pagination={false}
        noDataElement={<Empty description="还没有自营商品，点击「新建自营商品」开始" />}
      />

      <Modal
        title={editing ? `编辑自营商品 #${editing.goodsId}` : "新建自营商品"}
        visible={open}
        onCancel={() => setOpen(false)}
        onOk={save}
        confirmLoading={saving}
        okText="保存"
        style={{ width: 560, maxWidth: "95vw" }}
        unmountOnExit
      >
        <Form form={form} layout="vertical">
          <Form.Item label="商品名称" field="name" rules={[{ required: true, message: "请填写商品名称" }]}>
            <Input placeholder="例如：Outlook 双令牌绑定辅邮" />
          </Form.Item>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Form.Item
              label="售价（元）"
              field="price"
              extra="自营商品独立定价，不与上游商品联动"
              rules={[{ required: true, message: "请填写售价" }]}
            >
              <InputNumber min={0.0001} step={0.01} precision={4} style={{ width: "100%" }} />
            </Form.Item>
            <Form.Item label="成本价（元，留空同售价）" field="costPrice">
              <InputNumber min={0} step={0.01} precision={4} style={{ width: "100%" }} />
            </Form.Item>
          </div>
          <Form.Item label="商品描述" field="description">
            <Input.TextArea placeholder="展示在商品详情页的描述" autoSize={{ minRows: 2, maxRows: 5 }} />
          </Form.Item>
          <Form.Item label="图片地址" field="imgUrl">
            <Input placeholder="https://... 留空使用默认图" />
          </Form.Item>
          <Form.Item label="上架" field="enabled" triggerPropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}

// ===== 卡密管理 =====

function StocksTab({
  products,
  onRefreshProducts,
  externalImportGoodsId,
  onClearExternalImport,
}: {
  products: SelfProduct[];
  onRefreshProducts: () => void;
  externalImportGoodsId: number | null;
  onClearExternalImport: () => void;
}) {
  const [goodsId, setGoodsId] = useState<number>(0);
  const [importGoodsId, setImportGoodsId] = useState<number>(0);
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [items, setItems] = useState<SelfStock[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [importOpen, setImportOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (products.length > 0 && importGoodsId === 0) {
      setImportGoodsId(products[0].goodsId);
    }
  }, [products, importGoodsId]);

  useEffect(() => {
    if (externalImportGoodsId && externalImportGoodsId > 0) {
      setGoodsId(externalImportGoodsId);
      setImportGoodsId(externalImportGoodsId);
      setPage(1);
      setSelected([]);
      setImportOpen(true);
      onClearExternalImport();
    }
  }, [externalImportGoodsId, onClearExternalImport]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const sp = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
      if (goodsId > 0) sp.set("goodsId", String(goodsId));
      if (status !== "all") sp.set("status", status);
      if (keyword.trim()) sp.set("keyword", keyword.trim());
      const j = await fetch(`/api/admin/self/stocks?${sp}`).then((r) => r.json());
      if (j.ok) {
        setItems(j.items as SelfStock[]);
        setTotal(j.total as number);
      } else Message.error(j.message ?? "加载失败");
    } finally {
      setLoading(false);
    }
  }, [goodsId, status, keyword, page, pageSize]);

  useEffect(() => {
    void load();
  }, [load]);

  function openImportModal() {
    setImportGoodsId(goodsId > 0 ? goodsId : products[0]?.goodsId ?? 0);
    setImportOpen(true);
  }

  async function removeOne(row: SelfStock) {
    const j = await fetch("/api/admin/self/stocks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "delete", goodsId: row.goodsId, stockId: row.id }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已删除");
      void load();
      onRefreshProducts();
    } else Message.error(j.message ?? "删除失败");
  }

  async function removeBatch() {
    if (selected.length === 0) return;
    setSaving(true);
    try {
      const j = await fetch("/api/admin/self/stocks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "batchDelete", goodsId, ids: selected }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(`已删除 ${j.deleted} 条`);
        setSelected([]);
        void load();
        onRefreshProducts();
      } else Message.error(j.message ?? "删除失败");
    } finally {
      setSaving(false);
    }
  }

  const columns = [
    {
      title: "ID",
      dataIndex: "id",
      width: 80,
      render: (v: number) => <span className="font-mono text-[12px]">{v}</span>,
    },
    ...(goodsId === 0
      ? [
          {
            title: "所属商品",
            dataIndex: "goodsName",
            width: 190,
            render: (v: string, row: SelfStock) => (
              <div className="min-w-0 text-[12px]">
                <div className="truncate" title={v}>
                  {v || "-"}
                </div>
                <div className="font-mono text-[11px] text-[#86909c]">#{row.goodsId}</div>
              </div>
            ),
          },
        ]
      : []),
    {
      title: "卡密内容",
      dataIndex: "content",
      ellipsis: true,
      render: (v: string) => <span className="font-mono text-[12px]">{v}</span>,
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (v: string) => (
        <Tag size="small" color={STOCK_TAG[v]?.color}>
          {STOCK_TAG[v]?.label ?? v}
        </Tag>
      ),
    },
    {
      title: "订单号",
      dataIndex: "orderNumber",
      width: 180,
      render: (v: string) =>
        v ? <span className="font-mono text-[12px]">{v}</span> : <span className="text-[#86909c]">-</span>,
    },
    {
      title: "导入时间",
      dataIndex: "createdAt",
      width: 160,
      render: (v: number) => (v ? new Date(v).toLocaleString("zh-CN") : "-"),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 90,
      fixed: "right" as const,
      render: (_: unknown, row: SelfStock) =>
        row.status === "available" || row.status === "voided" ? (
          <Popconfirm title="确认删除该卡密？" onOk={() => removeOne(row)}>
            <Button size="mini" status="danger">
              删除
            </Button>
          </Popconfirm>
        ) : (
          <span className="text-[12px] text-[#86909c]">-</span>
        ),
    },
  ];

  const current = products.find((p) => p.goodsId === goodsId);

  return (
    <div className="flex flex-col gap-3">
      <Space wrap>
        <Select
          value={goodsId}
          onChange={(v) => {
            setGoodsId(Number(v));
            setPage(1);
            setSelected([]);
          }}
          style={{ width: 260 }}
          placeholder="选择自营商品"
        >
          <Select.Option value={0}>全部自营商品</Select.Option>
          {products.map((p) => (
            <Select.Option key={p.goodsId} value={p.goodsId}>
              <span className="text-[12px]">
                <span className="font-mono text-[#86909c]">{p.goodsId}</span> {p.name}
                <span className="ml-2 text-[#00b42a]">可用 {p.stockAvailable}</span>
              </span>
            </Select.Option>
          ))}
        </Select>
        <Input.Search
          allowClear
          value={keyword}
          placeholder="搜索卡密 / 订单号"
          style={{ width: 210 }}
          autoComplete="off"
          onChange={(v) => {
            setKeyword(v);
            setPage(1);
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
          style={{ width: 130 }}
        >
          <Select.Option value="all">全部状态</Select.Option>
          <Select.Option value="available">未售出</Select.Option>
          <Select.Option value="sold">已售出</Select.Option>
          <Select.Option value="reserved">锁定中</Select.Option>
        </Select>
        <Button size="small" type="primary" disabled={products.length === 0} onClick={openImportModal}>
          上传导入卡密
        </Button>
        <Button size="small" loading={loading} onClick={load}>
          刷新
        </Button>
        {selected.length > 0 && (
          <Popconfirm title={`确认删除选中的 ${selected.length} 条卡密？`} onOk={removeBatch}>
            <Button size="small" status="danger" loading={saving}>
              批量删除（{selected.length}）
            </Button>
          </Popconfirm>
        )}
        {current && (
          <span className="text-[12px] text-[#86909c]">
            当前商品可用库存 <b className="text-[#00b42a]">{current.stockAvailable}</b> 条
          </span>
        )}
      </Space>

      <Table
        rowKey="id"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: goodsId === 0 ? 1010 : 820 }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (keys) => setSelected(keys as number[]),
          checkboxProps: (row: SelfStock) => ({
            disabled: !(row.status === "available" || row.status === "voided"),
          }),
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

      <Modal
        title="文件导入自营卡密"
        visible={importOpen}
        onCancel={() => setImportOpen(false)}
        footer={<Button onClick={() => setImportOpen(false)}>关闭</Button>}
        style={{ width: 600, maxWidth: "95vw" }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">导入到目标商品</div>
            <Select
              value={importGoodsId || undefined}
              placeholder="请选择自营商品"
              onChange={(v) => setImportGoodsId(Number(v))}
              style={{ width: "100%" }}
            >
              {products.map((p) => (
                <Select.Option key={p.goodsId} value={p.goodsId}>
                  <span className="text-[12px]">
                    <span className="font-mono text-[#86909c]">{p.goodsId}</span> {p.name}
                    <span className="ml-2 text-[#00b42a]">当前可用 {p.stockAvailable}</span>
                  </span>
                </Select.Option>
              ))}
            </Select>
          </div>
          <SelfStockFileUploader
            goodsId={importGoodsId}
            onUploaded={() => {
              if (goodsId === 0 || goodsId === importGoodsId) {
                void load();
              } else {
                setGoodsId(importGoodsId);
                setPage(1);
              }
              onRefreshProducts();
              setImportOpen(false);
            }}
          />
        </div>
      </Modal>
    </div>
  );
}

export function SelfProductsSection() {
  const [activeTab, setActiveTab] = useState<"products" | "stocks">("products");
  const [products, setProducts] = useState<SelfProduct[]>([]);
  const [productsLoading, setProductsLoading] = useState(false);
  const [externalImportGoodsId, setExternalImportGoodsId] = useState<number | null>(null);

  const loadProducts = useCallback(async () => {
    setProductsLoading(true);
    try {
      const j = await fetch("/api/admin/self/products").then((r) => r.json());
      if (j.ok) setProducts(j.items as SelfProduct[]);
      else Message.error(j.message ?? "加载失败");
    } finally {
      setProductsLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadProducts();
  }, [loadProducts]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-[15px] font-medium text-[#1d2129]">自营商品</div>
          <div className="text-[12px] text-[#86909c]">
            自营商品独立于上游：自行定价、上传卡密文件交由后端流式入库，不参与上游同步。
          </div>
        </div>
      </div>
      <Tabs activeTab={activeTab} onChange={(k) => setActiveTab(k as "products" | "stocks")} type="rounded">
        <Tabs.TabPane key="products" title="商品管理">
          <ProductsTab
            items={products}
            loading={productsLoading}
            onRefresh={loadProducts}
            onOpenImportFor={(gid) => {
              setExternalImportGoodsId(gid);
              setActiveTab("stocks");
            }}
          />
        </Tabs.TabPane>
        <Tabs.TabPane key="stocks" title="卡密管理">
          <StocksTab
            products={products}
            onRefreshProducts={loadProducts}
            externalImportGoodsId={externalImportGoodsId}
            onClearExternalImport={() => setExternalImportGoodsId(null)}
          />
        </Tabs.TabPane>
      </Tabs>
    </div>
  );
}
