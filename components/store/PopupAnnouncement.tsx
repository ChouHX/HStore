"use client";

import { Button, Modal } from "@arco-design/web-react";
import { useEffect, useState } from "react";
import type { Announcement } from "@/lib/announcements";

export function PopupAnnouncement({ popup }: { popup: Announcement | null }) {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!popup) return;
    const key = `faka_popup_seen_${popup.id}`;
    try {
      if (!localStorage.getItem(key)) setOpen(true);
    } catch {
      setOpen(true);
    }
  }, [popup]);

  if (!popup) return null;

  function close() {
    setOpen(false);
    try {
      localStorage.setItem(`faka_popup_seen_${popup!.id}`, "1");
    } catch {
      /* ignore */
    }
  }

  return (
    <Modal
      title={popup.title || "公告"}
      visible={open}
      onCancel={close}
      footer={
        <Button type="primary" onClick={close}>
          我知道了
        </Button>
      }
      style={{ width: 520 }}
      unmountOnExit
      maskClosable={false}
    >
      <div className="max-h-[60vh] overflow-y-auto whitespace-pre-wrap text-[13px] leading-relaxed text-[#4e5969]">
        {popup.content}
      </div>
    </Modal>
  );
}
