"use client";

import {
  Alert,
  Button,
  Card,
  Input,
  InputNumber,
  Message,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { formatPrice } from "@/lib/format";

interface AlipayCfg {
  enabled: boolean;
  env: string;
  appId: string;
  hasPrivateKey: boolean;
  hasPublicKey: boolean;
  collectionCode: string;
  minCny: number;
  maxCny: number;
  productName: string;
  orderTtlSeconds: number;
  amountOffsetMaxCents: number;
  cooldownSeconds: number;
  activeScanSeconds: number;
  normalScanSeconds: number;
  safetyLagSeconds: number;
  lookbackSeconds: number;
  configured: boolean;
  endpoint: string;
}

interface AlipayOrderView {
  id: number;
  uid: number;
  username: string;
  orderNo: string;
  requestedCny: number;
  requestedCents: number;
  payableCents: number;
  paymentStatus: "UNPAID" | "CONFIRMED" | "DISPUTED";
  checkoutStatus: "OPEN" | "EXPIRED" | "CLOSED";
  paymentBasis: "NONE" | "INFERRED" | "MANUAL";
  receivedCents: number | null;
  alipayOrderNo: string;
  credited: number | null;
  status: "waiting" | "paid" | "expired" | "closed" | "disputed";
  checkoutUrl: string;
  expiresAt: number;
  endedAt: number;
  createdAt: number;
  paidAt: number | null;
  lastError: string;
}

interface AlipayLedgerView {
  id: number;
  externalEventId: string;
  occurredAt: number;
  amountCents: number;
  direction: "CREDIT" | "DEBIT";
  alipayOrderNo: string;
  merchantOrderNo: string;
  transMemo: string;
  otherAccount: string;
  state: "UNALLOCATED" | "CANDIDATE" | "ALLOCATED" | "IGNORED";
  orderId: number | null;
  createdAt: number;
}

interface ScanState {
  cursorAt: number;
  lastAttemptAt: number;
  lastSuccessAt: number;
  lastError: string;
  lastTraceId: string;
}

interface Snapshot {
  config: AlipayCfg;
  configured: boolean;
  scanState: ScanState;
  orders: AlipayOrderView[];
  pendingLedger: AlipayLedgerView[];
  recentLedger: AlipayLedgerView[];
  openOrders: number;
  unmatched: number;
  warning?: string;
  test?: { entries: number; total: number; traceId: string; windowStart: string; windowEnd: string };
  scan?: { status: string; entries: number; settled: number; unmatched: number; ambiguous: number; error?: string; traceId?: string };
  expired?: number;
  reconcile?: { settled: number; unmatched: number; ambiguous: number };
}

const STATUS_TAG: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  closed: { color: "gray", text: "已关闭" },
  disputed: { color: "red", text: "支付争议" },
};

const LEDGER_TAG: Record<string, { color: string; text: string }> = {
  UNALLOCATED: { color: "orange", text: "待匹配" },
  CANDIDATE: { color: "red", text: "多候选" },
  ALLOCATED: { color: "green", text: "已关联" },
  IGNORED: { color: "gray", text: "已忽略" },
};

function cents(v: number | null | undefined) {
  return v === null || v === undefined ? "-" : `¥${formatPrice(v / 100)}`;
}

function copyText(text: string) {
  navigator.clipboard?.writeText(text);
  Message.success("已复制");
}

export function AlipaySection() {
  const [data, setData] = useState<Snapshot | null>(null);
  const [busy, setBusy] = useState(false);
  const [ledgerOpen, setLedgerOpen] = useState(false);
  const [matchEntry, setMatchEntry] = useState<AlipayLedgerView | null>(null);
  const [matchOrderID, setMatchOrderID] = useState<number | undefined>();
  const [form, setForm] = useState({
    enabled: false,
    env: "production",
    appId: "",
    privateKey: "",
    publicKey: "",
    collectionCode: "",
    minCny: 1,
    maxCny: 5000,
    productName: "账户余额充值",
    orderTtlSeconds: 300,
    amountOffsetMaxCents: 99,
    cooldownSeconds: 600,
    activeScanSeconds: 8,
    normalScanSeconds: 60,
    safetyLagSeconds: 10,
    lookbackSeconds: 3600,
  });

  const apply = useCallback((j: Snapshot) => {
    setData(j);
    setForm((f) => ({
      ...f,
      enabled: j.config.enabled,
      env: j.config.env,
      appId: j.config.appId,
      collectionCode: j.config.collectionCode,
      minCny: j.config.minCny,
      maxCny: j.config.maxCny,
      productName: j.config.productName,
      orderTtlSeconds: j.config.orderTtlSeconds,
      amountOffsetMaxCents: j.config.amountOffsetMaxCents,
      cooldownSeconds: j.config.cooldownSeconds,
      activeScanSeconds: j.config.activeScanSeconds,
      normalScanSeconds: j.config.normalScanSeconds,
      safetyLagSeconds: j.config.safetyLagSeconds,
      lookbackSeconds: j.config.lookbackSeconds,
      privateKey: "",
      publicKey: "",
    }));
    if (j.warning) Message.warning(j.warning);
    if (j.test) {
      Message.success(`支付宝连接成功：最近 5 分钟 ${j.test.entries} 条流水`);
    }
    if (j.scan) {
      if (j.scan.error) Message.error(`查账失败：${j.scan.error}`);
      else if (j.scan.status === "skipped") Message.info("查账已跳过");
      else
        Message.success(
          `查账完成：新流水 ${j.scan.entries} 条，自动确认 ${j.scan.settled} 笔，待匹配 ${j.scan.unmatched} 笔`,
        );
    }
  }, []);

  const load = useCallback(async () => {
    const j = await fetch("/api/admin/alipay").then((r) => r.json());
    if (j.ok) apply(j as Snapshot);
    else Message.error(j.message ?? "加载失败");
  }, [apply]);

  useEffect(() => {
    load();
  }, [load]);

  async function post(body: unknown, okText: string) {
    setBusy(true);
    try {
      const j = await fetch("/api/admin/alipay", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      if (j.config && j.scanState && j.orders) apply(j as Snapshot);
      if (j.ok) Message.success(okText);
      else Message.error(j.message ?? "操作失败");
      return j;
    } finally {
      setBusy(false);
    }
  }

  if (!data) return <div className="py-16 text-center text-[13px] text-[#86909c]">加载中…</div>;

  const pendingOrders = data.orders.filter((o) => o.paymentStatus === "UNPAID");

  const ledgerColumns = [
    { title: "ID", dataIndex: "id", width: 60 },
    {
      title: "到账时间",
      dataIndex: "occurredAt",
      width: 170,
      render: (_: unknown, r: AlipayLedgerView) => new Date(r.occurredAt).toLocaleString("zh-CN"),
    },
    {
      title: "金额",
      dataIndex: "amountCents",
      width: 100,
      render: (_: unknown, r: AlipayLedgerView) => (
        <span className={r.direction === "CREDIT" ? "text-[#00b42a]" : "text-[#f53f3f]"}>
          {r.direction === "CREDIT" ? "+" : "-"}
          {cents(r.amountCents)}
        </span>
      ),
    },
    {
      title: "支付宝订单号",
      dataIndex: "alipayOrderNo",
      width: 190,
      render: (_: unknown, r: AlipayLedgerView) => (
        <span className="font-mono text-[11px]">{r.alipayOrderNo || "-"}</span>
      ),
    },
    {
      title: "备注 / 对方",
      dataIndex: "transMemo",
      render: (_: unknown, r: AlipayLedgerView) => (
        <div className="text-[11px]">
          <div>{r.transMemo || "-"}</div>
          <div className="text-[#86909c]">{r.otherAccount || "-"}</div>
        </div>
      ),
    },
    {
      title: "状态",
      dataIndex: "state",
      width: 100,
      render: (_: unknown, r: AlipayLedgerView) => (
        <Tag size="small" color={LEDGER_TAG[r.state]?.color}>
          {LEDGER_TAG[r.state]?.text ?? r.state}
        </Tag>
      ),
    },
    {
      title: "关联订单",
      dataIndex: "orderId",
      width: 90,
      render: (_: unknown, r: AlipayLedgerView) => r.orderId ?? "-",
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 110,
      render: (_: unknown, r: AlipayLedgerView) =>
        r.direction === "CREDIT" && r.state !== "ALLOCATED" ? (
          <Button
            size="small"
            type="primary"
            disabled={busy}
            onClick={() => {
              setMatchEntry(r);
              setMatchOrderID(undefined);
            }}
          >
            关联订单
          </Button>
        ) : (
          <span className="text-[11px] text-[#86909c]">-</span>
        ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <Alert
        type="info"
        content="HStore 直接对接支付宝开放平台：用户扫经营码支付唯一应付金额，后端定时查询账务明细并按金额 + 时间窗口自动确认入账。无需部署 PerPay，也无需手机监听软件。"
      />

      <div className="flex flex-col gap-4">
            <Card
              title="支付宝开放平台"
              bordered={false}
              bodyStyle={{ padding: 16 }}
              extra={
                <Space>
                  <Tag color={data.configured ? "green" : "red"}>{data.configured ? "已配置" : "未配置"}</Tag>
                  <Tag color={data.openOrders > 0 ? "orange" : "gray"}>待支付 {data.openOrders}</Tag>
                  <Tag color={data.unmatched > 0 ? "red" : "gray"}>待匹配 {data.unmatched}</Tag>
                  <Button size="small" onClick={() => setLedgerOpen(true)}>
                    查看流水
                  </Button>
                  <Button size="small" loading={busy} onClick={() => post({ action: "expire" }, "已处理过期订单")}>
                    处理过期订单
                  </Button>
                </Space>
              }
            >
              <div className="grid gap-3 lg:grid-cols-3">
                <div className="flex items-center gap-3">
                  <Switch checked={form.enabled} onChange={(v) => setForm({ ...form, enabled: v })} />
                  <div>
                    <div className="text-[12px] text-[#4e5969]">启用支付宝充值</div>
                    <div className="text-[11px] text-[#86909c]">关闭后仅停止新订单，已有订单继续查账</div>
                  </div>
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">环境</div>
                  <Select
                    value={form.env}
                    onChange={(v) => setForm({ ...form, env: String(v) })}
                    style={{ width: "100%" }}
                  >
                    <Select.Option value="production">生产环境 openapi.alipay.com</Select.Option>
                    <Select.Option value="sandbox">沙箱 openapi-sandbox.dl.alipaydev.com</Select.Option>
                  </Select>
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">App ID</div>
                  <Input
                    value={form.appId}
                    onChange={(v) => setForm({ ...form, appId: v })}
                    placeholder="开放平台应用 App ID"
                  />
                </div>
                <div className="lg:col-span-2">
                  <div className="mb-1 text-[12px] text-[#4e5969]">
                    应用私钥 {data.config.hasPrivateKey && <span className="text-[#86909c]">已配置</span>}
                  </div>
                  <Input.TextArea
                    value={form.privateKey}
                    onChange={(v) => setForm({ ...form, privateKey: v })}
                    autoSize={{ minRows: 2, maxRows: 4 }}
                    placeholder={data.config.hasPrivateKey ? "已配置，留空则不修改" : "粘贴应用私钥 PEM 或一行 base64"}
                  />
                </div>
                <div className="lg:col-span-2">
                  <div className="mb-1 text-[12px] text-[#4e5969]">
                    支付宝公钥 {data.config.hasPublicKey && <span className="text-[#86909c]">已配置</span>}
                  </div>
                  <Input.TextArea
                    value={form.publicKey}
                    onChange={(v) => setForm({ ...form, publicKey: v })}
                    autoSize={{ minRows: 2, maxRows: 4 }}
                    placeholder={data.config.hasPublicKey ? "已配置，留空则不修改" : "粘贴支付宝公钥（不是应用公钥）"}
                  />
                </div>
                <div className="lg:col-span-2">
                  <div className="mb-1 text-[12px] text-[#4e5969]">经营码链接</div>
                  <div className="flex gap-2">
                    <Input
                      value={form.collectionCode}
                      onChange={(v) => setForm({ ...form, collectionCode: v })}
                      placeholder="https://qr.alipay.com/..."
                    />
                    <Button size="small" disabled={!form.collectionCode} onClick={() => copyText(form.collectionCode)}>
                      复制
                    </Button>
                  </div>
                  <div className="mt-1 text-[11px] text-[#86909c]">
                    支付宝「收钱码/经营码」的二维码内容，只接受 qr.alipay.com。
                  </div>
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">商品名称</div>
                  <Input
                    value={form.productName}
                    onChange={(v) => setForm({ ...form, productName: v })}
                    maxLength={200}
                  />
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <div className="mb-1 text-[12px] text-[#4e5969]">最小充值</div>
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
                    <div className="mb-1 text-[12px] text-[#4e5969]">最大充值</div>
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
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">收银台有效期（秒）</div>
                  <InputNumber
                    value={form.orderTtlSeconds}
                    min={60}
                    max={1800}
                    step={30}
                    style={{ width: "100%" }}
                    onChange={(v) => setForm({ ...form, orderTtlSeconds: Number(v) || 300 })}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">金额加价上限（分）</div>
                  <InputNumber
                    value={form.amountOffsetMaxCents}
                    min={1}
                    max={99}
                    step={1}
                    style={{ width: "100%" }}
                    onChange={(v) => setForm({ ...form, amountOffsetMaxCents: Number(v) || 99 })}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">金额复用冷却（秒）</div>
                  <InputNumber
                    value={form.cooldownSeconds}
                    min={60}
                    max={3600}
                    step={60}
                    style={{ width: "100%" }}
                    onChange={(v) => setForm({ ...form, cooldownSeconds: Number(v) || 600 })}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">活跃查账间隔（秒）</div>
                  <InputNumber
                    value={form.activeScanSeconds}
                    min={5}
                    max={3600}
                    step={1}
                    style={{ width: "100%" }}
                    onChange={(v) => setForm({ ...form, activeScanSeconds: Number(v) || 8 })}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">常规查账间隔（秒）</div>
                  <InputNumber
                    value={form.normalScanSeconds}
                    min={5}
                    max={3600}
                    step={5}
                    style={{ width: "100%" }}
                    onChange={(v) => setForm({ ...form, normalScanSeconds: Number(v) || 60 })}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">安全延迟（秒）</div>
                  <InputNumber
                    value={form.safetyLagSeconds}
                    min={0}
                    max={300}
                    step={1}
                    style={{ width: "100%" }}
                    onChange={(v) => setForm({ ...form, safetyLagSeconds: Number(v) || 0 })}
                  />
                </div>
                <div>
                  <div className="mb-1 text-[12px] text-[#4e5969]">首次回看（秒）</div>
                  <InputNumber
                    value={form.lookbackSeconds}
                    min={60}
                    max={604800}
                    step={60}
                    style={{ width: "100%" }}
                    onChange={(v) => setForm({ ...form, lookbackSeconds: Number(v) || 3600 })}
                  />
                </div>
              </div>

              <div className="mt-4 flex flex-wrap items-center gap-2">
                <Button
                  type="primary"
                  loading={busy}
                  onClick={() =>
                    post(
                      {
                        action: "saveConfig",
                        enabled: form.enabled,
                        env: form.env,
                        appId: form.appId,
                        privateKey: form.privateKey || undefined,
                        publicKey: form.publicKey || undefined,
                        collectionCode: form.collectionCode,
                        minCny: form.minCny,
                        maxCny: form.maxCny,
                        productName: form.productName,
                        orderTtlSeconds: form.orderTtlSeconds,
                        amountOffsetMaxCents: form.amountOffsetMaxCents,
                        cooldownSeconds: form.cooldownSeconds,
                        activeScanSeconds: form.activeScanSeconds,
                        normalScanSeconds: form.normalScanSeconds,
                        safetyLagSeconds: form.safetyLagSeconds,
                        lookbackSeconds: form.lookbackSeconds,
                      },
                      "配置已保存",
                    )
                  }
                >
                  保存配置
                </Button>
                <Button loading={busy} onClick={() => post({ action: "test" }, "连接成功")}>
                  测试连接
                </Button>
                <Button loading={busy} onClick={() => post({ action: "scan" }, "查账完成")}>
                  立即查账
                </Button>
                <Button loading={busy} onClick={() => post({ action: "reconcilePending" }, "已重新匹配")}>
                  重新匹配待处理流水
                </Button>
                <Button loading={busy} onClick={() => post({ action: "resetScan" }, "已重置查账游标")}>
                  重置查账游标
                </Button>
              </div>
            </Card>

            <Card title="查账状态" bordered={false} bodyStyle={{ padding: 16 }}>
              <div className="grid gap-2 text-[12px] sm:grid-cols-2 lg:grid-cols-4">
                <div>
                  最近成功：
                  <span className="ml-1 text-[#1d2129]">
                    {data.scanState.lastSuccessAt
                      ? new Date(data.scanState.lastSuccessAt).toLocaleString("zh-CN")
                      : "尚未查账"}
                  </span>
                </div>
                <div>
                  最近尝试：
                  <span className="ml-1 text-[#1d2129]">
                    {data.scanState.lastAttemptAt
                      ? new Date(data.scanState.lastAttemptAt).toLocaleString("zh-CN")
                      : "-"}
                  </span>
                </div>
                <div>
                  查账游标：
                  <span className="ml-1 font-mono text-[11px]">
                    {data.scanState.cursorAt ? new Date(data.scanState.cursorAt).toLocaleString("zh-CN") : "-"}
                  </span>
                </div>
                <div>
                  待支付 / 待匹配：
                  <span className="ml-1 text-[#f53f3f]">{data.openOrders}</span> /{" "}
                  <span className="text-[#ff7d00]">{data.unmatched}</span>
                </div>
              </div>
              {data.scanState.lastError && (
                <div className="mt-2 break-all rounded bg-[#ffece8] px-3 py-2 font-mono text-[11px] text-[#f53f3f]">
                  {data.scanState.lastError}
                </div>
              )}
              <div className="mt-2 text-[11px] leading-relaxed text-[#86909c]">
                支付宝应用必须开通「账务明细查询」权限。HStore 每 {data.config.activeScanSeconds} 秒查一次
                （有等待订单时），无等待订单时每 {data.config.normalScanSeconds} 秒查一次；查账安全延迟{" "}
                {data.config.safetyLagSeconds} 秒，避免读到未稳定的流水。
              </div>
            </Card>

            <Card title="接入步骤" bordered={false} bodyStyle={{ padding: 16 }}>
              <ol className="ml-4 list-decimal space-y-1 text-[12px] leading-relaxed text-[#4e5969]">
                <li>在支付宝开放平台创建应用，开通「账务明细查询」权限，并配置应用公钥。</li>
                <li>当前使用公钥模式：这里填 App ID、应用私钥、支付宝公钥；应用公钥由你上传到支付宝，不要填到这里。</li>
                <li>获取你的支付宝经营码/收钱码链接（qr.alipay.com），填入经营码链接。</li>
                <li>保存后点「测试连接」，成功后再启用支付宝充值。</li>
                <li>用户下单会生成唯一应付金额（申请金额 + 1–{data.config.amountOffsetMaxCents} 分），必须按页面金额支付。</li>
              </ol>
            </Card>
      </div>

      <Modal
        title={`支付宝流水（待匹配 ${data.pendingLedger.length} / 全部 ${data.recentLedger.length}）`}
        visible={ledgerOpen}
        onCancel={() => setLedgerOpen(false)}
        footer={
          <Space>
            <Button onClick={load}>刷新</Button>
            <Button
              type="primary"
              disabled={busy}
              onClick={() => post({ action: "reconcilePending" }, "已重新匹配待处理流水")}
            >
              重新匹配待处理流水
            </Button>
            <Button onClick={() => setLedgerOpen(false)}>关闭</Button>
          </Space>
        }
        style={{ width: 1080 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-2 text-[13px] font-medium">待匹配流水</div>
            <Table
              rowKey="id"
              columns={ledgerColumns}
              data={data.pendingLedger}
              pagination={{ pageSize: 5 }}
              scroll={{ x: 1100 }}
              size="small"
            />
          </div>
          <div>
            <div className="mb-2 text-[13px] font-medium">全部流水</div>
            <Table
              rowKey="id"
              columns={ledgerColumns}
              data={data.recentLedger}
              pagination={{ pageSize: 8 }}
              scroll={{ x: 1100 }}
              size="small"
            />
          </div>
        </div>
      </Modal>

      <Modal
        title="手工关联支付宝流水"
        visible={Boolean(matchEntry)}
        onCancel={() => setMatchEntry(null)}
        onOk={async () => {
          if (!matchEntry || !matchOrderID) {
            Message.warning("请选择待支付订单");
            return;
          }
          const j = await post(
            { action: "manualMatch", orderId: matchOrderID, ledgerId: matchEntry.id },
            "已手工关联并确认入账",
          );
          if (j.ok) {
            setMatchEntry(null);
            setMatchOrderID(undefined);
          }
        }}
        confirmLoading={busy}
        unmountOnExit
      >
        {matchEntry && (
          <div className="flex flex-col gap-3 text-[13px]">
            <div>
              流水：<b>{cents(matchEntry.amountCents)}</b>，到账时间{" "}
              {new Date(matchEntry.occurredAt).toLocaleString("zh-CN")}
            </div>
            <div className="text-[11px] text-[#86909c]">
              自动匹配失败时，可人工关联。金额不一致也会按订单申请金额入账，请核对后再提交。
            </div>
            <Select
              value={matchOrderID}
              onChange={(v) => setMatchOrderID(Number(v))}
              placeholder="选择待支付订单"
              showSearch
            >
              {pendingOrders.map((o) => (
                <Select.Option key={o.id} value={o.id}>
                  {o.orderNo} · {cents(o.payableCents)} · {o.username || `UID ${o.uid}`}
                </Select.Option>
              ))}
            </Select>
            {pendingOrders.length === 0 && (
              <div className="text-[12px] text-[#f53f3f]">当前没有待支付订单，无法手工关联。</div>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}
