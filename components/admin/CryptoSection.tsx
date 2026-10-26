"use client";

import {
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

interface Cfg {
  enabled: boolean;
  chain: string;
  chainLabel: string;
  chainDisplay: string;
  rate: number;
  minUsdt: number;
}
interface Slot {
  id: number;
  subUid: string;
  username: string;
  address: string;
  addressTrx: string;
  addressBsc: string;
  chain: string;
  status: "available" | "reserved" | "disabled";
  currentOrderId: number | null;
  reservedUntil: number | null;
}
interface OrderView {
  id: number;
  uid: number;
  subUid: string;
  address: string;
  chain: string;
  chainLabel: string;
  requestedCny: number;
  requestedUsdt: string;
  usdtAmount: string;
  rate: number;
  creditCny: number;
  status: "waiting" | "paid" | "expired" | "cancelled";
  txid: string | null;
  receivedUsdt: string | null;
  credited: number | null;
  expiresAt: number;
  createdAt: number;
  paidAt: number | null;
}
interface Snapshot {
  config: Cfg;
  configured: boolean;
  chains: { code: string; label: string; display: string }[];
  pool: Slot[];
  orders: OrderView[];
  apiKeyMasked: string;
  hasApiKey: boolean;
  hasApiSecret: boolean;
}

const STATUS_TAG: Record<string, { color: string; text: string }> = {
  waiting: { color: "orange", text: "等待支付" },
  paid: { color: "green", text: "已到账" },
  expired: { color: "gray", text: "已过期" },
  cancelled: { color: "red", text: "已取消" },
};
const POOL_TAG: Record<string, { color: string; text: string }> = {
  available: { color: "green", text: "空闲" },
  reserved: { color: "orange", text: "占用中" },
  disabled: { color: "gray", text: "已停用" },
};

export function CryptoSection() {
  const [data, setData] = useState<Snapshot | null>(null);
  const [busy, setBusy] = useState(false);
  const [form, setForm] = useState({ apiKey: "", apiSecret: "", rate: 7.2, minUsdt: 2, chain: "TRX", enabled: true });
  const [newUid, setNewUid] = useState("");
  const [poolOpen, setPoolOpen] = useState(false);
  const [redeemUrl, setRedeemUrl] = useState("");
  const [savingRedeem, setSavingRedeem] = useState(false);

  const apply = useCallback((j: Snapshot & { refresh?: { errors?: string[] } }) => {
    setData(j);
    setForm((f) => ({
      ...f,
      rate: j.config.rate,
      minUsdt: j.config.minUsdt,
      chain: j.config.chain,
      enabled: j.config.enabled,
    }));
    if (j.refresh?.errors?.length) {
      Message.warning(`地址刷新部分失败：${j.refresh.errors.slice(0, 3).join("；")}`);
    }
  }, []);

  const load = useCallback(async () => {
    const [cj, sj] = await Promise.all([
      fetch("/api/admin/crypto").then((r) => r.json()),
      fetch("/api/admin/settings").then((r) => r.json()),
    ]);
    if (cj.ok) apply(cj as Snapshot);
    else Message.error(cj.message ?? "加载失败");
    if (sj.ok) setRedeemUrl(sj.redeemPurchaseUrl ?? "");
  }, [apply]);

  useEffect(() => {
    load();
  }, [load]);

  async function post(body: unknown, okText: string) {
    setBusy(true);
    try {
      const j = await fetch("/api/admin/crypto", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      if (j.ok) {
        apply(j as Snapshot);
        Message.success(okText);
        setForm((f) => ({ ...f, apiKey: "", apiSecret: "" }));
      } else Message.error(j.message ?? "操作失败");
    } finally {
      setBusy(false);
    }
  }

  async function saveRedeem() {
    setSavingRedeem(true);
    try {
      const j = await fetch("/api/admin/settings", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "saveSite", redeemPurchaseUrl: redeemUrl }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("兑换码购买链接已保存");
        setRedeemUrl(j.redeemPurchaseUrl ?? "");
      } else Message.error(j.message ?? "保存失败");
    } finally {
      setSavingRedeem(false);
    }
  }

  if (!data) return <div className="py-16 text-center text-[13px] text-[#86909c]">加载中…</div>;

  const poolColumns = [
    { title: "ID", dataIndex: "id", width: 60 },
    { title: "子账户 UID", dataIndex: "subUid", width: 130 },
    {
      title: "TRC20 地址",
      dataIndex: "addressTrx",
      render: (_: unknown, r: Slot) => (
        <span className="font-mono text-[11px]">{r.addressTrx || <span className="text-[#c9cdd4]">未获取</span>}</span>
      ),
    },
    {
      title: "BEP20 地址",
      dataIndex: "addressBsc",
      render: (_: unknown, r: Slot) => (
        <span className="font-mono text-[11px]">{r.addressBsc || <span className="text-[#c9cdd4]">未获取</span>}</span>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 100,
      render: (_: unknown, r: Slot) => (
        <Tag size="small" color={POOL_TAG[r.status]?.color}>
          {POOL_TAG[r.status]?.text ?? r.status}
        </Tag>
      ),
    },
    {
      title: "当前订单",
      dataIndex: "currentOrderId",
      width: 100,
      render: (_: unknown, r: Slot) => r.currentOrderId ?? "-",
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 170,
      render: (_: unknown, r: Slot) => (
        <Space>
          <Button
            size="small"
            disabled={busy || r.status === "reserved"}
            onClick={() => post({ action: "togglePool", id: r.id, enabled: r.status === "disabled" }, r.status === "disabled" ? "已启用" : "已停用")}
          >
            {r.status === "disabled" ? "启用" : "停用"}
          </Button>
          <Button
            size="small"
            status="danger"
            disabled={busy || r.status === "reserved"}
            onClick={() => post({ action: "removePool", id: r.id }, "已移除")}
          >
            移除
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-4">
      <Card
        title="Bybit 配置"
        bordered={false}
        bodyStyle={{ padding: 16 }}
        extra={
          <Space>
            <Tag color={data.configured ? "green" : "red"}>{data.configured ? "API 已配置" : "API 未配置"}</Tag>
            <Button size="small" loading={busy} onClick={() => post({ action: "checkOrders" }, "已检查进行中订单")}>
              检查进行中订单
            </Button>
          </Space>
        }
      >
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">启用 USDT 充值</div>
            <Switch size="small" checked={form.enabled} onChange={(c) => setForm({ ...form, enabled: c })} />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">默认链</div>
            <Select size="small" value={form.chain} onChange={(v) => setForm({ ...form, chain: String(v) })} style={{ width: "100%" }}>
              {data.chains.map((c) => (
                <Select.Option key={c.code} value={c.code}>
                  {c.label}
                </Select.Option>
              ))}
            </Select>
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">汇率（CNY / USDT）</div>
            <InputNumber
              size="small"
              value={form.rate}
              min={0.01}
              step={0.01}
              style={{ width: "100%" }}
              onChange={(v) => setForm({ ...form, rate: Number(v) || 0 })}
            />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">最小充值（USDT）</div>
            <InputNumber
              size="small"
              value={form.minUsdt}
              min={0.01}
              step={1}
              style={{ width: "100%" }}
              onChange={(v) => setForm({ ...form, minUsdt: Number(v) || 0 })}
            />
          </div>
          <div className="sm:col-span-2">
            <div className="mb-1 text-[12px] text-[#4e5969]">
              API Key {data.hasApiKey && <span className="text-[#86909c]">当前 {data.apiKeyMasked}</span>}
            </div>
            <Input
              size="small"
              value={form.apiKey}
              onChange={(v) => setForm({ ...form, apiKey: v })}
              placeholder={data.hasApiKey ? "留空则不修改" : "填写 API Key"}
            />
          </div>
          <div className="sm:col-span-2">
            <div className="mb-1 text-[12px] text-[#4e5969]">
              API Secret {data.hasApiSecret && <span className="text-[#86909c]">已配置</span>}
            </div>
            <Input.Password
              size="small"
              value={form.apiSecret}
              onChange={(v) => setForm({ ...form, apiSecret: v })}
              placeholder={data.hasApiSecret ? "留空则不修改" : "填写 API Secret"}
            />
          </div>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button
            size="small"
            type="primary"
            loading={busy}
            onClick={() =>
              post(
                {
                  action: "saveConfig",
                  enabled: form.enabled,
                  chain: form.chain,
                  rate: form.rate,
                  minUsdt: form.minUsdt,
                  apiKey: form.apiKey || undefined,
                  apiSecret: form.apiSecret || undefined,
                },
                "配置已保存，地址池已刷新",
              )
            }
          >
            保存配置
          </Button>
          <span className="text-[11px] text-[#86909c]">建议使用只读 Key。</span>
        </div>
        <div className="mt-3 border-t border-[#e5e6eb] pt-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px]">
              <span className="font-medium text-[#1d2129]">子账号地址池</span>
              <span className="text-[#4e5969]">共 <b>{data.pool.length}</b> 个</span>
              <span className="text-[#00b42a]">空闲 {data.pool.filter((x) => x.status === "available").length}</span>
              <span className="text-[#ff7d00]">占用 {data.pool.filter((x) => x.status === "reserved").length}</span>
              <span className="text-[#86909c]">停用 {data.pool.filter((x) => x.status === "disabled").length}</span>
            </div>
            <Button size="small" type="primary" onClick={() => setPoolOpen(true)}>
              管理子账号
            </Button>
          </div>
          <div className="mt-1 text-[11px] text-[#86909c]">每个子账号对应一条 TRC20 / BEP20 地址。</div>
        </div>
      </Card>

      <Card title="兑换码购买链接" bordered={false} bodyStyle={{ padding: 16 }}>
        <div className="flex flex-wrap items-end gap-2">
          <div className="min-w-[240px] flex-1">
            <div className="mb-1 text-[12px] text-[#4e5969]">购买链接（留空隐藏按钮）</div>
            <Input
              size="small"
              value={redeemUrl}
              onChange={setRedeemUrl}
              placeholder="https://example.com/buy"
            />
          </div>
          <Button size="small" type="primary" loading={savingRedeem} onClick={saveRedeem}>
            保存链接
          </Button>
        </div>
        <div className="mt-1 text-[11px] text-[#86909c]">
          用户会在「个人中心 - 充值 / 兑换」看到「购买兑换码」按钮。
        </div>
      </Card>

      <Modal
        title={`子账户地址池（${data.pool.length}）`}
        visible={poolOpen}
        onCancel={() => setPoolOpen(false)}
        footer={null}
        style={{ width: 960 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <Input
              size="small"
              value={newUid}
              onChange={setNewUid}
              placeholder="子账户 UID"
              style={{ width: 200 }}
            />
            <Button
              size="small"
              type="primary"
              disabled={!newUid.trim() || busy}
              onClick={() => {
                post({ action: "addPool", uid: newUid }, "已添加并刷新地址");
                setNewUid("");
              }}
            >
              添加子账号
            </Button>
            <Button size="small" loading={busy} onClick={() => post({ action: "refreshPool" }, "地址池已刷新")}>
              刷新地址
            </Button>
            <span className="text-[11px] text-[#86909c]">需先在 Bybit 创建子账号，添加后自动获取地址。</span>
          </div>
          <Table size="small" rowKey="id" columns={poolColumns} data={data.pool} pagination={false} scroll={{ x: 800 }} />
        </div>
      </Modal>

      </div>
    </div>
  );
}
