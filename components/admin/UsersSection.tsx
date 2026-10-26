"use client";
import { formatPrice } from "@/lib/format";

import {
  Button,
  Input,
  InputNumber,
  Message,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import type { LedgerEntry, Level, User } from "@/lib/types";
import { LevelsPanel } from "./LevelsPanel";

const PAGE_SIZE = 15;
const LEDGER_SIZE = 8;

interface EditForm {
  nickname: string;
  phone: string;
  email: string;
  balance: number;
  levelId: string;
  locked: boolean;
  status: "active" | "frozen";
}

export function UsersSection() {
  const [items, setItems] = useState<User[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(PAGE_SIZE);
  const [keyword, setKeyword] = useState("");
  const [status, setStatus] = useState("all");
  const [levelFilter, setLevelFilter] = useState("all");
  const [levels, setLevels] = useState<Level[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  const [managed, setManaged] = useState<User | null>(null);
  const [newPwd, setNewPwd] = useState("");
  const [resetting, setResetting] = useState(false);
  const [form, setForm] = useState<EditForm | null>(null);

  const [ledgerOpen, setLedgerOpen] = useState(false);
  const [ledger, setLedger] = useState<LedgerEntry[]>([]);
  const [ledgerTotal, setLedgerTotal] = useState(0);
  const [ledgerPage, setLedgerPage] = useState(1);
  const [levelsOpen, setLevelsOpen] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const sp = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
      if (keyword) sp.set("keyword", keyword);
      if (status !== "all") sp.set("status", status);
      if (levelFilter !== "all") sp.set("levelId", levelFilter);
      const [uj, lj] = await Promise.all([
        fetch(`/api/admin/users?${sp}`).then((r) => r.json()),
        fetch("/api/admin/levels").then((r) => r.json()),
      ]);
      if (uj.ok) {
        setItems(uj.items as User[]);
        setTotal(uj.total as number);
      }
      if (lj.ok) setLevels(lj.levels as Level[]);
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, keyword, status, levelFilter]);

  useEffect(() => {
    load();
  }, [load]);

  const loadLedger = useCallback(async (uid: number, p: number) => {
    const j = await fetch(`/api/admin/users?ledger=1&uid=${uid}&page=${p}&pageSize=${LEDGER_SIZE}`).then(
      (r) => r.json(),
    );
    if (j.ok) {
      setLedger(j.items as LedgerEntry[]);
      setLedgerTotal(j.total as number);
    }
  }, []);

  function openManage(u: User) {
    setManaged(u);
    setNewPwd("");
    setForm({
      nickname: u.nickname,
      phone: u.phone,
      email: u.email,
      balance: u.balance,
      levelId: u.levelId ? String(u.levelId) : "auto",
      locked: u.levelLocked === 1,
      status: u.status,
    });
  }

  async function refreshManaged(uid: number) {
    const j = await fetch(`/api/admin/users?keyword=${uid}`).then((r) => r.json());
    const u = (j.items as User[] | undefined)?.find((x) => x.uid === uid);
    if (u) setManaged(u);
    await load();
  }

  async function post(body: unknown) {
    const j = await fetch("/api/admin/users", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }).then((r) => r.json());
    return j as { ok: boolean; message?: string; user?: User; after?: number };
  }

  async function removeUser(u: User) {
    try {
      const j = await fetch("/api/admin/users", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "delete", uid: u.uid }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(`已删除用户 ${u.username}`);
        if (managed?.uid === u.uid) setManaged(null);
        load();
      } else {
        Message.error(j.message ?? "删除失败");
      }
    } catch {
      Message.error("删除失败");
    }
  }

  async function resetPassword() {
    if (!managed) return;
    if (newPwd.length < 6) {
      Message.warning("新密码至少 6 位");
      return;
    }
    setResetting(true);
    try {
      const j = await fetch("/api/admin/users", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "setPassword", uid: managed.uid, password: newPwd }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(`已重置 ${managed.username} 的密码`);
        setNewPwd("");
      } else {
        Message.error(j.message ?? "重置失败");
      }
    } catch {
      Message.error("重置失败");
    } finally {
      setResetting(false);
    }
  }

  async function save() {
    if (!managed || !form) return;
    const orig = managed;
    const balanceDelta = Number((form.balance - orig.balance).toFixed(4));

    setSaving(true);
    try {
      let changed = false;

      if (form.nickname !== orig.nickname || form.phone !== orig.phone || form.email !== orig.email) {
        const j = await post({ action: "update", uid: orig.uid, nickname: form.nickname, phone: form.phone, email: form.email });
        if (!j.ok) return Message.error(j.message ?? "保存失败");
        changed = true;
      }
      if (form.status !== orig.status) {
        const j = await post({ action: "update", uid: orig.uid, status: form.status });
        if (!j.ok) return Message.error(j.message ?? "保存失败");
        changed = true;
      }
      if (Math.abs(balanceDelta) >= 0.0001) {
        const j = await post({ action: "balance", uid: orig.uid, amount: balanceDelta });
        if (!j.ok) return Message.error(j.message ?? "余额调整失败");
        changed = true;
      }
      const origLevel = orig.levelId ? String(orig.levelId) : "auto";
      if (form.levelId !== origLevel || form.locked !== (orig.levelLocked === 1)) {
        const j = await post({
          action: "level",
          uid: orig.uid,
          levelId: form.levelId === "auto" ? null : Number(form.levelId),
          locked: form.locked,
        });
        if (!j.ok) return Message.error(j.message ?? "等级保存失败");
        changed = true;
      }

      Message.success(changed ? "已保存" : "没有需要保存的修改");
      await refreshManaged(orig.uid);
    } finally {
      setSaving(false);
    }
  }

  async function openLedger() {
    if (!managed) return;
    setLedgerOpen(true);
    setLedgerPage(1);
    await loadLedger(managed.uid, 1);
  }

  const columns = [
    {
      title: "用户",
      dataIndex: "username",
      render: (_: unknown, u: User) => (
        <div className="flex flex-col">
          <span>
            {u.nickname || u.username} <span className="text-[11px] text-[#86909c]">#{u.uid}</span>
          </span>
          <span className="text-[11px] text-[#86909c]">@{u.username}</span>
        </div>
      ),
    },
    {
      title: "联系方式",
      dataIndex: "contact",
      render: (_: unknown, u: User) => (
        <span className="text-[#86909c]">{[u.phone, u.email].filter(Boolean).join(" / ") || "-"}</span>
      ),
    },
    {
      title: "等级",
      dataIndex: "levelName",
      render: (_: unknown, u: User) => (
        <Space size={4}>
          <Tag size="small" color="arcoblue">
            {u.levelName || "无"}
          </Tag>
          {u.levelLocked === 1 && <Tag size="small" color="orange">锁定</Tag>}
        </Space>
      ),
    },
    {
      title: "余额",
      dataIndex: "balance",
      align: "right" as const,
      render: (_: unknown, u: User) => (
        <span className="font-semibold text-[#f53f3f]">¥{formatPrice(u.balance)}</span>
      ),
    },
    {
      title: "累计消费",
      dataIndex: "totalSpent",
      align: "right" as const,
      render: (_: unknown, u: User) => `¥${formatPrice(u.totalSpent)}`,
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 90,
      render: (_: unknown, u: User) => (
        <Tag size="small" color={u.status === "active" ? "green" : "red"}>
          {u.status === "active" ? "正常" : "冻结"}
        </Tag>
      ),
    },
    {
      title: "操作",
      dataIndex: "op",
      width: 150,
      render: (_: unknown, u: User) => (
        <Space>
          <Button size="small" onClick={() => openManage(u)}>
            管理
          </Button>
          <Popconfirm
            title={`确认删除用户 ${u.username}？`}
            content="删除后该账号无法登录，历史订单与流水仍保留。"
            onOk={() => removeUser(u)}
          >
            <Button size="small" status="danger">
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const ledgerColumns = [
    {
      title: "时间",
      dataIndex: "createdAt",
      render: (_: unknown, l: LedgerEntry) => new Date(l.createdAt).toLocaleString("zh-CN"),
    },
    { title: "类型", dataIndex: "type", width: 100 },
    {
      title: "变动",
      dataIndex: "amount",
      align: "right" as const,
      render: (_: unknown, l: LedgerEntry) => (
        <span style={{ color: l.amount >= 0 ? "#00b42a" : "#f53f3f" }}>
          {l.amount >= 0 ? "+" : ""}
          {formatPrice(l.amount)}
        </span>
      ),
    },
    {
      title: "调整前",
      dataIndex: "balanceBefore",
      align: "right" as const,
      render: (_: unknown, l: LedgerEntry) => formatPrice(l.balanceBefore),
    },
    {
      title: "调整后",
      dataIndex: "balanceAfter",
      align: "right" as const,
      render: (_: unknown, l: LedgerEntry) => formatPrice(l.balanceAfter),
    },
    { title: "备注", dataIndex: "remark" },
  ];

  return (
    <div className="flex flex-col gap-3">
      <Space wrap>
        <Input.Search
          allowClear
          placeholder="UID / 手机 / 邮箱 / 昵称"
          style={{ width: 220 }}
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
          <Select.Option value="active">正常</Select.Option>
          <Select.Option value="frozen">已冻结</Select.Option>
        </Select>
        <Button size="small" onClick={() => setLevelsOpen(true)}>
          会员等级
        </Button>
        <Select
          value={levelFilter}
          onChange={(v) => {
            setLevelFilter(String(v));
            setPage(1);
          }}
          style={{ width: 160 }}
        >
          <Select.Option value="all">全部等级</Select.Option>
          {levels.map((l) => (
            <Select.Option key={l.id} value={String(l.id)}>
              {l.name}
            </Select.Option>
          ))}
        </Select>
      </Space>

      <Table
        rowKey="uid"
        loading={loading}
        columns={columns}
        data={items}
        scroll={{ x: 900 }}
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

      {/* 单一编辑表单 */}
      <Modal
        title={managed ? `管理用户 #${managed.uid} · ${managed.username}` : ""}
        visible={managed !== null}
        onCancel={() => setManaged(null)}
        onOk={save}
        okText="保存"
        confirmLoading={saving}
        style={{ width: 560 }}
        unmountOnExit
      >
        {form && managed && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">昵称</div>
              <Input value={form.nickname} onChange={(v) => setForm({ ...form, nickname: v })} />
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">手机号</div>
              <Input value={form.phone} onChange={(v) => setForm({ ...form, phone: v })} />
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">邮箱</div>
              <Input value={form.email} onChange={(v) => setForm({ ...form, email: v })} />
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">
                余额（当前 ¥{formatPrice(managed.balance)}）
              </div>
              <InputNumber
                value={form.balance}
                precision={4}
                step={1}
                style={{ width: "100%" }}
                onChange={(v) => setForm({ ...form, balance: Number(v) || 0 })}
              />
              <div className="mt-1 text-[11px] text-[#86909c]">余额变动自动记录为「管理员调整」</div>
            </div>
            <div>
              <div className="mb-1 text-[12px] text-[#4e5969]">会员等级</div>
              <Select
                value={form.levelId}
                onChange={(v) => setForm({ ...form, levelId: String(v) })}
                style={{ width: "100%" }}
              >
                <Select.Option value="auto">自动判定</Select.Option>
                {levels.map((l) => (
                  <Select.Option key={l.id} value={String(l.id)}>
                    {l.name}（满 {l.threshold}）
                  </Select.Option>
                ))}
              </Select>
            </div>
            <div className="flex items-end gap-4">
              <span className="flex items-center gap-2 text-[12px] text-[#4e5969]">
                锁定等级
                <Switch checked={form.locked} onChange={(c) => setForm({ ...form, locked: c })} />
              </span>
              <span className="flex items-center gap-2 text-[12px] text-[#4e5969]">
                账号状态
                <Switch
                  checked={form.status === "active"}
                  onChange={(c) => setForm({ ...form, status: c ? "active" : "frozen" })}
                />
                <span className="text-[#86909c]">{form.status === "active" ? "正常" : "冻结"}</span>
              </span>
            </div>
            <div className="sm:col-span-2">
              <Button size="small" onClick={openLedger}>
                查看账本明细
              </Button>
            </div>
            <div className="rounded border border-[#e5e6eb] p-3 sm:col-span-2">
              <div className="mb-2 text-[12px] text-[#4e5969]">重置登录密码</div>
              <div className="flex gap-2">
                <Input.Password
                  value={newPwd}
                  onChange={setNewPwd}
                  placeholder="新密码，至少 6 位"
                  autoComplete="new-password"
                />
                <Button type="primary" loading={resetting} onClick={resetPassword}>
                  重置
                </Button>
              </div>
              <div className="mt-1 text-[11px] text-[#86909c]">
                不需要原密码，重置后请自行通知用户；操作会记入审计日志。
              </div>
            </div>
          </div>
        )}
      </Modal>

      {/* 账本明细 */}
      <Modal
        title={managed ? `账本明细 · #${managed.uid}` : "账本明细"}
        visible={ledgerOpen}
        onCancel={() => setLedgerOpen(false)}
        footer={null}
        style={{ width: 760 }}
        unmountOnExit
      >
        <Table
          rowKey="id"
          size="small"
          columns={ledgerColumns}
          data={ledger}
          scroll={{ x: 760 }}
          pagination={{
            current: ledgerPage,
            pageSize: LEDGER_SIZE,
            total: ledgerTotal,
            showTotal: true,
            onChange: (p) => {
              setLedgerPage(p);
              if (managed) loadLedger(managed.uid, p);
            },
          }}
        />
      </Modal>

      <Modal
        title="会员等级"
        visible={levelsOpen}
        onCancel={() => setLevelsOpen(false)}
        footer={null}
        style={{ width: 760 }}
        unmountOnExit
      >
        <LevelsPanel onChanged={load} />
      </Modal>
    </div>
  );
}
