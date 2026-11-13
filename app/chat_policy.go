package app

// #118 P1：主 chat 的单轮预算策略（MaxTurns 分档 + 历史 tool 结果裁剪）。
//
// 背景（2026-11-14 00:38 实测）：一次「撰写下一章」在 30 分钟主 ctx 内
// 累积 289 条消息 / 73.9 万 tokens（其中 turn 29 单轮就 248 个 tool 结果、
// 352KB 字符），逼近 0.85 压缩阈值才压缩，巨量 prompt 让请求极慢并反复
// 撞 9 分钟空闲看门狗，最终被 30 分钟硬超时砍断、半章正文丢失。
//
// 这里只做「送进 LLM 之前」的收敛，不动 DB 真相源、不动工具执行/回执契约：
//   - MaxTurns 由恒 100 收敛为写章 60 / 普通 40（写章仍留足写前对齐+大纲+审批+正文）；
//   - 历史 tool 结果只保留最近 toolHistoryKeepFull 条全文，更早的截到
//     toolHistoryClipRunes 并标注可重新调用（模型需要时自己重查）。
//
// 纪律：零自动修复、零新增执行体、纯本地、零上传。

import "github.com/novelgo/novelgo/internal/text"

const (
	// 写章轮要跑写前对齐 → 任务卡 → 大纲 → 审批 → 正文，轮数不能压到
	// chapterci 的 12 那种量级，否则会中途「已达最大轮数」；60 是保守收敛。
	writingTurnMaxTurns = 60
	chatTurnMaxTurns    = 40

	// 最近 N 条 tool 结果保留全文（压缩后通常不足 40 条，天然不裁剪）。
	toolHistoryKeepFull = 40
	// 更早的 tool 结果截断到该 rune 数，并追加重新调用提示。
	toolHistoryClipRunes = 600

	toolHistoryClipNote = "\n（历史工具结果已裁剪，如需完整内容请重新调用该工具）"
)

// chatMaxTurnsFor 按本轮命中的技能判断轮数上限：命中写章总调度 next 用写章档。
// activeSkills 已包含 slash 注入名（见 app/chat.go），因此 /next 同样命中。
func chatMaxTurnsFor(activeSkills []string) int {
	for _, name := range activeSkills {
		if name == "next" {
			return writingTurnMaxTurns
		}
	}
	return chatTurnMaxTurns
}

// clipHistoricalToolContent 裁剪「较老的历史 tool 结果」的 content。
// isHistorical=false（最近 toolHistoryKeepFull 条）或内容本就短于上限时逐字不动。
// 返回是否真的裁过，调用方据此重算该消息的 token（避免按持久化原值高估预算）。
func clipHistoricalToolContent(api map[string]any, isHistorical bool) bool {
	if !isHistorical {
		return false
	}
	content, _ := api["content"].(string)
	if len([]rune(content)) <= toolHistoryClipRunes {
		return false
	}
	api["content"] = text.TruncateRunes(content, toolHistoryClipRunes) + toolHistoryClipNote
	return true
}
