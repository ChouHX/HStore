"use client";

import { Button, Modal } from "@arco-design/web-react";
import { useCallback, useEffect, useState } from "react";
import type { Announcement } from "@/lib/api-types";

/** 关闭后「今天不再提示」的标记：记录到具体日期，跨天自动失效。 */
function todayKey(id: number) {
  const d = new Date();
  const day = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, "0")}${String(d.getDate()).padStart(2, "0")}`;
  return `faka_popup_mute_${id}_${day}`;
}

/** 「已读过」标记：点「关闭」只关掉本次，刷新后仍会再弹一次。 */
function seenKey(id: number) {
  return `faka_popup_seen_${id}`;
}

/**
 * 弹窗公告。
 *
 * 两个按钮的语义不同，不能混用同一个标记：
 * - 「关闭」：本次不看了，但下次进入页面仍会弹出
 * - 「今天不再提示」：当天内不再弹出（按日期存 key，第二天自动恢复）
 *
 * 注意：后端 /api/store/home 的 popup 是**单个对象或 null**，不是数组。
 */
export function PopupAnnouncement({ popup }: { popup: Announcement | null }) {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (!popup) {
      setOpen(false);
      return;
    }
    try {
      // 当天已被「今天不再提示」静音，就不再弹
      if (localStorage.getItem(todayKey(popup.id))) {
        setOpen(false);
        return;
      }
    } catch {
      /* localStorage 不可用时按未静音处理 */
    }
    setOpen(true);
  }, [popup]);

  const close = useCallback(() => {
    setOpen(false);
    if (!popup) return;
    try {
      // 「关闭」不写静音标记，所以下次进入还会提示
      localStorage.setItem(seenKey(popup.id), "1");
    } catch {
      /* ignore */
    }
  }, [popup]);

  const muteToday = useCallback(() => {
    setOpen(false);
    if (!popup) return;
    try {
      localStorage.setItem(todayKey(popup.id), "1");
      localStorage.setItem(seenKey(popup.id), "1");
    } catch {
      /* ignore */
    }
  }, [popup]);

  if (!popup) return null;

  return (
    <Modal
      title={popup.title || "公告"}
      visible={open}
      onCancel={close}
      footer={
        <div className="flex justify-end gap-2">
          <Button onClick={close}>关闭</Button>
          <Button type="primary" onClick={muteToday}>
            今天不再提示
          </Button>
        </div>
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
