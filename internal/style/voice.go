package style

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/voice"
)

// StatsVoiceProfile 把确定性文本统计映射为 Voice 十维的结构化条目（WBS §0.6/T2.2）：
//
//	AvgSentLen/SentLenStdDev/ShortSentPct → sentence_fingerprint（句式指纹）
//	QuoteDensity                          → dialogue_ratio（对话-叙述比）
//	逗/句/叹/问号密度                      → certainty_tone（确定性语气，辅助）
//
// 全部为确定性代码（非 LLM）：content 是统计事实的直陈（宁缺勿错，不猜测），
// metrics 键取 voice.MetricKey* 单源，evidence 锚点为参与统计的素材 sample_id。
func StatsVoiceProfile(samples []Sample, stats Stats) []voice.ProfileItem {
	ids := make([]int64, 0, len(samples))
	for _, s := range samples {
		ids = append(ids, s.ID)
	}
	ev := &voice.ProfileEvidence{SampleIDs: ids}
	return []voice.ProfileItem{
		{
			Dimension: string(voice.DimSentenceFingerprint),
			Content: fmt.Sprintf("确定性统计：均句长 %.1f 字（标准差 %.1f），短句（<15 字）占比 %.1f%%。",
				stats.AvgSentLen, stats.SentLenStdDev, stats.ShortSentPct),
			Metrics: map[string]float64{
				voice.MetricKeyAvgSentLen:    stats.AvgSentLen,
				voice.MetricKeySentLenStdDev: stats.SentLenStdDev,
				voice.MetricKeyShortSentPct:  stats.ShortSentPct,
			},
			Evidence: ev,
		},
		{
			Dimension: string(voice.DimDialogueRatio),
			Content: fmt.Sprintf("确定性统计：引号密度 %.2f%%（对话-叙述比的确定性代理指标）。",
				stats.QuoteDensity),
			Metrics: map[string]float64{
				voice.MetricKeyQuoteDensity: stats.QuoteDensity,
			},
			Evidence: ev,
		},
		{
			Dimension: string(voice.DimCertaintyTone),
			Content: fmt.Sprintf("确定性统计：句号密度 %.2f%%、感叹号 %.2f%%、问号 %.2f%%、逗号 %.2f%%（确定性语气辅助指标）。",
				stats.PeriodDensity, stats.ExclaimDensity, stats.QuestionDensity, stats.CommaDensity),
			Metrics: map[string]float64{
				voice.MetricKeyPeriodDensity:   stats.PeriodDensity,
				voice.MetricKeyExclaimDensity:  stats.ExclaimDensity,
				voice.MetricKeyQuestionDensity: stats.QuestionDensity,
				voice.MetricKeyCommaDensity:    stats.CommaDensity,
			},
			Evidence: ev,
		},
	}
}

// MergeVoiceProfile 合并确定性统计条目（statsItems）与 LLM 提炼条目（llmItems），
// WBS §0.6 口径：维度 content 取 LLM（有则覆盖确定性直陈），metrics 的确定性键
// 以 stats 为准（确定性测量 > LLM 估值），LLM 提供的额外键保留；evidence 取
// LLM（无则回落 stats）。statsItems 为空时原样返回 llmItems（反之亦然）。
func MergeVoiceProfile(statsItems, llmItems []voice.ProfileItem) []voice.ProfileItem {
	if len(statsItems) == 0 {
		return llmItems
	}
	if len(llmItems) == 0 {
		return statsItems
	}
	byDim := make(map[string]int, len(statsItems))
	out := make([]voice.ProfileItem, len(statsItems))
	copy(out, statsItems)
	for i, it := range out {
		byDim[it.Dimension] = i
	}
	for _, it := range llmItems {
		if i, ok := byDim[it.Dimension]; ok {
			merged := out[i]
			if it.Content != "" {
				merged.Content = it.Content // LLM 风格分析产出维度 content（§0.6）
			}
			metrics := make(map[string]float64, len(it.Metrics)+len(merged.Metrics))
			for k, v := range it.Metrics {
				metrics[k] = v
			}
			for k, v := range merged.Metrics { // 确定性键覆盖 LLM 估值
				metrics[k] = v
			}
			merged.Metrics = metrics
			if it.Evidence != nil {
				merged.Evidence = it.Evidence
			}
			out[i] = merged
			continue
		}
		byDim[it.Dimension] = len(out)
		out = append(out, it)
	}
	return out
}
