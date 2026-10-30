"use client";

import Quill from "quill";
import { useEffect, useRef } from "react";
import "quill/dist/quill.snow.css";
import "./rich-text.css";
import { normalizeEditorHtml } from "@/lib/rich-text";

/**
 * 公告正文的富文本编辑器。
 *
 * 用 Quill（成熟的 WYSIWYG 库）直接挂载，而不是 react-quill：
 * 后者依赖 findDOMNode，在 React 19 下已经不可用。
 *
 * 对外的值始终是 HTML 字符串，空内容统一归一化成 ""。
 */

const TOOLBAR = [
  [{ header: [1, 2, 3, false] }],
  ["bold", "italic", "underline", "strike"],
  [{ color: [] }, { background: [] }],
  [{ list: "ordered" }, { list: "bullet" }],
  [{ align: [] }],
  ["blockquote", "link"],
  ["clean"],
];

interface Props {
  value: string;
  onChange: (html: string) => void;
  placeholder?: string;
  minHeight?: number;
}

export function RichTextEditor({
  value,
  onChange,
  placeholder = "请输入公告内容…",
  minHeight = 180,
}: Props) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const quillRef = useRef<Quill | null>(null);
  const lastHtml = useRef("");
  const onChangeRef = useRef(onChange);

  onChangeRef.current = onChange;

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    // Quill 会把工具栏插到容器前面，所以每次都从一个干净的子节点开始，
    // 卸载时整块清掉，避免 StrictMode 双挂载留下两套工具栏。
    const mount = document.createElement("div");
    host.appendChild(mount);

    const quill = new Quill(mount, {
      theme: "snow",
      placeholder,
      modules: { toolbar: TOOLBAR },
    });
    quillRef.current = quill;

    if (value) {
      quill.clipboard.dangerouslyPasteHTML(value);
    }
    lastHtml.current = normalizeEditorHtml(quill.root.innerHTML);

    const handleChange = () => {
      const html = normalizeEditorHtml(quill.root.innerHTML);
      if (html === lastHtml.current) return;
      lastHtml.current = html;
      onChangeRef.current(html);
    };
    quill.on("text-change", handleChange);

    return () => {
      quill.off("text-change", handleChange);
      quillRef.current = null;
      host.innerHTML = "";
    };
    // 只初始化一次：value 的外部变化交给下面的 effect。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const quill = quillRef.current;
    if (!quill) return;
    const incoming = value ?? "";
    if (normalizeEditorHtml(incoming) === lastHtml.current) return;

    const range = quill.getSelection();
    quill.clipboard.dangerouslyPasteHTML(incoming);
    lastHtml.current = normalizeEditorHtml(incoming);
    if (range) quill.setSelection(range.index, range.length);
  }, [value]);

  return (
    <div
      className="rich-text-editor"
      style={{ "--rt-min-height": `${minHeight}px` } as React.CSSProperties}
    >
      <div ref={hostRef} />
    </div>
  );
}
