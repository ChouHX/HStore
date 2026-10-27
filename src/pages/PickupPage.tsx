import { Avatar, Button, Card, Empty, Input, Message, Spin, Tag } from "@arco-design/web-react";
import { IconArrowLeft, IconEmail, IconRefresh, IconUser } from "@arco-design/web-react/icon";
import { useCallback, useEffect, useState } from "react";
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

type ParseResult = { ok: true; value: string } | { ok: false; message: string };

/**
 * 自定义导入账号数据，格式固定：
 *   账号----密码----clientid----refreshtoken
 * 只接受一行一个账号，避免把多行数据拼成一个错误账号。
 */
function parseAccount(raw: string): ParseResult {
  const value = raw.trim();
  if (!value) return { ok: false, message: "请粘贴账号数据" };
  if (value.includes("\n")) return { ok: false, message: "一次只能导入一个账号，请检查是否有多行" };
  const parts = value.split("----").map((s) => s.trim());
  if (parts.length !== 4) {
    return { ok: false, message: "格式错误，应为 账号----密码----clientid----refreshtoken" };
  }
  if (!parts[0]) return { ok: false, message: "账号不能为空" };
  if (!parts[2]) return { ok: false, message: "clientid 不能为空" };
  if (!parts[3]) return { ok: false, message: "refreshtoken 不能为空" };
  return { ok: true, value: parts.join("----") };
}

function fmtTime(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * 独立取件页（公开，无需登录）。
 * 只支持自定义导入账号数据；登录后额外列出名下所有可取出邮箱。
 */
export function PickupPage() {
  const user = useSession();
  const navigate = useNavigate();

  const [account, setAccount] = useState("");
  const [target, setTarget] = useState<MailboxTarget | null>(null);
  const [email, setEmail] = useState("");

  const [items, setItems] = useState<MailboxItem[]>([]);
  const [loading, setLoading] = useState(false);

  const loggedIn = !!user;

  const loadMine = useCallback(async () => {
    if (!loggedIn) {
      setItems([]);
      return;
    }
    setLoading(true);
    try {
      const j = await fetch("/api/mailbox/cards").then((r) => r.json());
      if (j.ok) setItems(j.items ?? []);
      else Message.error(j.message ?? "加载可取出邮箱失败");
    } catch {
      Message.error("加载可取出邮箱失败");
    } finally {
      setLoading(false);
    }
  }, [loggedIn]);

  useEffect(() => {
    loadMine();
  }, [loadMine]);

  function submit() {
    const parsed = parseAccount(account);
    if (!parsed.ok) {
      Message.warning(parsed.message);
      return;
    }
    setTarget({ account: parsed.value });
    setEmail("");
  }

  function open(item: MailboxItem) {
    setTarget({ orderNumber: item.orderNumber, cardId: item.cardId, email: item.email });
    setEmail(item.email);
  }

  return (
    <div className="min-h-screen bg-[#f7f8fa]">
      <header className="sticky top-0 z-20 border-b border-[#e5e6eb] bg-white">
        <div className="mx-auto flex h-14 max-w-[1000px] items-center justify-between gap-3 px-4">
          <Link to="/" className="flex shrink-0 items-center gap-2 text-[16px] font-semibold">
            <img src="/favicon.svg" alt="HShop" className="h-7 w-7 rounded-md" />
            HShop
            <span className="text-[13px] font-normal text-[#86909c]">微软取件</span>
          </Link>

          <div className="flex items-center gap-2">
            <Button size="small" icon={<IconArrowLeft />} onClick={() => navigate("/")}>
              返回商城
            </Button>
            {user ? (
              <div className="flex items-center gap-2">
                <span className="hidden text-[13px] text-[#4e5969] sm:inline">
                  余额{" "}
                  <span className="font-semibold text-[#f53f3f]">¥{formatPrice(user.balance)}</span>
                </span>
                <Button size="small" type="primary" onClick={() => navigate("/account/overview")}>
                  个人中心
                </Button>
              </div>
            ) : (
              <Button
                size="small"
                type="primary"
                icon={<IconUser />}
                onClick={() => navigate("/login")}
              >
                登录
              </Button>
            )}
          </div>
        </div>
      </header>

      <main className="mx-auto flex max-w-[1000px] flex-col gap-4 px-4 py-5">
        <Card bordered={false}>
          <div className="flex flex-col gap-3">
            <div>
              <div className="text-[15px] font-medium text-[#1d2129]">微软取件</div>
              <div className="mt-1 text-[12px] text-[#86909c]">
                粘贴账号数据直接读取邮箱，无需登录。格式：
                <span className="font-mono text-[#4e5969]">账号----密码----clientid----refreshtoken</span>
                ，一次导入一个账号。
              </div>
            </div>
            <Input.TextArea
              value={account}
              onChange={setAccount}
              autoSize={{ minRows: 3, maxRows: 6 }}
              placeholder="账号----密码----clientid----refreshtoken"
              className="font-mono"
            />
            <div className="flex flex-wrap items-center gap-2">
              <Button type="primary" icon={<IconEmail />} onClick={submit}>
                导入并取件
              </Button>
              <Button
                onClick={() => {
                  setAccount("");
                  setTarget(null);
                  setEmail("");
                }}
              >
                清空
              </Button>
              <span className="text-[11px] text-[#86909c]">
                账号数据只用于本次读取，不会保存到浏览器。
              </span>
            </div>
          </div>
        </Card>

        {target ? (
          <Card
            bordered={false}
            title={<span className="text-[14px]">邮箱 {email}</span>}
            extra={
              <Button
                size="small"
                onClick={() => {
                  setTarget(null);
                  setEmail("");
                }}
              >
                关闭
              </Button>
            }
          >
            <MailboxPanel target={target} onEmail={setEmail} />
          </Card>
        ) : null}

        {loggedIn ? (
          <Card
            bordered={false}
            title={<span className="text-[14px]">我的邮箱</span>}
            extra={
              <Button size="small" icon={<IconRefresh />} loading={loading} onClick={loadMine}>
                刷新
              </Button>
            }
          >
            <Spin loading={loading} style={{ width: "100%" }}>
              {items.length === 0 ? (
                <Empty
                  description={loading ? "加载中…" : "暂无可取出的邮箱，购买邮箱类商品后会自动出现在这里"}
                />
              ) : (
                <div className="flex flex-col divide-y divide-[#f2f3f5] rounded border border-[#e5e6eb]">
                  {items.map((it) => (
                    <div
                      key={`${it.orderNumber}-${it.cardId}`}
                      className="flex flex-col gap-2 px-3 py-3 sm:flex-row sm:items-center sm:justify-between"
                    >
                      <div className="flex min-w-0 flex-col gap-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="truncate font-mono text-[13px] font-medium text-[#1d2129]">
                            {it.email}
                          </span>
                          <Tag size="small" color="arcoblue">
                            {it.goodsName || "邮箱商品"}
                          </Tag>
                        </div>
                        <div className="flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-[#86909c]">
                          <span>订单号 {it.orderNumber}</span>
                          {it.createdAt ? <span>{fmtTime(it.createdAt)}</span> : null}
                        </div>
                      </div>
                      <Button size="small" type="primary" onClick={() => open(it)}>
                        取件
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </Spin>
          </Card>
        ) : (
          <Card bordered={false}>
            <div className="flex flex-col items-center gap-2 py-6 text-center">
              <Avatar size={40} style={{ backgroundColor: "#e8f3ff", color: "#165dff" }}>
                <IconEmail />
              </Avatar>
              <div className="text-[13px] text-[#4e5969]">登录后可一键查看名下所有邮箱</div>
              <Button size="small" type="primary" onClick={() => navigate("/login")}>
                去登录
              </Button>
            </div>
          </Card>
        )}
      </main>
    </div>
  );
}
