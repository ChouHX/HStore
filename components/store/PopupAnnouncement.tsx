"use client";

import { Button, Modal } from "@arco-design/web-react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { Announcement } from "@/lib/api-types";
import { RichTextView } from "@/components/common/RichTextView";

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
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  // 重新打开时清掉上一次收缩动画留下的内联样式（快速连点关闭/打开时会复用同一个节点）
  useEffect(() => {
    if (!open) return;
    const modal = document.querySelector<HTMLElement>(".notice-popup .arco-modal");
    const mask = document.querySelector<HTMLElement>(".notice-popup .arco-modal-mask");
    if (modal) {
      modal.style.transform = "";
      modal.style.opacity = "";
      modal.style.transition = "";
      modal.style.animation = "";
      modal.style.willChange = "";
    }
    if (mask) {
      mask.style.opacity = "";
      mask.style.transition = "";
      mask.style.animation = "";
    }
  }, [open]);

  /**
   * 关闭时让弹窗直接「缩进」header / Hero 卡片上的公告图标。
   *
   * 用 FLIP 思路：量出弹窗与目标图标的中心差值，把弹窗平移过去并缩到图标大小。
   * 比固定方向的淡出更自然，用户能看出内容被收进了哪个入口。
   */
  function collapseToIcon(): number {
    const duration = 300;
    const modal = document.querySelector<HTMLElement>(".notice-popup .arco-modal");
    if (!modal) return 0;

    const anchors = Array.from(document.querySelectorAll<HTMLElement>("[data-notice-anchor]"));
    const anchor = anchors.find((el) => {
      const r = el.getBoundingClientRect();
      return r.width > 0 && r.height > 0;
    });

    const m = modal.getBoundingClientRect();
    let transform = "scale(0.9)";
    if (anchor) {
      const a = anchor.getBoundingClientRect();
      const dx = a.left + a.width / 2 - (m.left + m.width / 2);
      const dy = a.top + a.height / 2 - (m.top + m.height / 2);
      // 缩到图标宽度左右，避免缩成 0 时的像素抖动
      const scale = Math.max(0.04, Math.min(0.3, a.width / m.width));
      transform = `translate(${dx}px, ${dy}px) scale(${scale})`;
    }

    // 入场动画的 fill-mode 会把 transform 固定住，必须先关掉
    modal.style.animation = "none";
    modal.style.transformOrigin = "center center";
    modal.style.willChange = "transform, opacity";
    // 前半程保持不透明，能看清弹窗被吸进图标；后半程再淡出
    modal.style.transition = `transform ${duration}ms cubic-bezier(0.32, 0.72, 0, 1), opacity 140ms ease-in 160ms`;

    const mask = document.querySelector<HTMLElement>(".notice-popup .arco-modal-mask");
    if (mask) {
      mask.style.animation = "none";
      mask.style.transition = `opacity ${duration}ms ease-in`;
    }

    void modal.offsetWidth; // 强制重排，让 transition 从初始状态开始
    modal.style.transform = transform;
    modal.style.opacity = "0";
    if (mask) mask.style.opacity = "0";
    return duration;
  }

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
      if (timer.current) clearTimeout(timer.current);
      // 播放收起动画：直接向 header / Hero 公告图标收缩
      const duration = collapseToIcon();
      timer.current = setTimeout(
        () => {
          onOpenChange(false);
        },
        duration > 0 ? duration + 20 : 0,
      );
    },
    [popup, onOpenChange],
  );

  if (!popup) return null;

  return (
    <Modal
      title={popup.title || "公告"}
      visible={open}
      onCancel={() => dismiss(false)}
      wrapClassName="notice-popup"
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
      <div className="max-h-[60vh] overflow-y-auto text-[13px] leading-relaxed text-[#4e5969]">
        <RichTextView content={popup.content} />
      </div>
    </Modal>
  );
}
