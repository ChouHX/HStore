import {
  Alert,
  Button,
  Card,
  Drawer,
  Empty,
  Input,
  List,
  Message,
  Modal,
  Pagination,
  Space,
  Tag,
  Typography,
} from "@arco-design/web-react";
import {
  IconArrowLeft,
  IconDelete,
  IconEmail,
  IconMenu,
  IconPlus,
  IconRefresh,
  IconUser,
} from "@arco-design/web-react/icon";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { MailboxPanel, type MailboxTarget } from "@/components/MailboxPanel";
import { formatPrice } from "@/lib/format";
import { useSession } from "@/src/hooks/useSession";

interface MailboxItem {
  orderNumber: string;
  cardId: string;
  email: string;
  goodsName: string;
  createdAt: number;
  status: number;
}

interface PickupAccount {
  id: string;
  email: string;
  source: "mine" | "import";
  account?: string;
  orderNumber?: string;
  cardId?: string;
  createdAt: number;
}

interface ParsedRow {
  email: string;
  account: string;
}

const PAGE_SIZE = 7;
const STORAGE_KEY = "hstore:pickup:accounts";

/**
 * 批量解析账号数据，每行一个：
 *   账号----密码----clientid----refreshtoken
 * 密码允许为空，账号 / clientid / refreshtoken 必填。
 */
function parseBatch(raw: string, existing: Set<string>) {
  const lines = raw.split(/\r?\n/);
  const rows: ParsedRow[] = [];
  const errors: string[] = [];
  const seen = new Set<string>();
  let duplicates = 0;

  lines.forEach((rawLine, index) => {
    const line = rawLine.trim();
    if (!line) return;
    const parts = line.split("----").map((s) => s.trim());
    if (parts.length !== 4) {
      errors.push("第 " + (index + 1) + " 行：应为 4 个字段，实际 " + parts.length + " 个");
      return;
    }
    const email = parts[0];
    const clientId = parts[2];
    const refresh = parts[3];
    if (!email.includes("@")) {
      errors.push("第 " + (index + 1) + " 行：账号不是邮箱地址");
      return;
    }
    if (!clientId) {
      errors.push("第 " + (index + 1) + " 行：clientid 为空");
      return;
    }
    if (!refresh) {
      errors.push("第 " + (index + 1) + " 行：refreshtoken 为空");
      return;
    }
    const key = email.toLowerCase();
    if (seen.has(key) || existing.has(key)) {
      duplicates += 1;
      return;
    }
    seen.add(key);
    rows.push({ email, account: parts.join("----") });
  });

  return { rows, errors, duplicates };
}

function loadImported(): PickupAccount[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    const list = parsed
      .filter((item): item is PickupAccount => Boolean(item && item.email && item.account))
      .map((item) => {
        // 兼容旧版本：当时把整行「账号----密码----...」存进了 email，这里统一只保留邮箱
        const email = item.account.split("----")[0].trim() || item.email.trim();
        return { ...item, email };
      });
    if (JSON.stringify(list) !== JSON.stringify(parsed)) saveImported(list);
    return list;
  } catch {
    return [];
  }
}

function saveImported(list: PickupAccount[]) {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(list));
  } catch {
    /* 存储不可用时仅保留内存态 */
  }
}

function AccountList({
  accounts,
  rows,
  loading,
  selectedId,
  onSelect,
  onRemove,
  page,
  onChangePage,
}: {
  accounts: PickupAccount[];
  rows: PickupAccount[];
  loading: boolean;
  selectedId: string | null;
  onSelect: (account: PickupAccount) => void;
  onRemove: (account: PickupAccount) => void;
  page: number;
  onChangePage: (page: number) => void;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-y-auto">
        {accounts.length === 0 ? (
          <Empty style={{ marginTop: 72 }} description={loading ? "加载中…" : "还没有账号，导入后显示在这里"} />
        ) : (
          <List
            dataSource={rows}
            split
            render={(item) => (
              <List.Item
                key={item.id}
                style={{
                  cursor: "pointer",
                  padding: "7px 12px",
                  background: item.id === selectedId ? "#e8f3ff" : undefined,
                }}
                onClick={() => onSelect(item)}
                actions={
                  item.source === "import"
                    ? [
                        <Button
                          key="remove"
                          size="mini"
                          type="text"
                          icon={<IconDelete />}
                          onClick={(e) => {
                            e.stopPropagation();
                            onRemove(item);
                          }}
                        />,
                      ]
                    : undefined
                }
              >
                <span className="block truncate font-mono text-[12px]">{item.email}</span>
              </List.Item>
            )}
          />
        )}
      </div>
      {accounts.length > 0 && (
        <div className="flex shrink-0 justify-center border-t border-[#f2f3f5] py-2">
          <Pagination
            size="small"
            total={accounts.length}
            pageSize={PAGE_SIZE}
            current={page}
            onChange={onChangePage}
          />
        </div>
      )}
    </div>
  );
}

/**
 * 独立取件页（公开，无需登录）。
 * 左侧账号列表，右侧读取邮箱；账号来自已购邮箱与手动批量导入。
 */
export function PickupPage() {
  const user = useSession();
  const navigate = useNavigate();

  const [items, setItems] = useState<MailboxItem[]>([]);
  const [imported, setImported] = useState<PickupAccount[]>(() => loadImported());
  const [loadingMine, setLoadingMine] = useState(false);

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [target, setTarget] = useState<MailboxTarget | null>(null);
  const [nonce, setNonce] = useState(0);
  const [page, setPage] = useState(1);

  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);

  const loggedIn = !!user;

  const loadMine = useCallback(async () => {
    if (!loggedIn) {
      setItems([]);
      return;
    }
    setLoadingMine(true);
    try {
      const j = await fetch("/api/mailbox/cards").then((r) => r.json());
      if (j.ok) setItems(j.items ?? []);
      else Message.error(j.message ?? "加载已购邮箱失败");
    } catch {
      Message.error("加载已购邮箱失败");
    } finally {
      setLoadingMine(false);
    }
  }, [loggedIn]);

  useEffect(() => {
    loadMine();
  }, [loadMine]);

  const accounts = useMemo(() => {
    const map = new Map<string, PickupAccount>();
    for (const item of items) {
      const key = (item.email || "").toLowerCase();
      if (!key) continue;
      map.set(key, {
        id: "mine-" + item.orderNumber + "-" + item.cardId,
        email: item.email,
        source: "mine",
        orderNumber: item.orderNumber,
        cardId: item.cardId,
        createdAt: item.createdAt,
      });
    }
    for (const item of imported) {
      const key = item.email.toLowerCase();
      if (!map.has(key)) map.set(key, item);
    }
    return Array.from(map.values()).sort((a, b) => b.createdAt - a.createdAt);
  }, [items, imported]);

  const totalPages = Math.max(1, Math.ceil(accounts.length / PAGE_SIZE));
  const currentPage = Math.min(page, totalPages);
  const pageRows = accounts.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE);
  const selected = accounts.find((a) => a.id === selectedId) ?? null;

  const existingEmails = useMemo(() => new Set(accounts.map((a) => a.email.toLowerCase())), [accounts]);
  const parsed = useMemo(() => parseBatch(importText, existingEmails), [importText, existingEmails]);

  function selectAccount(account: PickupAccount) {
    setSelectedId(account.id);
    setTarget(null);
  }

  function pickup() {
    if (!selected) return;
    setTarget({
      account: selected.account,
      orderNumber: selected.orderNumber,
      cardId: selected.cardId,
      email: selected.email,
    });
    setNonce((n) => n + 1);
  }

  function removeImported(account: PickupAccount) {
    const next = imported.filter((a) => a.id !== account.id);
    setImported(next);
    saveImported(next);
    if (selectedId === account.id) {
      setSelectedId(null);
      setTarget(null);
    }
    Message.success("已移除账号");
  }

  function submitImport() {
    if (parsed.rows.length === 0) return;
    const now = Date.now();
    const added: PickupAccount[] = parsed.rows.map((row, i) => ({
      id: "import-" + row.email.toLowerCase(),
      email: row.email,
      source: "import",
      account: row.account,
      createdAt: now + i,
    }));
    const next = [...imported, ...added];
    setImported(next);
    saveImported(next);
    setImportOpen(false);
    setImportText("");
    setPage(1);
    setSelectedId(added[0].id);
    setTarget(null);
    Message.success("已导入 " + added.length + " 个账号");
  }

  function closeImport() {
    setImportOpen(false);
    setImportText("");
  }

  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-[#f5f6f8]">
      <header className="shrink-0 border-b border-[#e5e6eb] bg-white">
        <div className="mx-auto flex h-12 max-w-[1440px] items-center justify-between gap-3 px-4">
          <Link to="/" className="flex shrink-0 items-center gap-2 text-[15px] font-semibold">
            <img src="/favicon.svg" alt="HShop" className="h-6 w-6" />
            HShop
            <span className="text-[12px] font-normal text-[#86909c]">微软取件</span>
          </Link>

          <Space size={8}>
            <Button size="mini" icon={<IconArrowLeft />} onClick={() => navigate("/")}>
              返回商城
            </Button>
            {user ? (
              <>
                <span className="hidden text-[12px] text-[#4e5969] sm:inline">
                  余额 <span className="font-semibold text-[#f53f3f]">¥{formatPrice(user.balance)}</span>
                </span>
                <Button size="mini" type="primary" onClick={() => navigate("/account/overview")}>
                  个人中心
                </Button>
              </>
            ) : (
              <Button size="mini" type="primary" icon={<IconUser />} onClick={() => navigate("/login")}>
                登录
              </Button>
            )}
          </Space>
        </div>
      </header>

      <div className="mx-auto flex min-h-0 w-full max-w-[1440px] flex-1 flex-col gap-3 px-4 py-3">
        <div className="flex shrink-0 flex-wrap items-center justify-between gap-2">
          <div className="flex items-baseline gap-2">
            <span className="text-[15px] font-semibold">微软取件</span>
            <span className="hidden text-[12px] text-[#86909c] sm:inline">
              选择账号后点击「取件」读取邮箱，无需登录
            </span>
          </div>
          <Space size={8}>
            <Button className="lg:hidden!" size="mini" icon={<IconMenu />} onClick={() => setDrawerOpen(true)}>
              账号列表
            </Button>
            {loggedIn && (
              <Button size="mini" icon={<IconRefresh />} loading={loadingMine} onClick={loadMine}>
                刷新
              </Button>
            )}
            <Button size="mini" type="primary" icon={<IconPlus />} onClick={() => setImportOpen(true)}>
              导入账号
            </Button>
          </Space>
        </div>

        <div className="flex min-h-0 flex-1 gap-3">
          <aside className="hidden w-[300px] shrink-0 lg:block">
            <Card
              bordered={false}
              title="账号列表"
              extra={<Tag size="small">{accounts.length} 个</Tag>}
              style={{ display: "flex", flexDirection: "column", height: "100%", minHeight: 0 }}
              bodyStyle={{ padding: 0, flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}
            >
              <AccountList
                accounts={accounts}
                rows={pageRows}
                loading={loadingMine}
                selectedId={selectedId}
                onSelect={selectAccount}
                onRemove={removeImported}
                page={currentPage}
                onChangePage={setPage}
              />
            </Card>
          </aside>

          <section className="min-w-0 flex-1">
            <Card
              bordered={false}
              title={
                selected ? (
                  <span className="font-mono text-[13px]">{selected.email}</span>
                ) : (
                  <span className="text-[13px] text-[#86909c]">未选择账号</span>
                )
              }
              extra={
                <Button size="mini" type="primary" icon={<IconEmail />} disabled={!selected} onClick={pickup}>
                  取件
                </Button>
              }
              style={{ display: "flex", flexDirection: "column", height: "100%", minHeight: 0 }}
              bodyStyle={{ padding: 12, flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}
            >
              <div className="min-h-0 flex-1 overflow-auto">
                {target ? (
                  <MailboxPanel key={"pickup-" + nonce} target={target} />
                ) : (
                  <div className="flex h-full items-center justify-center">
                    <Empty description={selected ? "点击右上角「取件」读取邮箱" : "选择账号后点击「取件」"} />
                  </div>
                )}
              </div>
            </Card>
          </section>
        </div>
      </div>

      <Drawer
        title="账号列表"
        visible={drawerOpen}
        width={320}
        footer={null}
        onCancel={() => setDrawerOpen(false)}
        bodyStyle={{ padding: 0, display: "flex", flexDirection: "column" }}
      >
        <AccountList
          accounts={accounts}
          rows={pageRows}
          loading={loadingMine}
          selectedId={selectedId}
          onSelect={(account) => {
            selectAccount(account);
            setDrawerOpen(false);
          }}
          onRemove={removeImported}
          page={currentPage}
          onChangePage={setPage}
        />
      </Drawer>

      <Modal
        title="导入账号"
        visible={importOpen}
        onCancel={closeImport}
        style={{ width: 620 }}
        unmountOnExit
        footer={
          <Space>
            <Button onClick={closeImport}>取消</Button>
            <Button type="primary" disabled={parsed.rows.length === 0} onClick={submitImport}>
              导入 {parsed.rows.length} 个账号
            </Button>
          </Space>
        }
      >
        <Space direction="vertical" size={12} style={{ width: "100%" }}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            每行一个账号，格式 <Typography.Text code>账号----密码----clientid----refreshtoken</Typography.Text>
            ，支持一次粘贴多行。
          </Typography.Text>

          <Input.TextArea
            value={importText}
            onChange={setImportText}
            autoSize={{ minRows: 8, maxRows: 14 }}
            placeholder={
              "user@outlook.com----password----client-id----refresh-token\n" +
              "user2@outlook.com----password----client-id----refresh-token"
            }
            style={{ fontFamily: "monospace" }}
          />

          <Space size={12} wrap>
            <Typography.Text type="success" style={{ fontSize: 12 }}>
              已识别 {parsed.rows.length} 个
            </Typography.Text>
            {parsed.duplicates > 0 && (
              <Typography.Text type="warning" style={{ fontSize: 12 }}>
                重复跳过 {parsed.duplicates} 个
              </Typography.Text>
            )}
            {parsed.errors.length > 0 && (
              <Typography.Text type="error" style={{ fontSize: 12 }}>
                格式错误 {parsed.errors.length} 行
              </Typography.Text>
            )}
            <Typography.Text type="secondary" style={{ fontSize: 11 }}>
              仅保存在本机浏览器
            </Typography.Text>
          </Space>

          {parsed.errors.length > 0 && (
            <Alert
              type="error"
              content={
                <div className="font-mono text-[11px] leading-relaxed">
                  {parsed.errors.slice(0, 6).map((err) => (
                    <div key={err}>{err}</div>
                  ))}
                  {parsed.errors.length > 6 && <div>…还有 {parsed.errors.length - 6} 行</div>}
                </div>
              }
            />
          )}
        </Space>
      </Modal>
    </div>
  );
}
