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
  Tooltip,
  Typography,
} from "@arco-design/web-react";
import {
  IconArrowLeft,
  IconCopy,
  IconDelete,
  IconDownload,
  IconEmail,
  IconMenu,
  IconPlus,
  IconQuestionCircle,
  IconSearch,
  IconUser,
} from "@arco-design/web-react/icon";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { MAIL_FOLDERS, MailboxPanel, type MailboxTarget } from "@/components/MailboxPanel";
import { useSession } from "@/src/hooks/useSession";

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
/** 列表每行占用的高度（含分隔线），用于按容器高度反算每页条数 */
const ROW_HEIGHT = 34;
/** 分页条 + 列表上下内边距占用的高度 */
const LIST_CHROME_HEIGHT = 56;
const MIN_PAGE_SIZE = 5;
const MAX_PAGE_SIZE = 40;
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

/** 从「账号----密码----clientid----refreshtoken」中提取邮箱，兼容只存了整行的情况 */
function emailOf(item: PickupAccount): string {
  const raw = (item.account || item.email || "").trim();
  const first = raw.split("----")[0].trim();
  if (first.includes("@")) return first;
  const matched = raw.match(/[^\s@]+@[^\s@]+\.[^\s@]+/);
  return (matched?.[0] ?? item.email ?? "").trim();
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
        const account = item.account ?? "";
        return { ...item, account, email: emailOf({ ...item, account }) };
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

/**
 * 按容器实际可用高度反算每页条数：屏幕高时一页多铺几行，避免列表下半截空着。
 *
 * 用 callback ref 上报高度而不是读 ref.current：抽屉内容是异步挂载的，
 * effect 里读 ref 会拿到 null，量不到高度就只能一直用默认值。
 */
function useListHeight() {
  const [height, setHeight] = useState(0);
  const observerRef = useRef<ResizeObserver | null>(null);

  const attach = useCallback((el: HTMLDivElement | null) => {
    observerRef.current?.disconnect();
    if (!el) return;
    const report = (h: number) => {
      // 隐藏容器（display:none 的桌面侧栏）高度为 0，别把有效值覆盖掉
      if (h > 0) setHeight(h);
    };
    report(el.clientHeight);
    const observer = new ResizeObserver(() => report(el.clientHeight));
    observer.observe(el);
    observerRef.current = observer;
  }, []);

  return { height, attach };
}

function pageSizeOf(height: number) {
  if (height <= 0) return PAGE_SIZE;
  const rows = Math.floor((height - LIST_CHROME_HEIGHT) / ROW_HEIGHT);
  return Math.min(MAX_PAGE_SIZE, Math.max(MIN_PAGE_SIZE, rows));
}

/** 导出为与导入完全相同的「账号----密码----clientid----refreshtoken」文本 */
function buildExportText(accounts: PickupAccount[]) {
  return accounts
    .map((item) => {
      const raw = (item.account || "").trim();
      if (raw.split("----").length === 4) return raw;
      return item.email;
    })
    .join("\n");
}

function AccountList({
  accounts,
  rows,
  selectedId,
  onSelect,
  onRemove,
  page,
  onChangePage,
  pageSize,
  keyword,
  onChangeKeyword,
  onImport,
  onExport,
  total,
  attachList,
}: {
  accounts: PickupAccount[];
  rows: PickupAccount[];
  selectedId: string | null;
  onSelect: (account: PickupAccount) => void;
  onRemove: (account: PickupAccount) => void;
  page: number;
  onChangePage: (page: number) => void;
  pageSize: number;
  keyword: string;
  onChangeKeyword: (value: string) => void;
  onImport: () => void;
  onExport: () => void;
  total: number;
  attachList: (el: HTMLDivElement | null) => void;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 border-b border-[#f2f3f5] p-2">
        <Input
          size="small"
          allowClear
          value={keyword}
          onChange={onChangeKeyword}
          prefix={<IconSearch />}
          placeholder="搜索账号"
        />
        <div className="mt-2 flex items-center gap-1">
          <Button size="mini" type="primary" icon={<IconPlus />} onClick={onImport}>
            导入
          </Button>
          <Button size="mini" icon={<IconDownload />} onClick={onExport} disabled={accounts.length === 0}>
            导出
          </Button>
          <span className="ml-auto text-[11px] text-[#86909c]">{total} 个</span>
        </div>
      </div>
      <div ref={attachList} className="min-h-0 flex-1 overflow-y-auto">
        {accounts.length === 0 ? (
          <Empty
            style={{ marginTop: 48 }}
            description={keyword ? "没有匹配的账号" : "还没有账号，导入后显示在这里"}
          />
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
                <span className="block truncate font-mono text-[12px]">{emailOf(item)}</span>
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
            pageSize={pageSize}
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
 * 左侧账号列表，右侧读取邮箱；账号只来自用户手动批量导入，
 * 不自动读取本机订单 / 已购邮箱，避免用户没导入就看到一堆账号。
 */
export function PickupPage() {
  const user = useSession();
  const navigate = useNavigate();

  const [imported, setImported] = useState<PickupAccount[]>(() => loadImported());

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [target, setTarget] = useState<MailboxTarget | null>(null);
  const [nonce, setNonce] = useState(0);
  const [page, setPage] = useState(1);

  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [folder, setFolder] = useState("inbox");
  const [keyword, setKeyword] = useState("");

  const { height: listHeight, attach: attachList } = useListHeight();
  const pageSize = pageSizeOf(listHeight);

  const allAccounts = useMemo(() => {
    const map = new Map<string, PickupAccount>();
    for (const item of imported) {
      const email = emailOf(item);
      const key = email.toLowerCase();
      if (!key) continue;
      if (!map.has(key)) map.set(key, { ...item, email });
    }
    return Array.from(map.values()).sort((a, b) => b.createdAt - a.createdAt);
  }, [imported]);

  /** 搜索命中：邮箱或整行账号数据（含 clientid / refreshtoken）任一包含关键词 */
  const accounts = useMemo(() => {
    const q = keyword.trim().toLowerCase();
    if (!q) return allAccounts;
    return allAccounts.filter((item) => {
      const haystack = (item.email + "\n" + (item.account || "")).toLowerCase();
      return haystack.includes(q);
    });
  }, [allAccounts, keyword]);

  const totalPages = Math.max(1, Math.ceil(accounts.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const pageRows = accounts.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const selected = accounts.find((a) => a.id === selectedId) ?? null;

  // 搜索结果或每页条数变化后，当前页可能越界，回到第一页
  useEffect(() => {
    setPage(1);
  }, [keyword, pageSize]);

  const existingEmails = useMemo(
    () => new Set(allAccounts.map((a) => a.email.toLowerCase())),
    [allAccounts],
  );
  const parsed = useMemo(() => parseBatch(importText, existingEmails), [importText, existingEmails]);

  function copyEmail(email: string) {
    if (!email) return;
    void navigator.clipboard?.writeText(email);
    Message.success("已复制邮箱");
  }

  function selectAccount(account: PickupAccount) {
    setSelectedId(account.id);
    setTarget(null);
    setFolder("inbox");
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

  /** 导出当前筛选结果，格式与导入一致，可直接再导入回来 */
  function exportAccounts() {
    const list = accounts.length > 0 ? accounts : allAccounts;
    if (list.length === 0) {
      Message.warning("没有可导出的账号");
      return;
    }
    const text = buildExportText(list);
    const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    const stamp = new Date().toISOString().slice(0, 10);
    link.href = url;
    link.download = "pickup-accounts-" + stamp + ".txt";
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
    Message.success("已导出 " + list.length + " 个账号");
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
          <div className="flex items-center gap-1.5">
            <span className="text-[15px] font-semibold">微软取件</span>
            <Tooltip
              position="br"
              content={
                <div className="max-w-[260px] leading-relaxed">
                  选择账号后点击「取件」读取邮箱，无需登录。
                </div>
              }
            >
              <IconQuestionCircle className="cursor-help text-[13px] text-[#86909c]" />
            </Tooltip>
          </div>
          <Space size={8}>
            <Button className="lg:hidden!" size="mini" icon={<IconMenu />} onClick={() => setDrawerOpen(true)}>
              账号列表
            </Button>
          </Space>
        </div>

        <div className="flex min-h-0 flex-1 gap-3">
          <aside className="hidden w-[300px] shrink-0 lg:block">
            <Card
              bordered={false}
              style={{ display: "flex", flexDirection: "column", height: "100%", minHeight: 0 }}
              bodyStyle={{ padding: 0, flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}
            >
              <AccountList
                accounts={accounts}
                rows={pageRows}
                selectedId={selectedId}
                onSelect={selectAccount}
                onRemove={removeImported}
                page={currentPage}
                onChangePage={setPage}
                pageSize={pageSize}
                keyword={keyword}
                onChangeKeyword={setKeyword}
                onImport={() => setImportOpen(true)}
                onExport={exportAccounts}
                total={accounts.length}
                attachList={attachList}
              />
            </Card>
          </aside>

          <section className="min-w-0 flex-1">
            <Card
              bordered={false}
              style={{ display: "flex", flexDirection: "column", height: "100%", minHeight: 0 }}
              bodyStyle={{ padding: 12, flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}
            >
              {/* 邮箱、文件夹、取件都放在这一行；下方只显示取件结果，避免邮箱重复出现两次 */}
              <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-[#f2f3f5] pb-2">
                {selected ? (
                  <span className="flex min-w-0 items-center gap-1">
                    <span className="truncate font-mono text-[13px]">{selected.email}</span>
                    <Button
                      size="mini"
                      type="text"
                      icon={<IconCopy />}
                      aria-label="复制邮箱"
                      onClick={() => copyEmail(selected.email)}
                    />
                  </span>
                ) : (
                  <span className="text-[13px] text-[#86909c]">未选择账号</span>
                )}
                <div className="ml-auto flex items-center gap-1">
                  {MAIL_FOLDERS.map((f) => (
                    <Button
                      key={f.key}
                      size="small"
                      type={folder === f.key ? "primary" : "secondary"}
                      onClick={() => setFolder(f.key)}
                    >
                      {f.label}
                    </Button>
                  ))}
                  <Button size="small" type="primary" icon={<IconEmail />} disabled={!selected} onClick={pickup}>
                    取件
                  </Button>
                </div>
              </div>
              <div className="min-h-0 flex-1 overflow-auto pt-3">
                {target ? (
                  <MailboxPanel key={"pickup-" + nonce} target={target} folder={folder} showToolbar={false} />
                ) : (
                  <div className="flex h-full items-center justify-center">
                    <Empty description={selected ? "选择文件夹后点击「取件」读取邮箱" : "选择账号后点击「取件」"} />
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
          selectedId={selectedId}
          onSelect={(account) => {
            selectAccount(account);
            setDrawerOpen(false);
          }}
          onRemove={removeImported}
          page={currentPage}
          onChangePage={setPage}
          pageSize={pageSize}
          keyword={keyword}
          onChangeKeyword={setKeyword}
          onImport={() => {
            setDrawerOpen(false);
            setImportOpen(true);
          }}
          onExport={exportAccounts}
          total={accounts.length}
          attachList={attachList}
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
