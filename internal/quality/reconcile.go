package quality

// reconcile.go —— #114「冲突预检」：同一次 L0 检测里若同时给出互相矛盾的
// 修复指令，LLM 无论怎么改都会按下葫芦浮起瓢（实测《残骸里的声音》8 轮
// 全文重写、5 条发现原样挂着）。这里只做确定性文本合并，不改发现数量、
// 不改判定阈值、不引入任何自动修复。
//
// 两条已知互斥：
//   - 曲线类（拖沓/爽点/坡度）要求「抬张力」，而去 AI 味要求「别用直白情绪词/
//     感叹号」。旧文案默认让 LLM 堆情绪词 → 必然新增 ai_flavor。
//   - 字数/章长类要求控制篇幅，而曲线类建议「插入对抗/补爽点」→ 单纯扩写
//     又会触发超长。

const (
	avoidAIFlavorNote = "；【冲突预检】本章同时命中去 AI 味，禁止靠堆直白情绪词或感叹号抬张力（会新增 ai_flavor），请改用具体动作细节、对抗升级与短句节奏。"
	avoidLengthNote   = "；【冲突预检】本章同时存在字数/章长约束，请在现有字数预算内替换冗余段落，不要单纯扩写。"
)

var curveChecks = map[string]bool{
	CheckPacingDrag:   true,
	CheckBeatGap:      true,
	CheckBeatDensity:  true,
	CheckOpeningSlope: true,
}

// reconcileFindingConflicts 给曲线类发现追加互斥项的可执行边界（确定性、保序）。
// 无互斥项时逐字返回原清单——缺省行为完全不变。
func reconcileFindingConflicts(raw []Finding) []Finding {
	hasAI := false
	hasLength := false
	for _, f := range raw {
		switch f.Check {
		case CheckAIFlavor:
			hasAI = true
		case CheckWordCount, CheckOpeningLength, CheckChapterLengthFit:
			hasLength = true
		}
	}
	if !hasAI && !hasLength {
		return raw
	}
	for i := range raw {
		if !curveChecks[raw[i].Check] {
			continue
		}
		if hasAI {
			raw[i].Desc += avoidAIFlavorNote
		}
		if hasLength {
			raw[i].Desc += avoidLengthNote
		}
	}
	return raw
}
