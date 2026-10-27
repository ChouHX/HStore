"use client";

import { Button, Card, Input, Message, Space, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import { SmtpSection } from "@/components/admin/SmtpSection";

interface SettingsData {
  baseUrl: string;
  appId: string;
  appSecretMasked: string;
  hasSecret: boolean;
  hasCookie: boolean;
  cookieAt: number;
  cookiePreview: string;
  redeemPurchaseUrl?: string;
}

export function SettingsSection() {
  const [data, setData] = useState<SettingsData | null>(null);
  const [baseUrl, setBaseUrl] = useState("");
  const [appId, setAppId] = useState("");
  const [appSecret, setAppSecret] = useState("");
  const [redeemUrl, setRedeemUrl] = useState("");
  const [saving, setSaving] = useState(false);
  const [savingSite, setSavingSite] = useState(false);
  const [testing, setTesting] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    const j = await fetch("/api/admin/settings").then((r) => r.json());
    if (j.ok) {
      setData(j as SettingsData);
      setBaseUrl(j.baseUrl);
      setAppId(j.appId);
      setRedeemUrl(j.redeemPurchaseUrl ?? "");
    } else {
      Message.error(j.message ?? "加载失败");
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function save() {
    setSaving(true);
    try {
      const j = await fetch("/api/admin/settings", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ baseUrl, appId, appSecret: appSecret || undefined }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("配置已保存");
        setAppSecret("");
        await load();
      } else Message.error(j.message ?? "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function test() {
    setTesting(true);
    try {
      const j = await fetch("/api/admin/settings/test", { method: "POST" }).then((r) => r.json());
      if (j.ok) Message.success(`连接成功，上游余额 ¥${Number(j.balance).toFixed(2)}`);
      else Message.error(j.message ?? "连接失败");
    } finally {
      setTesting(false);
    }
  }

  async function refreshCookie() {
    setRefreshing(true);
    try {
      const j = await fetch("/api/admin/settings/cookie", { method: "POST" }).then((r) => r.json());
      if (j.ok) {
        Message.success("已自动获取 WAF Cookie");
        await load();
      } else Message.error(j.message ?? "获取失败");
    } finally {
      setRefreshing(false);
    }
  }

  async function clearCookie() {
    const j = await fetch("/api/admin/settings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "clearCookie" }),
    }).then((r) => r.json());
    if (j.ok) {
      Message.success("已清除");
      await load();
    } else Message.error(j.message ?? "清除失败");
  }

  async function saveSite() {
    setSavingSite(true);
    try {
      const j = await fetch("/api/admin/settings", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "saveSite", redeemPurchaseUrl: redeemUrl }),
      }).then((r) => r.json());
      if (j.ok) Message.success("商城设置已保存");
      else Message.error(j.message ?? "保存失败");
    } finally {
      setSavingSite(false);
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Card title="开放平台配置" bordered={false}>
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">API 基础地址</div>
            <Input value={baseUrl} onChange={setBaseUrl} placeholder="https://tianqi996.com" />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">客户编号（X-App-Id）</div>
            <Input value={appId} onChange={setAppId} placeholder="在商户后台「开放 API」页面查看" />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">
              API 密钥（appSecret）
              {data?.hasSecret && (
                <span className="ml-2 text-[#86909c]">当前：{data.appSecretMasked}（留空则不修改）</span>
              )}
            </div>
            <Input.Password
              value={appSecret}
              onChange={setAppSecret}
              placeholder={data?.hasSecret ? "留空则不修改" : "填写对接密钥"}
            />
          </div>
          <Space>
            <Button type="primary" loading={saving} onClick={save}>
              保存配置
            </Button>
            <Button loading={testing} onClick={test}>
              测试连接
            </Button>
          </Space>
          <p className="text-[11px] text-[#86909c]">
            配置保存在数据库中，无需修改任何配置文件；签名请求会自动使用这里的客户编号与密钥。
          </p>
        </div>
      </Card>

      <Card title="WAF 放行 Cookie（自动获取）" bordered={false}>
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2 text-[13px]">
            状态：
            {data?.hasCookie ? <Tag color="green">已获取</Tag> : <Tag color="gray">未获取</Tag>}
            {data?.cookieAt ? (
              <span className="text-[12px] text-[#86909c]">
                更新于 {new Date(data.cookieAt).toLocaleString("zh-CN")}
              </span>
            ) : null}
          </div>
          {data?.cookiePreview && (
            <div className="rounded bg-[#f7f8fa] px-3 py-2 font-mono text-[11px] text-[#4e5969] break-all">
              {data.cookiePreview}
            </div>
          )}
          <Space>
            <Button type="primary" loading={refreshing} onClick={refreshCookie}>
              自动获取 Cookie
            </Button>
            <Button status="danger" disabled={!data?.hasCookie} onClick={clearCookie}>
              清除 Cookie
            </Button>
          </Space>
          <p className="text-[11px] text-[#86909c]">
            当接口返回人机校验页时，系统会自动执行站点脚本获取 Cookie 并重试，通常无需手动操作；
            Cookie 过期后可点击上方按钮或等待下次请求自动刷新。
          </p>
        </div>
      </Card>

      <Card title="商城设置" bordered={false}>
        <div className="flex flex-col gap-3">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">兑换码购买链接</div>
            <Input
              value={redeemUrl}
              onChange={setRedeemUrl}
              placeholder="https://example.com/buy（留空则不显示购买按钮）"
            />
          </div>
          <Space>
            <Button type="primary" loading={savingSite} onClick={saveSite}>
              保存商城设置
            </Button>
          </Space>
          <p className="text-[11px] text-[#86909c]">
            配置后会显示在「充值 / 兑换」页面，引导用户跳转购买兑换码。
          </p>
        </div>
      </Card>

      <div className="lg:col-span-2">
        <SmtpSection />
      </div>
    </div>
  );
}
