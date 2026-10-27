"use client";

import { Button, Empty, Message, Pagination, Spin, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useRef, useState } from "react";

/** 后端 internal/mail.Message 的子集 */
export interface MailAddress {
  emailAddress: { name: string; address: string };
}

export interface MailMessage {
  id: string;
  folder: string;
  subject: string;
  sender: MailAddress;
  from: MailAddress;
  toRecipients: MailAddress[];
  receivedDateTime: string;
  bodyPreview: string;
  contentType?: string;
  body?: { contentType: string; content: string };
}

interface ListResponse {
  ok: boolean;
  message?: string;
  result?: {
    email: string;
    folder: string;
    total: number;
    page: number;
    page_size: number;
    items: MailMessage[];
  };
}

interface DetailResponse {
  ok: boolean;
  message?: string;
  result?: { email: string; folder: string; item: MailMessage };
}

export interface MailboxTarget {
  /** 自定义导入的账号数据：账号----密码----clientid----refreshtoken */
  account?: string;
  /** 订单号 */
  orderNumber?: string;
  /** 指定卡券（多张卡时可精确到某一张） */
  cardId?: string;
  /** 展示用邮箱地址 */
  email?: string;
}

const FOLDERS = [
  { key: "inbox", label: "收件箱" },
  { key: "spam", label: "垃圾邮件" },
];

function fmtTime(v: string): string {
  if (!v) return "";
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return v;
  return d.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function addr(a?: MailAddress): string {
  if (!a) return "";
  const n = a.emailAddress?.name?.trim();
  const e = a.emailAddress?.address?.trim();
  if (n && e) return `${n} <${e}>`;
  return n || e || "";
}

/** 把邮件 HTML 放进沙箱 iframe，避免样式污染与脚本执行 */
function HtmlBody({ html }: { html: string }) {
  const ref = useRef<HTMLIFrameElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const doc = el.contentDocument;
    if (!doc) return;
    doc.open();
    doc.write(
      `<!doctype html><html><head><meta charset="utf-8"><base target="_blank">
       <style>body{margin:0;padding:12px;font:13px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#1d2129;word-break:break-word}
       img{max-width:100%;height:auto}a{color:#165dff}</style></head><body>${html}</body></html>`,
    );
    doc.close();
    const resize = () => {
      try {
        el.style.height = `${Math.max(240, doc.body.scrollHeight + 24)}px`;
      } catch {
        /* ignore */
      }
    };
    resize();
    const t = setTimeout(resize, 300);
    return () => clearTimeout(t);
  }, [html]);

  return (
    <iframe
      ref={ref}
      title="邮件正文"
      sandbox="allow-same-origin allow-popups"
      className="w-full rounded border border-[#e5e6eb]"
      style={{ height: 320 }}
    />
  );
}

/** 邮箱阅读器主体：可放进抽屉，也可直接内嵌在独立取件页里 */
export function MailboxPanel({
  target,
  onEmail,
  className = "",
}: {
  target: MailboxTarget | null;
  onEmail?: (email: string) => void;
  className?: string;
}) {
  const [folder, setFolder] = useState("inbox");
  const [page, setPage] = useState(1);
  const [pageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [items, setItems] = useState<MailMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const [email, setEmail] = useState("");

  const [active, setActive] = useState<MailMessage | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  const payload = useCallback(() => {
    if (!target) return null;
    return {
      account: target.account ?? "",
      orderNumber: target.orderNumber ?? "",
      cardId: target.cardId ?? "",
      folder,
    };
  }, [target, folder]);

  const load = useCallback(async () => {
    const base = payload();
    if (!base) return;
    setLoading(true);
    try {
      const j: ListResponse = await fetch("/api/mailbox/list", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...base, page, pageSize }),
      }).then((r) => r.json());
      if (j.ok && j.result) {
        setItems(j.result.items ?? []);
        setTotal(j.result.total ?? 0);
        const mail = j.result.email || target?.email || "";
        setEmail(mail);
        onEmail?.(mail);
      } else {
        setItems([]);
        setTotal(0);
        Message.error(j.message ?? "读取邮箱失败");
      }
    } catch {
      Message.error("读取邮箱失败");
    } finally {
      setLoading(false);
    }
  }, [payload, page, pageSize, target?.email, onEmail]);

  useEffect(() => {
    setActive(null);
    setPage(1);
    setFolder("inbox");
  }, [target?.account, target?.orderNumber, target?.cardId]);

  useEffect(() => {
    load();
  }, [load]);

  async function openMessage(m: MailMessage) {
    const base = payload();
    if (!base) return;
    setActive(m);
    setDetailLoading(true);
    try {
      const j: DetailResponse = await fetch("/api/mailbox/detail", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...base, messageId: m.id }),
      }).then((r) => r.json());
      if (j.ok && j.result) setActive(j.result.item);
      else Message.error(j.message ?? "读取邮件失败");
    } catch {
      Message.error("读取邮件失败");
    } finally {
      setDetailLoading(false);
    }
  }

  return (
    <div className={`flex flex-col gap-3 ${className}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {email ? (
            <span className="truncate font-mono text-[12px] text-[#86909c]">{email}</span>
          ) : null}
          <Tag size="small">{total} 封</Tag>
        </div>
        <div className="flex items-center gap-1">
          {FOLDERS.map((f) => (
            <Button
              key={f.key}
              size="small"
              type={folder === f.key ? "primary" : "secondary"}
              onClick={() => {
                setFolder(f.key);
                setPage(1);
                setActive(null);
              }}
            >
              {f.label}
            </Button>
          ))}
          <Button size="small" loading={loading} onClick={load}>
            刷新
          </Button>
        </div>
      </div>

      {active ? (
        <div className="flex flex-col gap-2">
          <Button size="small" onClick={() => setActive(null)}>
            ← 返回列表
          </Button>
          <div className="rounded border border-[#e5e6eb] px-3 py-2">
            <div className="text-[14px] font-medium">{active.subject || "(无主题)"}</div>
            <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[12px] text-[#86909c]">
              <span>发件人：{addr(active.sender || active.from)}</span>
              <span>{fmtTime(active.receivedDateTime)}</span>
            </div>
            {active.toRecipients?.length ? (
              <div className="mt-1 text-[12px] text-[#86909c]">
                收件人：{active.toRecipients.map(addr).join("，")}
              </div>
            ) : null}
          </div>
          {detailLoading ? (
            <div className="flex justify-center py-8">
              <Spin />
            </div>
          ) : active.body?.content ? (
            active.body.contentType?.toLowerCase().includes("html") ? (
              <HtmlBody html={active.body.content} />
            ) : (
              <pre className="max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded border border-[#e5e6eb] p-3 text-[13px]">
                {active.body.content}
              </pre>
            )
          ) : (
            <div className="rounded border border-[#e5e6eb] px-3 py-4 text-center text-[12px] text-[#86909c]">
              该邮件没有可显示的正文
            </div>
          )}
        </div>
      ) : (
        <>
          <Spin loading={loading} style={{ width: "100%" }}>
            {items.length === 0 ? (
              <Empty description={loading ? "读取中…" : "暂无邮件"} />
            ) : (
              <div className="flex flex-col divide-y divide-[#f2f3f5] rounded border border-[#e5e6eb]">
                {items.map((m) => (
                  <button
                    key={m.id}
                    type="button"
                    onClick={() => openMessage(m)}
                    className="flex flex-col gap-1 px-3 py-2 text-left hover:bg-[#f7f8fa]"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate text-[13px] font-medium">
                        {m.subject || "(无主题)"}
                      </span>
                      <span className="shrink-0 text-[11px] text-[#86909c]">
                        {fmtTime(m.receivedDateTime)}
                      </span>
                    </div>
                    <div className="flex items-center gap-2">
                      <Tag size="small">{addr(m.sender || m.from) || "未知发件人"}</Tag>
                    </div>
                    <div className="line-clamp-2 text-[12px] text-[#86909c]">{m.bodyPreview}</div>
                  </button>
                ))}
              </div>
            )}
          </Spin>

          {total > pageSize && (
            <div className="flex justify-end">
              <Pagination
                size="small"
                current={page}
                pageSize={pageSize}
                total={total}
                onChange={(p) => setPage(p)}
              />
            </div>
          )}
        </>
      )}
    </div>
  );
}
