package agent

// #118 P0：流式正文在「重试前 Reset buffer」时会被整段丢弃，而整体超时又常
// 发生在重试等待/下一次请求中（主 chat 30 分钟上限），用户看到的半章正文因此
// 彻底丢失、DB 无痕（2026-11-14 00:38 实测）。此处把中断前的 partial 文本落一条
// 「仅前端可见、不进 API 上下文」的 assistant 消息，保证可复制恢复。
//
// 纪律：零新增表、零自动修复、零执行体；DB 已是真相源，草稿只是普通会话消息。
// ToAPI=false 是关键——重试复用同一份 opts.Messages，若把半章正文塞回上下文，
// 模型会误以为正文已成稿而不再调用落盘工具。

import (
	"strings"

	"github.com/novelgo/novelgo/internal/session"
)

// interruptedDraftMarker 让用户在会话里一眼识别「这不是已落盘的章节正文」。
const interruptedDraftMarker = "【本轮中断前已生成的内容（未落盘为章节正文，可复制后继续使用）】\n\n"

// canSaveInterruptedDraft 判定是否值得落草稿（纯函数，便于契约测试）。
// 子 Agent 复用主 turn 的 SessionID，但它的中间报告不是给用户看的章节正文：
// 若也写成 ToFrontend 消息，会把检测/自检的半成品混进主会话视图。
func canSaveInterruptedDraft(opts *RunOptions, content string) bool {
	return strings.TrimSpace(content) != "" && opts.SubTaskID == ""
}

// saveInterruptedDraft 保底保存中断前已生成的正文。空内容/子 Agent 不写；
// 写失败只告警，绝不向调用方返回错误（观测失败不得改变对话主流程）。
// 返回是否真的落库，供统一出口 defer 记录。
func (a *Agent) saveInterruptedDraft(opts *RunOptions, content, thinking string) bool {
	if !canSaveInterruptedDraft(opts, content) {
		return false
	}
	msg := &session.Message{
		SessionID:       opts.SessionID,
		TurnID:          opts.TurnID,
		AgentType:       opts.AgentType,
		SubTaskID:       opts.SubTaskID,
		Role:            "assistant",
		Content:         interruptedDraftMarker + content,
		ThinkingContent: thinking,
		Version:         opts.ActiveVersion,
		ToAPI:           false,
		ToFrontend:      true,
	}
	if err := a.db.Create(msg).Error; err != nil {
		a.logger.Error("保存中断草稿失败", "turnID", opts.TurnID, "err", err)
		return false
	}
	a.logger.Warn("已保存中断草稿", "turnID", opts.TurnID, "runes", len([]rune(content)))
	return true
}
