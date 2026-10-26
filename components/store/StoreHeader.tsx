"use client";
import { formatPrice } from "@/lib/format";

import { Avatar, Button, Drawer, Dropdown, Input, Menu, Modal, Tag } from "@arco-design/web-react";
import {
  IconEmail,
  IconFile,
  IconGift,
  IconNotification,
  IconSearch,
  IconList,
  IconPoweroff,
  IconSettings,
  IconUser,
} from "@arco-design/web-react/icon";
import { Link } from "react-router-dom";
import { useNavigate } from "react-router-dom";
import type { User } from "@/lib/types";
import type { Announcement } from "@/lib/api-types";
import { useEffect, useState } from "react";
import { invalidateSession } from "@/src/hooks/useSession";

interface Props {
  user: User | null;
  searchQuery?: string;
  onSearch?: (q: string) => void;
  announcements?: Announcement[];
}

export function StoreHeader({ user, searchQuery = "", onSearch, announcements = [] }: Props) {
  const navigate = useNavigate();
  const isAdmin = user?.role === "admin";
  const [noticeOpen, setNoticeOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [kw, setKw] = useState(searchQuery);

  useEffect(() => {
    setKw(searchQuery);
  }, [searchQuery]);

  async function logout() {
    await fetch("/api/auth/logout", { method: "POST" });
    invalidateSession();
    navigate("/");
    window.location.reload();
  }

  function onMenu(key: string) {
    if (key === "account") navigate("/account/overview");
    else if (key === "orders") navigate("/account/orders");
    else if (key === "query") navigate("/orders");
    else if (key === "recharge") navigate("/account/overview?focus=recharge");
    else if (key === "admin") navigate("/admin/overview");
    else if (key === "logout") logout();
  }

  return (
    <header className="sticky top-0 z-20 border-b border-[#e5e6eb] bg-white">
      <div className="mx-auto flex h-14 max-w-[1200px] items-center justify-between gap-3 px-4">
        <div className="flex min-w-0 items-center gap-2">
          <Link to="/" className="flex shrink-0 items-center gap-2 text-[16px] font-semibold">
            <img src="/favicon.svg" alt="HShop" className="h-7 w-7 rounded-md" />
            HShop
          </Link>
        </div>

        <div className="hidden min-w-0 flex-1 justify-center px-4 md:flex">
          <Input.Search
            allowClear
            value={kw}
            placeholder="搜索全部商品名称 / 编号"
            style={{ maxWidth: 420 }}
            onChange={(v) => {
              setKw(v);
              if (!v) onSearch?.("");
            }}
            onSearch={(v) => onSearch?.(v)}
          />
        </div>

        <div className="flex items-center gap-3">
          <span className="md:hidden">
            <Button size="small" icon={<IconSearch />} aria-label="搜索" onClick={() => setSearchOpen(true)} />
          </span>
          <span className="lg:hidden">
            <Button size="small" icon={<IconNotification />} aria-label="公告" onClick={() => setNoticeOpen(true)} />
          </span>
          {/* 微软取件 / 订单查询都是公开页面，未登录也保留入口 */}
          <Link to="/pickup" className="inline-flex">
            <Button size="small" icon={<IconEmail />} aria-label="微软取件">
              <span className="hidden sm:inline">微软取件</span>
            </Button>
          </Link>
          <Link to="/orders" className="inline-flex">
            <Button size="small" icon={<IconFile />} aria-label="订单查询">
              <span className="hidden sm:inline">订单查询</span>
            </Button>
          </Link>
          {user ? (
            <>
              <span className="hidden text-[13px] text-[#4e5969] sm:inline">
                余额 <span className="font-semibold text-[#f53f3f]">¥{formatPrice(user.balance)}</span>
              </span>
              <Dropdown
                trigger="click"
                position="br"
                droplist={
                  <div className="min-w-[170px] rounded bg-white py-1 shadow-lg">
                    <div className="mb-1 border-b border-[#e5e6eb] px-3 py-2 text-[12px] text-[#86909c] sm:hidden">
                      余额 <span className="font-semibold text-[#f53f3f]">¥{formatPrice(user.balance)}</span>
                    </div>
                    <Menu onClickMenuItem={onMenu} style={{ border: "none" }}>
                      <Menu.Item key="account">
                        <span className="inline-flex items-center gap-2">
                          <IconUser /> 个人中心
                        </span>
                      </Menu.Item>
                      <Menu.Item key="orders">
                        <span className="inline-flex items-center gap-2">
                          <IconList /> 我的订单
                        </span>
                      </Menu.Item>
                      <Menu.Item key="query">
                        <span className="inline-flex items-center gap-2">
                          <IconFile /> 订单查询
                        </span>
                      </Menu.Item>
                      <Menu.Item key="recharge">
                        <span className="inline-flex items-center gap-2">
                          <IconGift /> 充值 / 兑换
                        </span>
                      </Menu.Item>
                      {isAdmin && (
                        <Menu.Item key="admin">
                          <span className="inline-flex items-center gap-2">
                            <IconSettings /> 管理后台
                          </span>
                        </Menu.Item>
                      )}
                      <Menu.Item key="logout">
                        <span className="inline-flex items-center gap-2">
                          <IconPoweroff /> 退出登录
                        </span>
                      </Menu.Item>
                    </Menu>
                  </div>
                }
              >
                <Button size="small" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                  <Avatar size={22} style={{ background: "#165dff" }}>
                    {(user.nickname || user.username).slice(0, 1).toUpperCase()}
                  </Avatar>
                  <span className="max-w-[90px] truncate text-[12px]">{user.nickname || user.username}</span>
                </Button>
              </Dropdown>
            </>
          ) : (
            <Link to="/login">
              <Button size="small" type="primary">
                登录 / 注册
              </Button>
            </Link>
          )}
        </div>
      </div>

      <Drawer
        width={320}
        placement="right"
        title="公告"
        visible={noticeOpen}
        onCancel={() => setNoticeOpen(false)}
        footer={null}
        unmountOnExit
      >
        <div className="flex flex-col">
          {announcements.length === 0 ? (
            <div className="text-[12px] text-[#86909c]">暂无公告</div>
          ) : (
            announcements.map((a) => (
              <div key={a.id} className="py-2.5">
                <div className="flex items-baseline gap-2">
                  <span className="shrink-0 text-[11px] text-[#86909c]">
                    {new Date(a.createdAt).toLocaleDateString("zh-CN", { month: "2-digit", day: "2-digit" })}
                  </span>
                  <span className="text-[12px] font-medium text-[#1d2129]">{a.title}</span>
                </div>
                {a.content && (
                  <div className="mt-1 whitespace-pre-wrap text-[12px] leading-relaxed text-[#4e5969]">{a.content}</div>
                )}
              </div>
            ))
          )}
        </div>
      </Drawer>

      <Modal
        title="搜索商品"
        visible={searchOpen}
        onCancel={() => setSearchOpen(false)}
        footer={null}
        style={{ width: 420 }}
        unmountOnExit
      >
        <Input.Search
          allowClear
          autoFocus
          value={kw}
          placeholder="搜索全部商品名称 / 编号"
          onChange={setKw}
          onSearch={(v) => {
            onSearch?.(v);
            setSearchOpen(false);
          }}
        />
      </Modal>
    </header>
  );
}
