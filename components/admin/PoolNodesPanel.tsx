"use client";

import {
  Button,
  Card,
  Input,
  Message,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
} from "@arco-design/web-react";
import { IconPlus, IconRefresh } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useState } from "react";

interface PoolNodeView {
  index: number;
  label: string;
  enabled: boolean;
  online: boolean;
  idle: number;
  /** 最近一次探测到的家宽出口延迟（ms）；-1 表示探测失败，0/缺失表示还没测到 */
  latencyMs?: number;
  lastSeen?: string;
  command?: string;
  createdAt: number;
}

interface PoolData {
  listen: string;
  tunnel: string;
  tunnelAddr: string;
  reached: boolean;
  error?: string;
  nodes: PoolNodeView[] | null;
  total: number;
  healthy: number;
}

/**
 * 下游家宽出口节点管理。
 *
 * 每个节点 = 一个家宽设备 + 一个服务器隧道端口（18080+N）。
 * sockspool 在 18080 收口，容器不需要改任何配置；节点掉线时自动切到其他节点。
 */
export function PoolNodesPanel() {
  const [data, setData] = useState<PoolData | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [newLabel, setNewLabel] = useState("");
  const [keysOpen, setKeysOpen] = useState(false);
  const [tunnelInput, setTunnelInput] = useState("");
  const [createdCmd, setCreatedCmd] = useState<{ index: number; command: string } | null>(null);

  const load = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const j = await fetch("/api/admin/pool", { cache: "no-store" }).then((r) => r.json());
      if (j.ok) {
        setData({
          listen: j.listen,
          tunnel: j.tunnel,
          tunnelAddr: j.tunnelAddr,
          reached: j.reached,
          error: j.error,
          nodes: j.nodes ?? [],
          total: j.total,
          healthy: j.healthy,
        });
      } else if (!silent) {
        Message.error(j.message ?? "加载失败");
      }
    } catch {
      if (!silent) Message.error("加载失败");
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    void load(false);
    const tick = async () => {
      await load(true);
      if (!stopped) timer = setTimeout(tick, 30000);
    };
    timer = setTimeout(tick, 30000);
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
    };
  }, [load]);

  async function post(body: Record<string, unknown>) {
    setSaving(true);
    try {
      const j = await fetch("/api/admin/pool", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      }).then((r) => r.json());
      return j;
    } finally {
      setSaving(false);
    }
  }

  async function addNode() {
    const j = await post({ action: "add", label: newLabel.trim() });
    if (j.ok) {
      setAddOpen(false);
      setNewLabel("");
      setCreatedCmd({ index: j.node.index, command: j.node.command });
      Message.success(`已创建节点 ${j.node.index}`);
      await load();
    } else Message.error(j.message ?? "创建失败");
  }

  async function toggleNode(n: PoolNodeView, enabled: boolean) {
    const j = await post({ action: "update", index: n.index, label: n.label, enabled });
    if (j.ok) {
      Message.success(enabled ? "已启用" : "已禁用");
      await load();
    } else Message.error(j.message ?? "操作失败");
  }

  async function removeNode(n: PoolNodeView) {
    const j = await post({ action: "delete", index: n.index });
    if (j.ok) {
      Message.success("已删除");
      await load();
    } else Message.error(j.message ?? "删除失败");
  }

  async function saveTunnel() {
    const j = await post({ action: "saveTunnel", tunnelAddr: tunnelInput.trim() });
    if (j.ok) {
      Message.success("隧道地址已保存");
      setKeysOpen(false);
      await load();
    } else Message.error(j.message ?? "保存失败");
  }

  function copy(text: string, tip: string) {
    void navigator.clipboard?.writeText(text);
    Message.success(tip);
  }

  const nodes = data?.nodes ?? [];

  const columns = [
    {
      title: "节点",
      dataIndex: "index",
      width: 110,
      render: (_: unknown, n: PoolNodeView) => (
        <span className="text-[12px]">节点 {n.index}</span>
      ),
    },
    {
      title: "备注",
      dataIndex: "label",
      width: 150,
      render: (v: string) => <span className="text-[12px]">{v || "—"}</span>,
    },
    {
      title: "状态",
      dataIndex: "online",
      width: 150,
      render: (v: boolean, n: PoolNodeView) => (
        <Space size={4}>
          <Tag size="small" color={v ? "green" : n.enabled ? "red" : "gray"}>
            {v ? "在线" : n.enabled ? "离线" : "已禁用"}
          </Tag>
          {v && n.idle > 0 && (
            <span className="text-[11px] text-[#86909c]">待命 {n.idle}</span>
          )}
        </Space>
      ),
    },
    {
      title: "延迟",
      dataIndex: "latencyMs",
      width: 100,
      render: (_: unknown, n: PoolNodeView) => {
        if (!n.enabled || !n.online) {
          return <span className="text-[11px] text-[#c9cdd4]">—</span>;
        }
        const ms = n.latencyMs ?? 0;
        if (ms < 0) {
          return <span className="text-[11px] text-[#f53f3f]">探测失败</span>;
        }
        if (ms === 0) {
          return <span className="text-[11px] text-[#c9cdd4]">测量中…</span>;
        }
        // 按延迟分档着色：<80ms 绿、<200ms 蓝、其余橙
        const color = ms < 80 ? "#00b42a" : ms < 200 ? "#165dff" : "#d25f00";
        return (
          <span className="font-mono text-[12px]" style={{ color }}>
            {ms} ms
          </span>
        );
      },
    },
    {
      title: "最后活动",
      dataIndex: "lastSeen",
      render: (v?: string) =>
        v ? (
          <span className="text-[11px] text-[#86909c]">
            {new Date(v).toLocaleString("zh-CN")}
          </span>
        ) : (
          <span className="text-[11px] text-[#c9cdd4]">—</span>
        ),
    },
    {
      title: "启用",
      dataIndex: "enabled",
      width: 80,
      render: (v: boolean, n: PoolNodeView) => (
        <Switch size="small" checked={v} onChange={(c) => toggleNode(n, c)} />
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 170,
      render: (_: unknown, n: PoolNodeView) => (
        <Space size="mini">
          <Button
            size="mini"
            disabled={!n.command}
            onClick={() => n.command && copy(n.command, `已复制节点 ${n.index} 接入命令`)}
          >
            复制命令
          </Button>
          <Popconfirm title={`确认删除节点 ${n.index}？`} onOk={() => removeNode(n)}>
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
      <Card
        bordered={false}
        title="家宽出口节点"
        extra={
          <Space wrap>
            <Button size="mini" icon={<IconRefresh />} loading={loading} onClick={() => load(false)}>
              刷新
            </Button>
            <Button size="mini" onClick={() => { setTunnelInput(data?.tunnelAddr || ""); setKeysOpen(true); }}>
              隧道地址
            </Button>
            <Button size="mini" type="primary" icon={<IconPlus />} onClick={() => setAddOpen(true)}>
              添加节点
            </Button>
          </Space>
        }
      >
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
            <Tag color={data?.reached ? (data.healthy > 0 ? "green" : "red") : "red"}>
              {data?.reached ? `${data.healthy} / ${data.total} 在线` : "聚合器未运行"}
            </Tag>
            <span className="text-[#4e5969]">
              容器统一出口 <span className="font-mono">{data?.listen || "0.0.0.0:18080"}</span>
            </span>
            <span className="text-[11px] text-[#86909c]">
              任一节点在线即可用，掉线自动切换
            </span>
          </div>
          {data?.error && <div className="text-[11px] text-[#f53f3f]">{data.error}</div>}

          <Table
            rowKey="index"
            size="small"
            loading={loading}
            columns={columns}
            data={nodes}
            pagination={false}
            scroll={{ x: 900 }}
            noDataElement={
              <div className="py-6 text-center text-[12px] text-[#86909c]">
                还没有节点，点击右上角「添加节点」创建，然后在对应家宽设备上执行一键命令。
              </div>
            }
          />

          <p className="text-[11px] leading-relaxed text-[#86909c]">
            每个节点对应一台家宽设备。添加后复制该节点的命令，在设备上以 root 执行即可自动接入：
            agent 会主动连出到服务器隧道端口，不需要公网 IP、不需要 SSH、也无需任何加白。
            容器统一走 18080，多节点自动故障转移，掉一台不影响业务。
          </p>
        </div>
      </Card>

      <Modal
        title="添加家宽节点"
        visible={addOpen}
        onCancel={() => setAddOpen(false)}
        onOk={addNode}
        confirmLoading={saving}
        okText="创建"
        style={{ width: 460 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-2">
          <div className="text-[12px] text-[#4e5969]">备注名（方便区分，如「成都电信」）</div>
          <Input
            value={newLabel}
            onChange={setNewLabel}
            placeholder="留空则显示为 node-N"
            autoComplete="off"
          />
          <p className="text-[11px] leading-relaxed text-[#86909c]">
            创建后会生成该节点专用 SSH 密钥并分配端口，把命令拿到对应设备执行即可。
          </p>
        </div>
      </Modal>

      <Modal
        title={createdCmd ? `节点 ${createdCmd.index} 一键接入命令` : ""}
        visible={createdCmd !== null}
        onCancel={() => setCreatedCmd(null)}
        footer={
          <Space>
            <Button onClick={() => setCreatedCmd(null)}>关闭</Button>
            <Button
              type="primary"
              onClick={() => createdCmd && copy(createdCmd.command, "已复制接入命令")}
            >
              复制命令
            </Button>
          </Space>
        }
        style={{ width: 620 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-2">
          <div className="text-[12px] text-[#4e5969]">
            在对应家宽设备上以 root 执行（脚本会自动安装 SOCKS5 与常驻隧道）：
          </div>
          <code className="block overflow-x-auto whitespace-nowrap rounded bg-[#f2f3f5] px-3 py-2 font-mono text-[12px] text-[#1d2129]">
            {createdCmd?.command}
          </code>
        </div>
      </Modal>

      <Modal
        title="隧道接入地址"
        visible={keysOpen}
        onCancel={() => setKeysOpen(false)}
        onOk={saveTunnel}
        confirmLoading={saving}
        okText="保存"
        style={{ width: 520 }}
        unmountOnExit
      >
        <div className="flex flex-col gap-2">
          <div className="text-[12px] text-[#4e5969]">
            家宽设备上的 agent 连这个地址接入（host:port）：
          </div>
          <Input
            value={tunnelInput}
            onChange={setTunnelInput}
            placeholder="1.2.3.4:18090"
            autoComplete="off"
          />
          <p className="text-[11px] leading-relaxed text-[#86909c]">
            留空则自动使用 HSTORE_PUBLIC_HOST 的主机名 + 18090。
            修改后需要重新复制节点命令（命令里已内联该地址）。
          </p>
        </div>
      </Modal>
    </div>
  );
}
