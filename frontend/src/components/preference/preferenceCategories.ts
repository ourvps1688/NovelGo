// 偏好分类稳定标识符（后端 FormatPreferences 按 category 区分注入，不随语言变化）。
// 此文件已加入 scripts/i18n-check.mjs 的 ALLOWLIST_FILES，允许硬编码中文字符串。
export const CATEGORY_PLATFORM = "平台约束";
export const CATEGORY_CREATIVE = "创作约束";
// 新书孵化工作流「采纳方案」表单的扩展字段，随孵化器小说一起持久化。
// 这些值仅做记录，不参与本书系统提示注入。
export const CATEGORY_TAGS = "标签";
export const CATEGORY_DIRECTION = "方向";
export const CATEGORY_HOOK = "一句话卖点";
