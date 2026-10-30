import DOMPurify from "dompurify";

/**
 * 公告内容在数据库里是 HTML 字符串（富文本编辑器产出）。
 * 历史数据是纯文本，渲染前需要统一成 HTML，否则换行会丢。
 */

const HTML_TAG = /<\/?[a-z][^>]*>/i;

function escapeHtml(text: string) {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

/** 是否是编辑器产出的 HTML（用于兼容早期的纯文本公告）。 */
export function looksLikeHtml(content: string) {
  return HTML_TAG.test(content);
}

/**
 * 富文本永远不可信：管理员也可能粘贴进来带脚本的内容，
 * 而它会被渲染到所有访客的页面上。
 */
export function sanitizeRichText(html: string) {
  if (!html) return "";
  if (typeof window === "undefined") return "";
  return DOMPurify.sanitize(html, {
    USE_PROFILES: { html: true },
    ADD_ATTR: ["target", "rel"],
    FORBID_TAGS: ["style", "form", "input", "iframe", "object", "embed"],
  });
}

/** 纯文本 -> 可安全渲染的 HTML（保留换行）。 */
function plainTextToHtml(text: string) {
  return escapeHtml(text).replace(/\r?\n/g, "<br />");
}

/** 存库内容转成可渲染的 HTML：历史纯文本自动转义 + 换行，HTML 走消毒。 */
export function contentToHtml(content: string) {
  if (!content) return "";
  if (!looksLikeHtml(content)) return plainTextToHtml(content);
  return sanitizeRichText(content);
}

/** 列表/摘要用的纯文本（表格里不能塞 HTML）。 */
export function contentToPlainText(content: string) {
  if (!content) return "";
  if (!looksLikeHtml(content)) return content;
  if (typeof document === "undefined") {
    return content.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
  }
  const holder = document.createElement("div");
  holder.innerHTML = content;
  return (holder.textContent ?? "").replace(/\s+/g, " ").trim();
}

/** 编辑器为空时 Quill 会留下一个空段落，存库前统一按空处理。 */
export function normalizeEditorHtml(html: string) {
  const trimmed = html.trim();
  if (!trimmed || trimmed === "<p><br></p>" || trimmed === "<p></p>") return "";
  return trimmed;
}
