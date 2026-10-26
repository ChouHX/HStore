import { Button, Card, Form, Input, Message, Typography } from "@arco-design/web-react";
import { useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { invalidateSession } from "@/src/hooks/useSession";

export function LoginPage() {
  const navigate = useNavigate();
  const loc = useLocation();
  const [loading, setLoading] = useState(false);

  const from = (loc.state as { from?: string } | null)?.from;

  async function onSubmit(values: { username: string; password: string }) {
    setLoading(true);
    try {
      const res = await fetch("/api/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(values),
      });
      const j = await res.json();
      if (j.ok) {
        Message.success("登录成功");
        invalidateSession();
        const fallback = j.user?.role === "admin" ? "/admin/overview" : "/";
        navigate(from && from !== "/login" ? from : fallback, { replace: true });
      } else {
        Message.error(j.message ?? "登录失败");
      }
    } catch {
      Message.error("请求失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <Card style={{ width: 360 }} bordered={false}>
        <div className="mb-1 flex items-center gap-2">
          <img src="/favicon.svg" alt="HShop" className="h-9 w-9 rounded-lg" />
          <Typography.Title heading={5} style={{ margin: 0 }}>
            HShop
          </Typography.Title>
        </div>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          登录 · 演示账号 demo/demo123 · 管理员 admin/admin123
        </Typography.Text>

        <Form
          layout="vertical"
          style={{ marginTop: 16 }}
          initialValues={{ username: "demo", password: "demo123" }}
          onSubmit={onSubmit}
        >
          <Form.Item label="用户名" field="username" rules={[{ required: true, message: "请输入用户名" }]}>
            <Input placeholder="用户名" />
          </Form.Item>
          <Form.Item label="密码" field="password" rules={[{ required: true, message: "请输入密码" }]}>
            <Input.Password placeholder="密码" />
          </Form.Item>
          <Button type="primary" htmlType="submit" long loading={loading}>
            登录
          </Button>
        </Form>

        <div style={{ marginTop: 12, textAlign: "center" }}>
          <Link to="/" style={{ fontSize: 12 }}>
            返回商城
          </Link>
        </div>
      </Card>
    </div>
  );
}
