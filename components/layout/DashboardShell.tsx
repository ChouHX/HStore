"use client";
import { formatPrice } from "@/lib/format";

import { Avatar, Button, Drawer, Dropdown, Layout, Menu } from "@arco-design/web-react";
import {
  IconApps,
  IconBook,
  IconEmail,
  IconNotification,
  IconDashboard,
  IconEdit,
  IconFile,
  IconGift,
  IconHome,
  IconList,
  IconMenu,
  IconPoweroff,
  IconLock,
  IconSafe,
  IconSettings,
  IconStorage,
  IconTag,
  IconThunderbolt,
  IconTrophy,
  IconUser,
  IconUserGroup,
} from "@arco-design/web-react/icon";
import { useLocation, useNavigate } from "react-router-dom";
import { useEffect, useState } from "react";
import { SITE_NAME } from "@/lib/site";
import type { User } from "@/lib/types";
import { invalidateSession } from "@/src/hooks/useSession";

const { Sider, Header, Content } = Layout;

export interface NavItem {
  id: string;
  href: string;
  label: string;
}

interface Props {
  user: User;
  nav: NavItem[];
  children: React.ReactNode;
}

const ICONS: Record<string, React.ReactNode> = {
  overview: <IconDashboard />,
  products: <IconApps />,
  productInfo: <IconEdit />,
  self: <IconThunderbolt />,
  catalog: <IconStorage />,
  announcements: <IconNotification />,
  crypto: <IconSafe />,
  payment: <IconSafe />,
  settings: <IconSettings />,
  pricing: <IconTag />,
  users: <IconUserGroup />,
  redeem: <IconGift />,
  levels: <IconTrophy />,
  logs: <IconFile />,
  recharge: <IconGift />,
  orders: <IconList />,
  pickup: <IconEmail />,
  ledger: <IconBook />,
  security: <IconLock />,
};

export function DashboardShell({ user, nav, children }: Props) {
  const pathname = useLocation().pathname;
  const navigate = useNavigate();
  const active = nav.find((n) => pathname === n.href || pathname.startsWith(n.href + "/")) ?? nav[0];
  const [upstream, setUpstream] = useState<number | null>(null);
  const [mobileOpen, setMobileOpen] = useState(false);
  // 管理员身份只用来决定「能否进入管理后台」；个人中心的展示与普通用户完全一致，
  // 上游余额等内部信息只在管理后台（/admin/*）里出现。
  const canAdmin = user.role === "admin";
  const adminArea = pathname === "/admin" || pathname.startsWith("/admin/");
  const hrefById = Object.fromEntries(nav.map((n) => [n.id, n.href]));

  useEffect(() => {
    if (!adminArea) {
      setUpstream(null);
      return;
    }
    fetch("/api/account")
      .then((r) => r.json())
      .then((j) => {
        if (j.ok) setUpstream(j.balance);
      })
      .catch(() => {});
  }, [adminArea]);

  async function logout() {
    await fetch("/api/auth/logout", { method: "POST" });
    invalidateSession();
    navigate("/login");
  }

  function navMenu(after?: () => void) {
    return (
      <Menu
        mode="vertical"
        selectedKeys={active ? [active.id] : []}
        onClickMenuItem={(key) => {
          // 「返回商城」不属于本区导航，单独处理
          if (key === "shop") {
            navigate("/");
            after?.();
            return;
          }
          const href = hrefById[String(key)];
          if (href) navigate(href);
          after?.();
        }}
        style={{ border: "none" }}
      >
        {nav.map((n) => (
          <Menu.Item key={n.id}>
            <span className="inline-flex items-center gap-2">
              {ICONS[n.id]}
              {n.label}
            </span>
          </Menu.Item>
        ))}
        {/* 个人中心 / 管理后台都不是商城，菜单末尾统一给一个回商城的入口 */}
        <Menu.Item key="shop">
          <span className="inline-flex items-center gap-2">
            <IconHome /> 返回商城
          </span>
        </Menu.Item>
      </Menu>
    );
  }

  return (
    <Layout style={{ minHeight: "100vh" }}>
      {/* 桌面侧边栏 */}
      <Sider className="app-sider" width={208} style={{ background: "#fff" }}>
        <div style={{ padding: "16px 16px 8px", display: "flex", alignItems: "center", gap: 8 }}>
          <img src="/favicon.svg" alt={SITE_NAME} style={{ width: 30, height: 30, borderRadius: 8 }} />
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 15, fontWeight: 600 }}>{SITE_NAME}</div>
            <div style={{ fontSize: 12, color: "#86909c" }}>{adminArea ? "管理后台" : "个人中心"}</div>
          </div>
        </div>
        {navMenu()}
      </Sider>

      {/* 移动端抽屉侧边栏 */}
      <Drawer
        width={240}
        placement="left"
        title={
          <span style={{ fontWeight: 600, display: "inline-flex", alignItems: "center", gap: 8 }}>
            <img src="/favicon.svg" alt="" style={{ width: 24, height: 24, borderRadius: 6 }} />
            {SITE_NAME}
            <span style={{ fontSize: 12, fontWeight: 400, color: "#86909c" }}>
              {adminArea ? "管理后台" : "个人中心"}
            </span>
          </span>
        }
        visible={mobileOpen}
        onCancel={() => setMobileOpen(false)}
        footer={null}
        unmountOnExit
      >
        {navMenu(() => setMobileOpen(false))}
      </Drawer>

      <Layout>
        <Header
          style={{
            height: 56,
            background: "#fff",
            borderBottom: "1px solid #e5e6eb",
            padding: "0 16px",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
          }}
        >
          <div className="flex items-center gap-2">
            <span className="md:hidden">
              <Button
                size="small"
                shape="circle"
                icon={<IconMenu />}
                aria-label="菜单"
                title="菜单"
                onClick={() => setMobileOpen(true)}
              />
            </span>
            <span className="md:hidden">
              <img src="/favicon.svg" alt={SITE_NAME} style={{ width: 24, height: 24, borderRadius: 6 }} />
            </span>
            <div style={{ fontSize: 14, fontWeight: 600 }}>{active?.label}</div>
          </div>

          <div className="flex items-center gap-3">
            <span style={{ fontSize: 13, color: "#4e5969" }}>
              余额 <span style={{ color: "#f53f3f", fontWeight: 600 }}>¥{formatPrice(user.balance)}</span>
            </span>
            {adminArea && (
              <span className="hidden sm:inline" style={{ fontSize: 13, color: "#86909c" }}>
                上游{" "}
                <span style={{ fontWeight: 600, color: "#1d2129" }}>
                  {upstream === null ? "--" : `¥${formatPrice(upstream)}`}
                </span>
              </span>
            )}
            <Dropdown
              trigger="click"
              position="br"
              droplist={
                <Menu
                  onClickMenuItem={(key) => {
                    if (key === "account") navigate("/account/overview");
                    else if (key === "admin") navigate("/admin/overview");
                    else if (key === "shop") navigate("/");
                    else if (key === "logout") logout();
                  }}
                >
                  <Menu.Item key="account">
                    <span className="inline-flex items-center gap-2">
                      <IconUser /> 个人中心
                    </span>
                  </Menu.Item>
                  {canAdmin && !adminArea && (
                    <Menu.Item key="admin">
                      <span className="inline-flex items-center gap-2">
                        <IconDashboard /> 管理后台
                      </span>
                    </Menu.Item>
                  )}
                  <Menu.Item key="shop">
                    <span className="inline-flex items-center gap-2">
                      <IconHome /> 返回商城
                    </span>
                  </Menu.Item>
                  <Menu.Item key="logout">
                    <span className="inline-flex items-center gap-2">
                      <IconPoweroff /> 退出登录
                    </span>
                  </Menu.Item>
                </Menu>
              }
            >
              <Button size="small" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                <Avatar size={22}>{(user.nickname || user.username).slice(0, 1)}</Avatar>
                {user.nickname || user.username}
              </Button>
            </Dropdown>
          </div>
        </Header>

        <Content style={{ padding: 16, overflow: "auto", minWidth: 0 }}>{children}</Content>
      </Layout>
    </Layout>
  );
}
