package voice

// P1 WBS T3.2：Voice 台账渲染（voice provider 追加块，格式单源）。
// 纯文本、纯函数：把已落库维度的 content 按 §0.1 固定顺序渲染为提示词块。
// 消费端契约（WBS §0.4）：台账块追加在 P0 心智正文之后；台账空 → 返回空串，
// provider 输出与 P0 逐字一致（降级纪律，本 WBS 最高验收标准）。
//
// 渲染口径（宁缺勿错）：
//   - 只渲染 content：metrics/evidence 是溯源元数据，不进提示词；
//   - 无内容行不渲染；已知维度按 §0.1 固定序渲染（同维度重复取首现）；
//   - 未知维度（坏数据，Upsert 已拒）排最后按输入序，与 Store.List
//     「未知维度排最后，不丢行」口径一致。

import "strings"

// LedgerHeader 台账块头（嵌于 Voice 块体内、P0 心智正文之后）。
const LedgerHeader = "【文风台账】"

// dimLabels 维度中文标签（§0.1 表「维度」列短名，渲染可读性用）；
// 枚举值仍是行键语义，diff/快照以枚举值为准。
var dimLabels = map[Dimension]string{
	DimSentenceFingerprint: "句式指纹",
	DimHighFreqWords:       "高频词",
	DimTabooWords:          "禁忌词",
	DimRhythm:              "节奏",
	DimHumor:               "幽默",
	DimCertaintyTone:       "确定性语气",
	DimPOV:                 "POV 与视角处理",
	DimTense:               "时态",
	DimImageryDensity:      "意象密度",
	DimDialogueRatio:       "对话-叙述比",
}

// RenderLedger 把台账行渲染为纯文本块（§0.1 固定维度顺序，输入乱序不影响输出）。
// 空台账/无可渲染行返回空串（降级：provider 输出与 P0 逐字一致）。
func RenderLedger(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	byDim := make(map[string]Entry, len(entries))
	var unknown []string // 未知维度行（坏数据不丢行），按输入序排最后
	seenUnknown := make(map[string]bool)
	for _, e := range entries {
		content := strings.TrimSpace(e.Content)
		if content == "" {
			continue // 宁缺勿错：无内容行不渲染
		}
		if !Dimension(e.Dimension).Valid() {
			if !seenUnknown[e.Dimension] {
				seenUnknown[e.Dimension] = true
				unknown = append(unknown, "- "+e.Dimension+"："+content)
			}
			continue
		}
		if _, ok := byDim[e.Dimension]; !ok {
			byDim[e.Dimension] = e // 同维度重复取首现（与 mergeVoiceProfiles 口径一致）
		}
	}
	lines := make([]string, 0, len(byDim)+len(unknown))
	for _, d := range dimensions {
		e, ok := byDim[string(d)]
		if !ok {
			continue
		}
		label, ok := dimLabels[d]
		if !ok {
			label = string(d) // 兜底：labels 缺项时以枚举值为标签（不丢行）
		}
		lines = append(lines, "- "+label+"："+strings.TrimSpace(e.Content))
	}
	lines = append(lines, unknown...)
	if len(lines) == 0 {
		return ""
	}
	return LedgerHeader + "\n" + strings.Join(lines, "\n")
}
