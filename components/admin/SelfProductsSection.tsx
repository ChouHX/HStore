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

const PAGE_SIZE = 20;

interface SelfProduct {
  goodsId: number;
  name: string;
  description: string;
  imgUrl: string;
  finalPrice: number;
  costPrice: number;
  enabled: number;
  fallbackGoodsId: number;
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

function ProductsTab() {
  const [items, setItems] = useState<SelfProduct[]>([]);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editing, setEditing] = useState<SelfProduct | null>(null);
  const [form] = Form.useForm();

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const j = await fetch("/api/admin/self/products").then((r) => r.json());
      if (j.ok) setItems(j.items as SelfProduct[]);
      else Message.error(j.message ?? "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  function openCreate() {
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({ price: 0.07, costPrice: 0.07, enabled: true, fallbackGoodsId: 0 });
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
      fallbackGoodsId: row.fallbackGoodsId,
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
        price: values.price,
        costPrice: values.costPrice,
        description: values.description ?? "",
        imgUrl: values.imgUrl ?? "",
        enabled: values.enabled !== false,
        fallbackGoodsId: Number(values.fallbackGoodsId) || 0,
      };
      const j = await fetch("/api/admin/self/products", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(editing ? "已保存" : `已创建，商品编号 ${j.goodsId}`);
        setOpen(false);
        load();
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
      load();
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
      load();
    } else Message.error(j.message ?? "删除失败");
  }

  const columns = [
    {
      title: "商品编号",
      dataIndex: "goodsId",
      width: 100,
      render: (v: number) => <span className="font-mono text-[12px]">{v}</span>,
    },
    { title: "商品名称", dataIndex: "name" },
    {
      title: "售价",
      dataIndex: "finalPrice",
      width: 100,
      render: (v: number) => <span className="text-[#f53f3f]">¥{formatPrice(v)}</span>,
    },
    {
      title: "可用库存",
      dataIndex: "stockAvailable",
      width: 100,
      render: (v: number) =>
        v > 0 ? <span>{v}</span> : <span className="text-[#f53f3f]">0（需补货）</span>,
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
      width: 220,
      fixed: "right" as const,
      render: (_: unknown, row: SelfProduct) => (
        <Space size="small">
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
        <Button size="small" onClick={load}>
          刷新
        </Button>
      </Space>

      <Table
        rowKey="goodsId"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 900 }}
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
        style={{ width: 560 }}
        unmountOnExit
      >
        <Form form={form} layout="vertical">
          <Form.Item label="商品名称" field="name" rules={[{ required: true, message: "请填写商品名称" }]}>
            <Input placeholder="例如：Outlook 双令牌绑定辅邮" />
          </Form.Item>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Form.Item label="售价（元）" field="price" rules={[{ required: true, message: "请填写售价" }]}>
              <InputNumber min={0} step={0.01} precision={4} style={{ width: "100%" }} />
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
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Form.Item
              label="兜底商品ID（库存不足时上游补货）"
              field="fallbackGoodsId"
              extra="填 0 表示不兜底，库存不足时提示补货"
            >
              <InputNumber min={0} style={{ width: "100%" }} />
            </Form.Item>
            <Form.Item label="上架" field="enabled" triggerPropName="checked">
              <Switch />
            </Form.Item>
          </div>
        </Form>
      </Modal>
    </div>
  );
}

// ===== 卡密管理 =====

function StocksTab() {
  const [products, setProducts] = useState<SelfProduct[]>([]);
  const [goodsId, setGoodsId] = useState<number>(0);
  const [status, setStatus] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [items, setItems] = useState<SelfStock[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);
  const [importOpen, setImportOpen] = useState(false);
  const [content, setContent] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    fetch("/api/admin/self/products")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) {
          const list = j.items as SelfProduct[];
          setProducts(list);
          if (list.length > 0) setGoodsId((cur) => cur || list[0].goodsId);
        }
      });
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const sp = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
      if (goodsId > 0) sp.set("goodsId", String(goodsId));
      if (status !== "all") sp.set("status", status);
      if (keyword) sp.set("keyword", keyword);
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
    load();
  }, [load]);

  async function doImport() {
    if (!goodsId) {
      Message.warning("请先选择自营商品");
      return;
    }
    if (!content.trim()) {
      Message.warning("请粘贴卡密内容");
      return;
    }
    setSaving(true);
    try {
      const j = await fetch("/api/admin/self/stocks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "import", goodsId, content }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(`导入成功 ${j.added} 条${j.skipped ? `，跳过重复 ${j.skipped} 条` : ""}`);
        setImportOpen(false);
        setContent("");
        load();
      } else Message.error(j.message ?? "导入失败");
    } finally {
      setSaving(false);
    }
  }

  async function removeOne(row: SelfStock) {
    const j = await fetch("/api/admin/self/stocks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "delete", goodsId: row.goodsId, stockId: row.id }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已删除");
      load();
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
        load();
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
    {
      title: "商品",
      dataIndex: "goodsName",
      width: 200,
      render: (_: unknown, r: SelfStock) => (
        <span className="text-[12px]">
          <span className="font-mono text-[#86909c]">{r.goodsId}</span> {r.goodsName}
        </span>
      ),
    },
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
          placeholder="搜索卡密 / 订单号"
          style={{ width: 200 }}
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
        <Button size="small" type="primary" onClick={() => setImportOpen(true)}>
          导入卡密
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
        scroll={{ x: 1000 }}
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
        title="导入自营卡密"
        visible={importOpen}
        onCancel={() => setImportOpen(false)}
        onOk={doImport}
        confirmLoading={saving}
        okText="导入"
        style={{ width: 600 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">导入到商品</div>
            <Select value={goodsId} onChange={(v) => setGoodsId(Number(v))} style={{ width: "100%" }}>
              {products.map((p) => (
                <Select.Option key={p.goodsId} value={p.goodsId}>
                  <span className="text-[12px]">
                    <span className="font-mono text-[#86909c]">{p.goodsId}</span> {p.name}
                  </span>
                </Select.Option>
              ))}
            </Select>
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">
              卡密内容（每行一条，重复内容会自动跳过）
            </div>
            <Input.TextArea
              value={content}
              onChange={setContent}
              placeholder={"账号----密码\n账号----密码"}
              autoSize={{ minRows: 8, maxRows: 16 }}
              style={{ fontFamily: "monospace", fontSize: 12 }}
            />
          </div>
        </div>
      </Modal>
    </div>
  );
}

export function SelfProductsSection() {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-[15px] font-medium text-[#1d2129]">自营商品</div>
          <div className="text-[12px] text-[#86909c]">
            自营商品不参与上游同步，可自行增删改查并导入卡密发货
          </div>
        </div>
      </div>
      <Tabs defaultActiveTab="products" type="rounded">
        <Tabs.TabPane key="products" title="商品管理">
          <ProductsTab />
        </Tabs.TabPane>
        <Tabs.TabPane key="stocks" title="卡密管理">
          <StocksTab />
        </Tabs.TabPane>
      </Tabs>
    </div>
  );
}
