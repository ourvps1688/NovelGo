// styleTemplates: 写作风格「选择项」的结构化常量与识别函数。
// 本文件内容含固定中文 marker（【写作风格·{id}】），与后端 internal/style/types.go
// 的 StyleConstraintCategory / StyleMarkerPrefix 常量严格对齐，非可翻译 UI 文案，
// 故加入 i18n 硬编中文检查白名单（与 genreTemplates.ts 同机制）。

// 写作风格偏好的固定分类名（与后端 STYLE_CONSTRAINT_CATEGORY 对齐）。
export const STYLE_CONSTRAINT_CATEGORY = "写作风格";

// 激活 marker 前缀（完整形如 【写作风格·{id}】），用于识别与回链风格画像。
export const STYLE_MARKER_PREFIX = "【写作风格·";

// styleMarkerOf 生成完整激活 marker（含闭合 】），供偏好面板写入偏好 content 首行。
// 放在本文件以隔离固定中文，避免 StyleConstraintPicker.tsx 触发 i18n 硬编中文检查。
export function styleMarkerOf(id: number): string {
  return `${STYLE_MARKER_PREFIX}${id}】`;
}

/**
 * detectStyleProfileId: 从偏好 content 首行 marker 【写作风格·{id}】 提取画像 id。
 * 非本系统条目（含用户自定义「写作风格」分类无 marker）返回 null。
 */
export function detectStyleProfileId(content: string): number | null {
  const firstLine = content.trimStart().split("\n", 1)[0]?.trim() ?? "";
  const m = firstLine.match(/^【写作风格·(\d+)】$/);
  if (!m) return null;
  const id = Number(m[1]);
  return Number.isFinite(id) ? id : null;
}
