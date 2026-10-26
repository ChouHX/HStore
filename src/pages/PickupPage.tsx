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

/**
 * 兑换码形如 XXXX-XXXX-XXXX-XXXX，分隔符可有可无。
 * 判断宽松一点没关系：后端会先按兑换码查，查不到再按订单号查。
 */
function looksLikeCode(v: string): boolean {
  return v.replace(/[^0-9A-Za-z]/g, "").length === 16;
}

/** 把用户输入规整成 XXXX-XXXX-XXXX-XXXX */
function normalizeCode(v: string): string {
  const digits = v.replace(/[^0-9A-Za-z]/g, "").toUpperCase();
  if (digits.length !== 16) return v.trim().toUpperCase();
  return digits.replace(/(.{4})(?=.)/g, "$1-");
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
 * 未登录：凭兑换码取件；登录：额外可用订单号，并列出名下所有可取出邮箱。
 */
export function PickupPage() {
  const user = useSession();
  const navigate = useNavigate();

  const [keyword, setKeyword] = useState("");
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
    const v = keyword.trim();
    if (!v) {
      Message.warning("请输入兑换码或订单号");
      return;
    }
    if (looksLikeCode(v)) {
      setTarget({ code: normalizeCode(v) });
      setEmail("");
      return;
    }
    if (!loggedIn) {
      Message.warning("未登录只能使用兑换码取件，订单号请登录后使用");
      return;
    }
    setTarget({ orderNumber: v });
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
                输入兑换码（XXXX-XXXX-XXXX-XXXX）
                {loggedIn ? "或订单号" : ""}
                ，直接打开对应邮箱读取邮件。兑换码不绑定账号，谁持有谁就能取件，无需登录。
              </div>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row">
              <Input
                allowClear
                size="large"
                value={keyword}
                onChange={setKeyword}
                onPressEnter={submit}
                placeholder={loggedIn ? "兑换码 或 订单号" : "兑换码"}
                style={{ maxWidth: 460 }}
              />
              <Button type="primary" size="large" icon={<IconEmail />} onClick={submit}>
                取件
              </Button>
            </div>
            {!loggedIn ? (
              <div className="text-[12px] text-[#86909c]">
                想按订单号取件或查看名下所有邮箱？{" "}
                <Link to="/login" className="text-[#165dff]">
                  登录
                </Link>
              </div>
            ) : null}
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
                  description={
                    loading ? "加载中…" : "暂无可取出的邮箱，购买邮箱类商品或兑换商品码后会自动出现在这里"
                  }
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
