"use client";

import { contentToHtml } from "@/lib/rich-text";
import "./rich-text.css";

/**
 * 只读渲染公告正文。
 *
 * 内容来自后台富文本编辑器，渲染前统一走一遍消毒（DOMPurify），
 * 历史纯文本公告会自动转成保留换行的 HTML。
 */
export function RichTextView({ content, className }: { content: string; className?: string }) {
  const html = contentToHtml(content);
  if (!html) return null;
  return (
    <div
      className={className ? `rich-text-view ${className}` : "rich-text-view"}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
