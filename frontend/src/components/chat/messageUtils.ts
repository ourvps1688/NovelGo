// chat 消息内容清理工具。
// 某些工作流（如 book-pitch）需要把机器可读的 JSON 块输出到消息末尾以便前端解析，
// 但这些 JSON 不应作为对话内容显示给用户。本模块提供通用清理函数。

// 需要从用户可见消息中隐藏的三反引号围栏语言标识。
const HIDDEN_FENCE_LANGS = new Set(["bookpitch"]);

/**
 * 移除消息内容中的「机器可读围栏块」（例如 ```bookpitch ... ```）。
 * 保留普通 Markdown 代码块和正文不受影响。
 */
export function sanitizeAssistantContent(content: string): string {
  if (!content) return content;
  // 匹配 ```lang\n...\n```，其中 lang 在隐藏列表中；允许围栏后紧跟零个或多个换行。
  const re = new RegExp(
    "```(?:" +
      [...HIDDEN_FENCE_LANGS].join("|") +
      ")\\s*\\n?[\\s\\S]*?```\\s*\\n?",
    "g",
  );
  return content.replace(re, "").trimEnd();
}
