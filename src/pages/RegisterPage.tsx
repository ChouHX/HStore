import { Button, Card, Form, Input, Message, Typography } from "@arco-design/web-react";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { invalidateSession } from "@/src/hooks/useSession";

export function RegisterPage() {
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [sending, setSending] = useState(false);
  const [countdown, setCountdown] = useState(0);
  const [loading, setLoading] = useState(false);
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    return () => {
      if (timer.current) clearInterval(timer.current);
    };
  }, []);

  function startCountdown() {
    setCountdown(60);
    timer.current = setInterval(() => {
      setCountdown((s) => {
        if (s <= 1) {
          if (timer.current) clearInterval(timer.current);
          timer.current = null;
          return 0;
        }
        return s - 1;
      });
    }, 1000);
  }

  async function sendCode() {
    const email = form.getFieldValue("email") as string | undefined;
    if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      Message.warning("请先输入正确的邮箱地址");
      return;
    }
    setSending(true);
    try {
      const j = await fetch("/api/auth/register/code", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: email.trim() }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success(j.message ?? "验证码已发送");
        startCountdown();
      } else {
        Message.error(j.message ?? "发送失败");
      }
    } catch {
      Message.error("发送请求失败");
    } finally {
      setSending(false);
    }
  }

  async function onSubmit(values: { email: string; code: string; password: string; confirm: string; nickname?: string }) {
    if (values.password !== values.confirm) {
      Message.warning("两次输入的密码不一致");
      return;
    }
    setLoading(true);
    try {
      const j = await fetch("/api/auth/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          email: values.email.trim(),
          code: values.code.trim(),
          password: values.password,
          nickname: values.nickname?.trim() ?? "",
        }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("注册成功，已自动登录");
        invalidateSession();
        navigate("/", { replace: true });
      } else {
        Message.error(j.message ?? "注册失败");
      }
    } catch {
      Message.error("注册请求失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center px-4 py-8">
      <Card style={{ width: 380 }} bordered={false}>
        <div className="mb-1 flex items-center gap-2">
          <img src="/favicon.svg" alt="HShop" className="h-9 w-9 rounded-lg" />
          <Typography.Title heading={5} style={{ margin: 0 }}>
            注册 HShop
          </Typography.Title>
        </div>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          使用邮箱验证码注册，注册后邮箱即为登录账号
        </Typography.Text>

        <Form form={form} layout="vertical" style={{ marginTop: 16 }} onSubmit={onSubmit}>
          <Form.Item
            label="邮箱"
            field="email"
            rules={[
              { required: true, message: "请输入邮箱" },
              { match: /^[^\s@]+@[^\s@]+\.[^\s@]+$/, message: "邮箱格式不正确" },
            ]}
          >
            <Input placeholder="you@example.com" />
          </Form.Item>

          <Form.Item
            label="邮箱验证码"
            field="code"
            rules={[{ required: true, message: "请输入 6 位验证码" }]}
          >
            <Input
              placeholder="6 位数字验证码"
              maxLength={6}
              suffix={
                <Button size="mini" type="text" loading={sending} disabled={countdown > 0} onClick={sendCode}>
                  {countdown > 0 ? countdown + "s 后重发" : "发送验证码"}
                </Button>
              }
            />
          </Form.Item>

          <Form.Item label="昵称（可选）" field="nickname">
            <Input placeholder="默认为邮箱前缀" maxLength={32} />
          </Form.Item>

          <Form.Item
            label="密码"
            field="password"
            rules={[
              { required: true, message: "请输入密码" },
              { minLength: 6, message: "密码至少 6 位" },
            ]}
          >
            <Input.Password placeholder="至少 6 位" />
          </Form.Item>

          <Form.Item
            label="确认密码"
            field="confirm"
            rules={[{ required: true, message: "请再次输入密码" }]}
          >
            <Input.Password placeholder="再次输入密码" />
          </Form.Item>

          <Button type="primary" htmlType="submit" long loading={loading}>
            注册并登录
          </Button>
        </Form>

        <div className="mt-3 flex items-center justify-between text-[12px]">
          <Link to="/login">已有账号？去登录</Link>
          <Link to="/">返回商城</Link>
        </div>
      </Card>
    </div>
  );
}
