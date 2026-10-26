"use client";

import {
  Alert,
  Button,
  Card,
  Input,
  InputNumber,
  Message,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { formatPrice } from "@/lib/format";

interface PerPayCfg {
  enabled: boolean;
  baseUrl: string;
  clientId: string;
  apiSecretMasked: string;
  hasApiSecret: boolean;
  webhookSecretMasked: string;
  hasWebhookSecret: boolean;
  publicBaseUrl: string;
  minCny: number;
  maxCny: number;
  productName: string;
  notifyEnabled: boolean;
  returnEnabled: boolean;
  configured: boolean;
  webhookReady: boolean;
}

interface PerPayOrderView {
  id: number;
  uid: number;
  username: string;
  merchantOrderNo: string;
  perpayOrderId: string | null;
  amountCny: number;
  amountCents: number;
  payableCents: number | null;
  receivedCents: number | null;
  status: "creating" | "waiting" | "paid" | "expired" | "closed" | "failed" | "disputed";
  paymentBasis: string;
  checkoutUrl: string;
  checkoutStatus: string;
  checkoutExpiresAt: number | null;
  orderVersion: number;
  lastError: string;
  credited: number | null;
  createdAt: number;
  updatedAt: number;
  paidAt: number | null;
  lastCheckedAt: number;
}

interface WebhookRow {
  id: number;
  eventId: string;
  eventType: string;
  merchantOrderNo: string;
  deliveryId: string;
  orderVersion: number;
  disposition: string;
  receivedAt: number;
}

interface Snapshot {
  config: PerPayCfg;
  configured: boolean;
  notifyUrl: string;
  returnUrl: string;
  orders: PerPayOrderView[];
  events: WebhookRow[];
  hasApiKey: boolean;
  hasWebhook: boolean;
  warning?: string;
}

const STATUS_TAG: Record<string, { color: string; text: string }> = {
  creating: { color: "blue", text: "创建中" },
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  closed: { color: "gray", text: "已关闭" },
  failed: { color: "red", text: "创建失败" },
  disputed: { color: "red", text: "支付争议" },
};

function cents(v: number | null | undefined) {
  return v === null || v === undefined ? "-" : `¥${formatPrice(v / 100)}`;
}

function copyText(text: string) {
  navigator.clipboard?.writeText(text);
  Message.success("已复制");
}

export function PerPaySection() {
  const [data, setData] = useState<Snapshot | null>(null);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({
    enabled: false,
    notifyEnabled: false,
    returnEnabled: true,
    baseUrl: "",
    clientId: "",
    publicBaseUrl: "",
    apiSecret: "",
    webhookSecret: "",
    minCny: 10,
    maxCny: 5000,
    productName: "账户余额充值",
  });

  const apply = useCallback((j: Snapshot) => {
    setData(j);
    setForm((f) => ({
      ...f,
      enabled: j.config.enabled,
      notifyEnabled: j.config.notifyEnabled,
      returnEnabled: j.config.returnEnabled,
      baseUrl: j.config.baseUrl,
      clientId: j.config.clientId,
      publicBaseUrl: j.config.publicBaseUrl,
      minCny: j.config.minCny,
      maxCny: j.config.maxCny,
      productName: j.config.productName,
      apiSecret: "",
      webhookSecret: "",
    }));
    if (j.warning) Message.warning(j.warning);
  }, []);

  const load = useCallback(async () => {
    const j = await fetch("/api/admin/perpay").then((r) => r.json());
    if (j.ok) apply(j as Snapshot);
    else Message.error(j.message ?? "加载失败");
  }, [apply]);

  useEffect(() => {
    load();
  }, [load]);

  async function post(body: unknown, okText: string) {
    setBusy(true);
    try {
      const j = await fetch("/api/admin/perpay", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      if (j.ok) {
        apply(j as Snapshot);
        Message.success(okText);
        if (j.status) {
          Message.info(
            `PerPay：${j.status.status}，接单 ${j.status.accepting_orders ? "是" : "否"}，通知 ${
              j.status.notifications_enabled ? "已启用" : "未启用"
            }`,
          );
        }
      } else {
        Message.error(j.message ?? "操作失败");
      }
    } finally {
      setBusy(false);
    }
  }

  if (!data) return <div className="py-16 text-center text-[13px] text-[#86909c]">加载中…</div>;

  const orderColumns = [
    { title: "ID", dataIndex: "id", width: 60 },
    { title: "用户", dataIndex: "username", width: 100, render: (_: unknown, r: PerPayOrderView) => r.username || `UID ${r.uid}` },
    {
      title: "商户订单号",
      dataIndex: "merchantOrderNo",
      width: 210,
      render: (_: unknown, r: PerPayOrderView) => <span className="font-mono text-[11px]">{r.merchantOrderNo}</span>,
    },
    {
      title: "申请 / 应付",
      dataIndex: "amountCny",
      width: 130,
      render: (_: unknown, r: PerPayOrderView) => (
        <div className="text-[12px]">
          <div>¥{formatPrice(r.amountCny)}</div>
          <div className="text-[#86909c]">{cents(r.payableCents)}</div>
        </div>
      ),
    },
    {
      title: "实收 / 入账",
      dataIndex: "credited",
      width: 130,
      render: (_: unknown, r: PerPayOrderView) => (
        <div className="text-[12px]">
          <div>{cents(r.receivedCents)}</div>
          <div className="text-[#00b42a]">{r.credited != null ? `¥${formatPrice(r.credited)}` : "-"}</div>
        </div>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 110,
      render: (_: unknown, r: PerPayOrderView) => (
        <Tag size="small" color={STATUS_TAG[r.status]?.color}>
          {STATUS_TAG[r.status]?.text ?? r.status}
        </Tag>
      ),
    },
    {
      title: "收银台",
      dataIndex: "checkoutUrl",
      width: 100,
      render: (_: unknown, r: PerPayOrderView) =>
        r.checkoutUrl ? (
          <a href={r.checkoutUrl} target="_blank" rel="noreferrer" className="text-[12px] text-[#165dff]">
            打开
          </a>
        ) : (
          <span className="text-[12px] text-[#86909c]">-</span>
        ),
    },
    {
      title: "创建 / 确认",
      dataIndex: "createdAt",
      width: 170,
      render: (_: unknown, r: PerPayOrderView) => (
        <div className="text-[11px] text-[#4e5969]">
          <div>{new Date(r.createdAt).toLocaleString("zh-CN")}</div>
          <div className="text-[#00b42a]">
            {r.paidAt ? new Date(r.paidAt).toLocaleString("zh-CN") : "-"}
          </div>
        </div>
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 210,
      render: (_: unknown, r: PerPayOrderView) => (
        <Space>
          {r.status !== "paid" && r.status !== "disputed" && (
            <Button size="small" disabled={busy} onClick={() => post({ action: "checkOrder", orderId: r.id }, "已查单")}>
              查单
            </Button>
          )}
          {r.status === "failed" && (
            <Button size="small" disabled={busy} onClick={() => post({ action: "retryOrder", orderId: r.id }, "已重试创建")}>
              重试
            </Button>
          )}
          {(r.status === "waiting" || r.status === "creating") && (
            <>
              <Button
                size="small"
                status="warning"
                disabled={busy}
                onClick={() => post({ action: "simulate", orderId: r.id }, "已模拟到账（测试）")}
              >
                模拟
              </Button>
              <Button
                size="small"
                status="danger"
                disabled={busy}
                onClick={() => post({ action: "closeOrder", orderId: r.id }, "已关闭")}
              >
                关闭
              </Button>
            </>
          )}
          {r.lastError && (
            <span className="text-[11px] text-[#f53f3f]" title={r.lastError}>
              有错误
            </span>
          )}
        </Space>
      ),
    },
  ];

  const eventColumns = [
    { title: "ID", dataIndex: "id", width: 60 },
    {
      title: "事件 ID",
      dataIndex: "eventId",
      width: 280,
      render: (_: unknown, r: WebhookRow) => <span className="font-mono text-[11px]">{r.eventId}</span>,
    },
    { title: "类型", dataIndex: "eventType", width: 150 },
    {
      title: "商户订单号",
      dataIndex: "merchantOrderNo",
      width: 200,
      render: (_: unknown, r: WebhookRow) => <span className="font-mono text-[11px]">{r.merchantOrderNo}</span>,
    },
    { title: "版本", dataIndex: "orderVersion", width: 70 },
    { title: "处理结果", dataIndex: "disposition", width: 150 },
    {
      title: "接收时间",
      dataIndex: "receivedAt",
      width: 170,
      render: (_: unknown, r: WebhookRow) => new Date(r.receivedAt).toLocaleString("zh-CN"),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <Alert
        type="info"
        content="支付宝充值通过自托管 PerPay 完成：用户在 PerPay 收银台扫码，确认由查单轮询或签名通知推进。请先在 PerPay 创建业务网站，填写客户端 ID、API 密钥与通知密钥。"
      />

      <Tabs className="recharge-tabs" defaultActiveTab="config" lazyload={false} destroyOnHide={false}>
        <Tabs.TabPane key="config" title="接入配置" lazyload={false} destroyOnHide={false}>
          <div className="flex flex-col gap-4">
            <Card
              title="PerPay 连接"
              bordered={false}
              bodyStyle={{ padding: 16 }}
              extra={
                <Space>
                  <Tag color={data.configured ? "green" : "red"}>{data.configured ? "已配置" : "未配置"}</Tag>
                  <Tag color={data.config.webhookReady ? "green" : "gray"}>
                    {data.config.webhookReady ? "通知已启用" : "通知未启用"}
                  </Tag>
                </Space>
              }
            >
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex items-center gap-3">
                  <div>
                    <div className="text-[12px] text-[#4e5969]">启用支付宝充值</div>
                    <div className="text-[11px] text-[#86909c]">关闭后用户无法创建新订单</div>
                  </div>
                  <Switch checked={form.enabled} onChange={(v) => setForm({ ...form, enabled: v })} />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">商品名称（显示在 PerPay 收银台）</div>
                  <Input
                    value={form.productName}
                    onChange={(v) => setForm({ ...form, productName: v })}
                    placeholder="账户余额充值"
                    maxLength={200}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">PerPay 地址（只填来源）</div>
                  <Input
                    value={form.baseUrl}
                    onChange={(v) => setForm({ ...form, baseUrl: v })}
                    placeholder="https://pay.example.com"
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">客户端 ID</div>
                  <Input
                    value={form.clientId}
                    onChange={(v) => setForm({ ...form, clientId: v })}
                    placeholder="PerPay 业务网站页面显示的客户端 ID"
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">
                    API 密钥 {data.hasApiKey && <span className="text-[#86909c]">当前 {data.config.apiSecretMasked}</span>}
                  </div>
                  <Input.Password
                    value={form.apiSecret}
                    onChange={(v) => setForm({ ...form, apiSecret: v })}
                    placeholder={data.hasApiKey ? "留空则不修改" : "PerPay 生成的 32 字节 base64url 密钥"}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">
                    通知密钥 {data.hasWebhook && <span className="text-[#86909c]">当前 {data.config.webhookSecretMasked}</span>}
                  </div>
                  <Input.Password
                    value={form.webhookSecret}
                    onChange={(v) => setForm({ ...form, webhookSecret: v })}
                    placeholder={data.hasWebhook ? "留空则不修改" : "用于验签 PerPay 回调"}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">本商城公开地址（只填来源）</div>
                  <Input
                    value={form.publicBaseUrl}
                    onChange={(v) => setForm({ ...form, publicBaseUrl: v })}
                    placeholder="https://shop.example.com"
                  />
                  <div className="mt-1 text-[11px] text-[#86909c]">
                    本机调试可留空，系统仍会每 20 秒查单确认；生产通知/返回链接要求 HTTPS。
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <div className="mb-1 text-[12px] text-[#4e5969]">最小充值（CNY）</div>
                    <InputNumber
                      value={form.minCny}
                      min={0.01}
                      step={1}
                      precision={2}
                      style={{ width: "100%" }}
                      onChange={(v) => setForm({ ...form, minCny: Number(v) || 0 })}
                    />
                  </div>
                  <div>
                    <div className="mb-1 text-[12px] text-[#4e5969]">最大充值（CNY）</div>
                    <InputNumber
                      value={form.maxCny}
                      min={0.01}
                      step={100}
                      precision={2}
                      style={{ width: "100%" }}
                      onChange={(v) => setForm({ ...form, maxCny: Number(v) || 0 })}
                    />
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <Switch
                    size="small"
                    checked={form.notifyEnabled}
                    onChange={(v) => setForm({ ...form, notifyEnabled: v })}
                  />
                  <div>
                    <div className="text-[12px] text-[#4e5969]">启用签名回调通知</div>
                    <div className="text-[11px] text-[#86909c]">需在 PerPay 同时开启通知并配置本商城来源</div>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <Switch
                    size="small"
                    checked={form.returnEnabled}
                    onChange={(v) => setForm({ ...form, returnEnabled: v })}
                  />
                  <div>
                    <div className="text-[12px] text-[#4e5969]">附带返回商城链接</div>
                    <div className="text-[11px] text-[#86909c]">需在 PerPay 配置 return_origin</div>
                  </div>
                </div>
              </div>

              <div className="mt-4 flex flex-wrap items-center gap-2">
                <Button
                  size="small"
                  type="primary"
                  loading={busy}
                  onClick={() =>
                    post(
                      {
                        action: "saveConfig",
                        enabled: form.enabled,
                        notifyEnabled: form.notifyEnabled,
                        returnEnabled: form.returnEnabled,
                        baseUrl: form.baseUrl,
                        clientId: form.clientId,
                        publicBaseUrl: form.publicBaseUrl,
                        apiSecret: form.apiSecret || undefined,
                        webhookSecret: form.webhookSecret || undefined,
                        minCny: form.minCny,
                        maxCny: form.maxCny,
                        productName: form.productName,
                      },
                      "PerPay 配置已保存",
                    )
                  }
                >
                  保存配置
                </Button>
                <Button size="small" loading={busy} onClick={() => post({ action: "test" }, "连接成功")}>
                  测试连接
                </Button>
                <span className="text-[11px] text-[#86909c]">
                  测试会调用 PerPay 的签名状态接口，不会创建真实订单。
                </span>
              </div>
            </Card>

            <Card title="回调与返回地址" bordered={false} bodyStyle={{ padding: 16 }}>
              <div className="grid gap-3 sm:grid-cols-2">
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">通知地址 notify_url</div>
                  <div className="flex items-center gap-2">
                    <Input readOnly value={data.notifyUrl || "请先填写本商城公开地址"} />
                    <Button size="small" disabled={!data.notifyUrl} onClick={() => copyText(data.notifyUrl)}>
                      复制
                    </Button>
                  </div>
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">返回地址前缀 return_url</div>
                  <div className="flex items-center gap-2">
                    <Input readOnly value={data.returnUrl || "请先填写本商城公开地址"} />
                    <Button size="small" disabled={!data.returnUrl} onClick={() => copyText(data.returnUrl)}>
                      复制
                    </Button>
                  </div>
                </div>
              </div>
              <div className="mt-3 text-[11px] leading-relaxed text-[#86909c]">
                在 PerPay 后台把通知来源（notification_origin）和返回来源（return_origin）都配置为同一个公开来源，
                例如 https://shop.example.com。未启用通知时，系统仍会每 20 秒查单确认；通知能更快到账。
                回调只验签并返回 ACK，不依赖浏览器跳转。
              </div>
            </Card>

            <Card title="配置步骤" bordered={false} bodyStyle={{ padding: 16 }}>
              <ol className="ml-4 list-decimal space-y-1 text-[12px] leading-relaxed text-[#4e5969]">
                <li>部署 PerPay，完成支付宝应用、经营码、查账与自动确认配置。</li>
                <li>在 PerPay 创建业务网站，复制客户端 ID；在「接入与通知」生成 API 密钥。</li>
                <li>如需回调，生成通知密钥，开启通知并填写本商城公开来源；返回来源同样填写。</li>
                <li>把客户端 ID、API 密钥、通知密钥填到上面并保存，点击「测试连接」。</li>
                <li>启用支付宝充值后，用户可在「个人中心 → 充值 / 兑换」使用支付宝扫码。</li>
              </ol>
            </Card>
          </div>
        </Tabs.TabPane>

        <Tabs.TabPane key="orders" title={`充值订单（${data.orders.length}）`} lazyload={false} destroyOnHide={false}>
          <Card
            title="支付宝充值订单"
            bordered={false}
            extra={
              <Space>
                <Button size="small" loading={busy} onClick={() => post({ action: "checkOrders" }, "已检查等待订单")}>
                  检查等待中订单
                </Button>
                <Button size="small" onClick={load}>
                  刷新
                </Button>
              </Space>
            }
          >
            <Table
              rowKey="id"
              columns={orderColumns}
              data={data.orders}
              pagination={{ pageSize: 10 }}
              scroll={{ x: 1280 }}
            />
          </Card>
        </Tabs.TabPane>

        <Tabs.TabPane key="events" title={`回调记录（${data.events.length}）`} lazyload={false} destroyOnHide={false}>
          <Card title="PerPay 通知记录" bordered={false} extra={<Button size="small" onClick={load}>刷新</Button>}>
            <Table
              rowKey="id"
              columns={eventColumns}
              data={data.events}
              pagination={{ pageSize: 10 }}
              scroll={{ x: 1100 }}
            />
          </Card>
        </Tabs.TabPane>
      </Tabs>
    </div>
  );
}
