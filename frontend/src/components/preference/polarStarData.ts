// 北极星/底线 元目标层的数据结构与序列化（数据素材，非可翻译 UI 文案，
// 已加入 scripts/i18n-check.mjs 的 ALLOWLIST_FILES 白名单，跳过硬编码中文检查）。
//
// category 是稳定存储键（跨语言不变，不能进 i18n 翻译，否则切换语言后读不到），
// 序列化标记是注入 LLM 的正文格式，与偏好系统整体中文正文一致。
export const POLAR_STAR_CATEGORY = "北极星底线";

export interface PolarData {
  north_star: string;
  bottom_lines: string[];
}

// 序列化：易读文本块，注入 LLM 时天然成句。
export function serializePolar(data: PolarData): string {
  const lines = data.bottom_lines
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => `- ${l}`)
    .join("\n");
  return `【北极星】${data.north_star.trim()}\n【底线】\n${lines}`;
}

// 反序列化：宽容解析（LLM 经 upsert_preference 改写后格式可能略变）。
export function parsePolarContent(content: string): PolarData {
  const north =
    content.match(/【北极星】([\s\S]*?)(?:\n【底线】|$)/)?.[1]?.trim() ?? "";
  const linesPart = content.includes("【底线】")
    ? content.split("【底线】")[1]
    : "";
  const bottomLines = linesPart
    .split("\n")
    .map((l) => l.replace(/^[-*]\s*/, "").trim())
    .filter(Boolean);
  return { north_star: north, bottom_lines: bottomLines };
}
