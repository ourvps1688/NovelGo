package app

import (
	"strings"

	"github.com/novelgo/novelgo/internal/quality"
)

// finding_prescription_inject.go —— #114 改稿轮「验收硬约束」确定性注入。
//
// 旧链路里改稿消息走普通 chat 意图路由：命中 revision-pass(auto) 或
// internalOnly 重定向时，写章技法包完全不注入（只有下一章 next 轮才注入），
// 于是验收条件退化成一句「请给建议」的普通用户消息，LLM 每轮自由发挥、
// 整章 full_replace 推翻重写。这里按消息里的验收标记确定性注入一段
// system-reminder：不经 LLM 意图分类、不受技能 1500/2400 rune 帽影响、
// 不新增任何后台修复执行体——所有修复仍然在 chat 里人工协作完成。

// buildAcceptanceGuard 依据用户消息里的验收条件块标记返回硬约束提示。
// 无标记时返回空串（缺省行为逐字不变）。
func buildAcceptanceGuard(message string) string {
	if !strings.Contains(message, quality.AcceptanceMarker) {
		return ""
	}
	var b strings.Builder
	b.WriteString("<system-reminder>\n")
	b.WriteString("本轮消息带有【本章验收条件】块，来自本地确定性检测器（不是模型主观评分），是本轮必须逐条满足的硬约束：\n")
	b.WriteString("1. 动笔前先逐条复述「当前实测值 → 目标值 → 要改的位置」，确认判定式后再改。\n")
	b.WriteString("2. 只做定点修改（search_replace / line_range_replace）：严禁整章 full_replace 推翻重写，那会让其它已达标项回退，正是反复改稿消不掉问题的主因。\n")
	b.WriteString("3. 严禁采用每条「避免」项列出的写法；尤其不要靠堆直白情绪词或感叹号抬张力（会同时触发去 AI 味），改用具体动作细节、对抗升级与短句节奏。\n")
	b.WriteString("4. 不得自行更换数字、另立目标或把未达标项解释成「已通过」；判官主观分数不能替代这些确定性条件。\n")
	b.WriteString("5. 改完逐条回报「已达成 / 未达成 + 依据」；未达成要说明是判定器冲突还是改动不足。用户确认前不要落盘。\n")
	b.WriteString("</system-reminder>\n")
	return b.String()
}

// findingKeysJSONFromMessage 把改稿消息里的【本章目标发现项】块解析成 P1 单源
// 稳定 key 的 JSON 快照（quality.MarshalFindingKeys），供技能账本落库。
// 无标记或无可识别 key 时返回空串——旧消息/普通对话天然兼容，零行为变更。
func findingKeysJSONFromMessage(message string) string {
	return quality.MarshalFindingKeys(parseTargetFindingKeys(message))
}

// parseTargetFindingKeys 只收形如 `- l0|check|feature#n` / `- l1|...` 的行：
// 前缀必须是 P1 单源的作用域，避免把处方块、轮次保护块里的普通列表行误收。
// 不自行生成或改写 key（身份口径单源，见 finding_round_wiring_test.go）。
func parseTargetFindingKeys(message string) []string {
	if !strings.Contains(message, quality.FindingKeysMarker) {
		return nil
	}
	var keys []string
	seen := make(map[string]bool)
	for _, raw := range strings.Split(message, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if (!strings.HasPrefix(key, "l0|") && !strings.HasPrefix(key, "l1|")) || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}
