import {
  Button,
  Card,
  Input,
  InputNumber,
  Message,
  Select,
  Space,
  Switch,
  Tag,
} from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";

interface SmtpData {
  provider: "smtp" | "resend";
  host: string;
  port: number;
  security: string;
  username: string;
  fromName: string;
  enabled: boolean;
  hasPassword: boolean;
  passwordMasked: string;
  resendDomain: string;
  hasResendApiKey: boolean;
  resendKeyMasked: string;
  resendFrom: string;
  ready: boolean;
  todaySent: number;
  dailyLimit: number;
  smtpDailyLimit: number;
  resendDailyLimit: number;
}

export function SmtpSection() {
  const [data, setData] = useState<SmtpData | null>(null);
  const [provider, setProvider] = useState<"smtp" | "resend">("smtp");
  const [host, setHost] = useState("smtp.gmail.com");
  const [port, setPort] = useState(465);
  const [security, setSecurity] = useState("ssl");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [fromName, setFromName] = useState("HShop");
  const [enabled, setEnabled] = useState(true);
  const [resendDomain, setResendDomain] = useState("");
  const [resendApiKey, setResendApiKey] = useState("");
  const [testTo, setTestTo] = useState("");
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  const load = useCallback(async () => {
    const j = await fetch("/api/admin/smtp").then((r) => r.json());
    if (j.ok) {
      const d = j as SmtpData;
      setData(d);
      setProvider(d.provider === "resend" ? "resend" : "smtp");
      setHost(d.host);
      setPort(d.port);
      setSecurity(d.security);
      setUsername(d.username);
      setFromName(d.fromName);
      setEnabled(d.enabled);
      setResendDomain(d.resendDomain ?? "");
    } else {
      Message.error(j.message ?? "加载发信配置失败");
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
        body: JSON.stringify({
          provider,
          host,
          port,
          security,
          username,
          fromName,
          enabled,
          resendDomain,
          password: password || undefined,
          resendApiKey: resendApiKey || undefined,
        }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("发信配置已保存");
        setPassword("");
        setResendApiKey("");
        await load();
      } else {
        Message.error(j.message ?? "保存失败");
      }
    } finally {
      setSaving(false);
    }
  }

  async function sendTest() {
    if (!testTo.trim()) {
      Message.warning("请输入测试收件邮箱");
      return;
    }
    setTesting(true);
    try {
      const j = await fetch("/api/admin/smtp", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "test", testTo: testTo.trim() }),
      }).then((r) => r.json());
      if (j.ok) Message.success(j.message ?? "测试邮件已发送");
      else Message.error(j.message ?? "发送失败");
    } finally {
      setTesting(false);
    }
  }

  const isResend = provider === "resend";

  return (
    <Card
      title="注册验证码邮件"
      bordered={false}
      extra={
        <Space size={8}>
          {data && (
            <Tag size="small" color={data.todaySent >= data.dailyLimit ? "red" : "gray"}>
              今日已发 {data.todaySent} / {data.dailyLimit}
            </Tag>
          )}
          {data && (
            <Tag size="small" color={data.ready ? "green" : "orange"}>
              {data.ready ? "已就绪" : "未就绪"}
            </Tag>
          )}
          <Switch checked={enabled} onChange={setEnabled} size="small" />
        </Space>
      }
    >
      <div className="mb-4 grid gap-3 sm:grid-cols-2">
        <div>
          <div className="mb-1 text-[12px] text-[#4e5969]">发信通道</div>
          <Select value={provider} onChange={(v) => setProvider(v as "smtp" | "resend")} style={{ width: "100%" }}>
            <Select.Option value="smtp">SMTP（Gmail / 自建邮箱）</Select.Option>
            <Select.Option value="resend">Resend（域名 + API Key）</Select.Option>
          </Select>
        </div>
        <div>
          <div className="mb-1 text-[12px] text-[#4e5969]">发件人名称</div>
          <Input value={fromName} onChange={setFromName} placeholder="HShop" maxLength={32} />
        </div>
      </div>

      {isResend ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">发信域名</div>
            <Input
              value={resendDomain}
              onChange={setResendDomain}
              placeholder="opcuu.com"
            />
            <div className="mt-1 text-[11px] text-[#86909c]">
              需先在 Resend 控制台完成该域名的 DNS 验证；也可直接填 noreply@opcuu.com
            </div>
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">
              API Key
              {data?.hasResendApiKey && (
                <span className="ml-2 text-[#86909c]">当前：{data.resendKeyMasked}</span>
              )}
            </div>
            <Input.Password
              value={resendApiKey}
              onChange={setResendApiKey}
              placeholder={data?.hasResendApiKey ? "留空则不修改" : "re_ 开头"}
            />
            <div className="mt-1 text-[11px] text-[#86909c]">
              实际发件地址：{data?.resendFrom || "—"}
            </div>
          </div>
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">SMTP 服务器</div>
            <Input value={host} onChange={setHost} placeholder="smtp.gmail.com" />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">端口</div>
            <InputNumber
              value={port}
              onChange={(v) => setPort(Number(v) || 465)}
              min={1}
              max={65535}
              style={{ width: "100%" }}
            />
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">加密方式</div>
            <Select value={security} onChange={(v) => setSecurity(String(v))} style={{ width: "100%" }}>
              <Select.Option value="ssl">SSL（465）</Select.Option>
              <Select.Option value="starttls">TLS / STARTTLS（587）</Select.Option>
            </Select>
          </div>
          <div>
            <div className="mb-1 text-[12px] text-[#4e5969]">邮箱账号（需身份验证）</div>
            <Input value={username} onChange={setUsername} placeholder="yourname@gmail.com" />
          </div>
          <div className="sm:col-span-2">
            <div className="mb-1 text-[12px] text-[#4e5969]">
              密码 / 应用专用密码
              {data?.hasPassword && (
                <span className="ml-2 text-[#86909c]">当前：{data.passwordMasked}</span>
              )}
            </div>
            <Input.Password
              value={password}
              onChange={setPassword}
              placeholder={data?.hasPassword ? "留空则不修改" : "Gmail 请使用 16 位应用专用密码"}
            />
          </div>
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <Button type="primary" loading={saving} onClick={save}>
          保存发信配置
        </Button>
        <Input
          value={testTo}
          onChange={setTestTo}
          placeholder="测试收件邮箱"
          style={{ width: 220 }}
          allowClear
        />
        <Button loading={testing} onClick={sendTest}>
          发送测试邮件
        </Button>
      </div>

      <p className="mt-2 text-[11px] text-[#86909c]">
        {isResend
          ? "Resend 只需验证过的域名与 API Key，无需邮箱密码；本地每日封顶 " +
            (data?.resendDailyLimit ?? 1000) +
            " 封，避免超额计费。"
          : "Gmail 免费账号每天约 500 封，达到上限后当天会提示「当日注册量已达上限，请明日再试」。"}
      </p>
    </Card>
  );
}
