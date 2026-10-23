import type { Platform } from "./types";

const MICROSOFT_KEYWORDS = [
  "微软",
  "microsoft",
  "outlook",
  "hotmail",
  "live.com",
  "office",
  "ms邮箱",
  "msn",
];

const GOOGLE_KEYWORDS = ["谷歌", "google", "gmail", "gmail邮箱", "goog"];

/** 根据分类名称判断所属平台 */
export function detectPlatform(name: string): Platform {
  const n = (name || "").toLowerCase();
  if (MICROSOFT_KEYWORDS.some((k) => n.includes(k))) return "microsoft";
  if (GOOGLE_KEYWORDS.some((k) => n.includes(k))) return "google";
  return "other";
}

export const PLATFORM_LABEL: Record<Platform, string> = {
  microsoft: "微软邮箱",
  google: "谷歌邮箱",
  other: "其它",
};
