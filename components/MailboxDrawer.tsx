"use client";

import { Drawer } from "@arco-design/web-react";
import { useState } from "react";
import { MailboxPanel, type MailboxTarget } from "@/components/MailboxPanel";

export type { MailboxTarget };

/** 抽屉形态的取件（用于订单详情、我的邮箱列表等场景） */
export function MailboxDrawer({
  visible,
  target,
  onClose,
}: {
  visible: boolean;
  target: MailboxTarget | null;
  onClose: () => void;
}) {
  const [email, setEmail] = useState("");

  return (
    <Drawer
      width={640}
      title={
        <div className="flex items-center gap-2">
          <span>取件</span>
          {email ? <span className="font-mono text-[12px] text-[#86909c]">{email}</span> : null}
        </div>
      }
      visible={visible}
      onCancel={onClose}
      footer={null}
      unmountOnExit
    >
      <MailboxPanel target={target} onEmail={setEmail} />
    </Drawer>
  );
}
