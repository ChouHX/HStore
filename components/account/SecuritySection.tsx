import { Alert, Button, Card, Form, Input, Message } from "@arco-design/web-react";
import { useState } from "react";
import { useSession } from "@/src/hooks/useSession";

/** 账号安全：自助修改登录密码。 */
export function SecuritySection() {
  const user = useSession();
  const [form] = Form.useForm();
  const [busy, setBusy] = useState(false);

  async function submit(values: { oldPassword: string; newPassword: string; confirm: string }) {
    if (values.newPassword !== values.confirm) {
      Message.warning("两次输入的新密码不一致");
      return;
    }
    setBusy(true);
    try {
      const j = await fetch("/api/me/password", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ oldPassword: values.oldPassword, newPassword: values.newPassword }),
      }).then((r) => r.json());
      if (j.ok) {
        Message.success("密码已修改，下次登录请使用新密码");
        form.resetFields();
      } else {
        Message.error(j.message ?? "修改失败");
      }
    } catch {
      Message.error("网络错误，请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <Card bordered={false} title="修改登录密码">
        <Alert
          type="info"
          content={`当前账号：${user?.username ?? "-"}。修改后当前登录仍然有效，下次登录请使用新密码。`}
          style={{ marginBottom: 16 }}
        />
        <Form form={form} layout="vertical" onSubmit={submit} style={{ maxWidth: 420 }}>
          <Form.Item
            label="原密码"
            field="oldPassword"
            rules={[{ required: true, message: "请输入原密码" }]}
          >
            <Input.Password placeholder="当前登录密码" autoComplete="current-password" />
          </Form.Item>
          <Form.Item
            label="新密码"
            field="newPassword"
            rules={[
              { required: true, message: "请输入新密码" },
              { minLength: 6, message: "密码至少 6 位" },
            ]}
          >
            <Input.Password placeholder="至少 6 位" autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            label="确认新密码"
            field="confirm"
            rules={[{ required: true, message: "请再次输入新密码" }]}
          >
            <Input.Password placeholder="再次输入新密码" autoComplete="new-password" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={busy}>
              保存
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
