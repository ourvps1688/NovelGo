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
// #118：旧逻辑落到末尾兜底「对话出错，请重试」，既看不出是超时触发，也不知道
// 已吐出的正文是否保住；这里给可行动的文案（不谎称内容一定存在）。
// #119：写章档预算已放宽到 60 分钟，文案不再写死 30；并提醒正文可能已落盘，
// 避免「已写成却被判失败」时用户误以为整章丢了。
const deadlineFriendlyMessage = "本轮对话已超时（普通对话 30 分钟 / 写章 60 分钟上限），已停止；中断前生成的内容（如有）已存为草稿，未落盘为章节正文。请先查看章节列表确认正文是否已写入，再决定重试或新建会话。"

// detachedDeadlineFriendlyMessage 是写后维护子 Agent「独立预算」到期的提示（#121）。
// 03:16 实测：chapter-ci 的 8 分钟独立预算到期后，DeadlineExceeded 经 FriendlyError
// 被包装成「本轮对话已超时（30/60 分钟上限）…正文未落盘」，与事实完全相反——
// 正文和上一轮 L1 都已落盘，主对话根本没超时。后台错误不得借用主对话文案。
const detachedDeadlineFriendlyMessage = "后台写后维护（台账同步 / 章节检测）已到独立预算上限并停止；这轮后台检查未完成，但章节正文与上一轮检测结果都已落盘、不受影响。如需最新结论，请在检测面板点「重新检测本章」。"

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
