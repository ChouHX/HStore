"use client";

import { Button, Modal } from "@arco-design/web-react";
import { useCallback, useEffect, useRef, useState } from "react";
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

/** 当天是否已被「今天不再提示」静音，供首页决定是否自动弹出。 */
export function isPopupMutedToday(id: number): boolean {
  try {
    return Boolean(localStorage.getItem(todayKey(id)));
  } catch {
    return false;
  }
}

interface Props {
  popup: Announcement | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * 弹窗公告（受控）。
 *
 * 两个按钮的语义不同，不能混用同一个标记：
 * - 「关闭」：本次不看了，但下次进入页面仍会弹出
 * - 「今天不再提示」：当天内不再弹出（按日期存 key，第二天自动恢复）
 *
 * 关闭时先播放收起过渡动画，动画结束后再通知父组件隐藏，
 * 这样弹窗消失的过程不会显得生硬，同时首页 header 上的公告 icon 会亮一下。
 *
 * 注意：后端 /api/store/home 的 popup 是**单个对象或 null**，不是数组。
 */
export function PopupAnnouncement({ popup, open, onOpenChange }: Props) {
  const [closing, setClosing] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (open) setClosing(false);
  }, [open]);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const dismiss = useCallback(
    (muteToday: boolean) => {
      if (!popup) {
        onOpenChange(false);
        return;
      }
      try {
        if (muteToday) localStorage.setItem(todayKey(popup.id), "1");
        localStorage.setItem(seenKey(popup.id), "1");
      } catch {
        /* ignore */
      }
      // 播放收起动画：向下淡出并轻微缩小
      setClosing(true);
      timer.current = setTimeout(() => {
        setClosing(false);
        onOpenChange(false);
      }, 200);
    },
    [popup, onOpenChange],
  );

  if (!popup) return null;

  return (
    <Modal
      title={popup.title || "公告"}
      visible={open}
      onCancel={() => dismiss(false)}
      wrapClassName={`notice-popup${closing ? " notice-popup-closing" : ""}`}
      footer={
        <div className="flex justify-end gap-2">
          <Button onClick={() => dismiss(false)}>关闭</Button>
          <Button type="primary" onClick={() => dismiss(true)}>
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
