import { Button, Card, Input, InputNumber, Message, Select, Space, Switch, Tag } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";

interface SmtpData {
  host: string;
  port: number;
  security: string;
  username: string;
  fromName: string;
  enabled: boolean;
  hasPassword: boolean;
  passwordMasked: string;
  todaySent: number;
  dailyLimit: number;
}

export function SmtpSection() {
  const [data, setData] = useState<SmtpData | null>(null);
  const [host, setHost] = useState("smtp.gmail.com");
  const [port, setPort] = useState(465);
  const [security, setSecurity] = useState("ssl");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [fromName, setFromName] = useState("HShop");
  const [enabled, setEnabled] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    const j = await fetch("/api/admin/smtp").then((r) => r.json());
    if (j.ok) {
      setData(j as SmtpData);
      setHost(j.host);
      setPort(j.port);
      setSecurity(j.security);
      setUsername(j.username);
      setFromName(j.fromName);
      setEnabled(j.enabled);
    } else {
      Message.error(j.message ?? "加载 SMTP 配置失败");
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  async function save() {
    setSaving(true);
    try {
      const j = await fetch("/api/admin/smtp", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ host, port, security, username, fromName, enabled, password: password || undefined }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("SMTP 配置已保存");
        setPassword("");
        await load();
      } else {
        Message.error(j.message ?? "保存失败");
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card
      title="注册验证码邮件（SMTP）"
      bordered={false}
      extra={
        <Space size={8}>
          {data && (
            <Tag size="small" color={data.todaySent >= data.dailyLimit ? "red" : "gray"}>
              今日已发 {data.todaySent} / {data.dailyLimit}
            </Tag>
          )}
          <Switch checked={enabled} onChange={setEnabled} size="small" />
        </Space>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <div className="mb-1 text-[12px] text-[#4e5969]">SMTP 服务器</div>
          <Input value={host} onChange={setHost} placeholder="smtp.gmail.com" />
        </div>
        <div>
          <div className="mb-1 text-[12px] text-[#4e5969]">端口</div>
          <InputNumber value={port} onChange={(v) => setPort(Number(v) || 465)} min={1} max={65535} style={{ width: "100%" }} />
        </div>
        <div>
          <div className="mb-1 text-[12px] text-[#4e5969]">加密方式</div>
          <Select value={security} onChange={(v) => setSecurity(String(v))} style={{ width: "100%" }}>
            <Select.Option value="ssl">SSL（465）</Select.Option>
            <Select.Option value="starttls">TLS / STARTTLS（587）</Select.Option>
          </Select>
        </div>
        <div>
          <div className="mb-1 text-[12px] text-[#4e5969]">发件人名称</div>
          <Input value={fromName} onChange={setFromName} placeholder="HShop" maxLength={32} />
        </div>
        <div>
          <div className="mb-1 text-[12px] text-[#4e5969]">邮箱账号（需身份验证）</div>
          <Input value={username} onChange={setUsername} placeholder="yourname@gmail.com" />
        </div>
        <div>
          <div className="mb-1 text-[12px] text-[#4e5969]">
            密码 / 应用专用密码
            {data?.hasPassword && <span className="ml-2 text-[#86909c]">当前：{data.passwordMasked}</span>}
          </div>
          <Input.Password
            value={password}
            onChange={setPassword}
            placeholder={data?.hasPassword ? "留空则不修改" : "Gmail 请使用 16 位应用专用密码"}
          />
        </div>
      </div>
      <div className="mt-3">
        <Button type="primary" loading={saving} onClick={save}>
          保存 SMTP 配置
        </Button>
      </div>
      <p className="mt-2 text-[11px] text-[#86909c]">
        用于注册验证码邮件；Gmail 免费账号每天约 500 封，达到上限后当天会提示「当日注册量已达上限，请明日再试」。
      </p>
    </Card>
  );
}
