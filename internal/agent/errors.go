package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/llm"
)

// truncateErrMsg 按 rune 截断字符串到 maxLen，用于防止过长内容（含潜在注入载体）进入 LLM 上下文。
// rune 截断避免破坏 UTF-8 多字节字符。
func truncateErrMsg(s string, maxLen int) string {
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen]) + "..."
	}
	return s
}

// deadlineFriendlyMessage 是主对话整体超时（context.DeadlineExceeded）的用户提示。
// #118：旧逻辑落到末尾兜底「对话出错，请重试」，既看不出是 30 分钟上限触发，
// 也不知道已吐出的正文是否保住；这里给可行动的文案（不谎称内容一定存在）。
const deadlineFriendlyMessage = "本轮对话已超时（主对话 30 分钟上限），已停止；中断前生成的内容（如有）已存为草稿，未落盘为章节正文。请重试，或新建会话以缩短上下文。"

// FriendlyError 将 LLM 错误转换为用户友好的消息。
// 原始 error 应由调用方另行记录日志。
func FriendlyError(err error) string {
	if errors.Is(err, context.Canceled) {
		return ""
	}
	// 超时不是供应商故障也不是程序崩溃，须与「对话出错」区分，并交代正文保全口径。
	if errors.Is(err, context.DeadlineExceeded) {
		return deadlineFriendlyMessage
	}
	// 中断类错误（工具连续失败/死循环/MaxTurns）：原因本身已是用户可读中文，直接透传。
	var ie InterruptError
	if errors.As(err, &ie) {
		return ie.InterruptReason()
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		// 网络错误/首字节超时：StatusCode=0，无 HTTP 状态码
		// base 用"网络错误"区分网络场景与其他对话错误
		if apiErr.StatusCode == 0 {
			const base = "网络错误"
			if msg := strings.TrimSpace(apiErr.Message); msg != "" {
				return fmt.Sprintf("%s：%s", base, msg)
			}
			return base
		}
		var base string
		switch apiErr.StatusCode {
		case 401:
			base = "API Key 无效，请在设置中检查"
		case 403:
			base = "API Key 无权限"
		case 429:
			base = "请求过于频繁，请稍后重试"
		default:
			if apiErr.StatusCode >= 500 {
				base = "AI 服务暂时不可用，请稍后重试"
			} else {
				base = "对话出错，请重试"
			}
		}
		if msg := strings.TrimSpace(apiErr.Message); msg != "" {
			return fmt.Sprintf("%s（HTTP %d：%s）", base, apiErr.StatusCode, msg)
		}
		return base
	}
	return "对话出错，请重试"
}
