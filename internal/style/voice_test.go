package style

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/voice"
)

// TestStatsVoiceProfile 锁死确定性映射（WBS §0.6/T2.2）：
// 指标键取 voice.MetricKey* 单源；content 为统计事实直陈；evidence 锚点=素材 id。
func TestStatsVoiceProfile(t *testing.T) {
	samples := []Sample{{ID: 11, Content: "甲。"}, {ID: 22, Content: "乙！"}}
	stats := Stats{
		AvgSentLen: 8.7, SentLenStdDev: 3.2, ShortSentPct: 80,
		QuoteDensity: 1.5,
		CommaDensity: 2, PeriodDensity: 3, ExclaimDensity: 0.5, QuestionDensity: 0.1,
	}
	items := StatsVoiceProfile(samples, stats)
	require.Len(t, items, 3)

	byDim := map[string]voice.ProfileItem{}
	for _, it := range items {
		byDim[it.Dimension] = it
	}

	fp := byDim[string(voice.DimSentenceFingerprint)]
	assert.Contains(t, fp.Content, "8.7", "确定性 content 应含统计数值")
	assert.InDelta(t, 8.7, fp.Metrics[voice.MetricKeyAvgSentLen], 1e-9)
	assert.InDelta(t, 3.2, fp.Metrics[voice.MetricKeySentLenStdDev], 1e-9)
	assert.InDelta(t, 80.0, fp.Metrics[voice.MetricKeyShortSentPct], 1e-9)

	dr := byDim[string(voice.DimDialogueRatio)]
	assert.InDelta(t, 1.5, dr.Metrics[voice.MetricKeyQuoteDensity], 1e-9)

	ct := byDim[string(voice.DimCertaintyTone)]
	assert.InDelta(t, 3.0, ct.Metrics[voice.MetricKeyPeriodDensity], 1e-9)
	assert.InDelta(t, 2.0, ct.Metrics[voice.MetricKeyCommaDensity], 1e-9)

	for _, it := range items {
		require.NotNil(t, it.Evidence)
		assert.Equal(t, []int64{11, 22}, it.Evidence.SampleIDs)
	}
}

// TestStatsMetrics 锁死八键装配（P2 漂移 current 值来源，键与 StatsVoiceProfile 同源）。
func TestStatsMetrics(t *testing.T) {
	stats := Stats{
		AvgSentLen: 8.7, SentLenStdDev: 3.2, ShortSentPct: 80,
		QuoteDensity: 1.5,
		CommaDensity: 2, PeriodDensity: 3, ExclaimDensity: 0.5, QuestionDensity: 0.1,
	}
	got := StatsMetrics(stats)
	require.Len(t, got, 8)
	assert.InDelta(t, 8.7, got[voice.MetricKeyAvgSentLen], 1e-9)
	assert.InDelta(t, 3.2, got[voice.MetricKeySentLenStdDev], 1e-9)
	assert.InDelta(t, 80.0, got[voice.MetricKeyShortSentPct], 1e-9)
	assert.InDelta(t, 1.5, got[voice.MetricKeyQuoteDensity], 1e-9)
	assert.InDelta(t, 2.0, got[voice.MetricKeyCommaDensity], 1e-9)
	assert.InDelta(t, 3.0, got[voice.MetricKeyPeriodDensity], 1e-9)
	assert.InDelta(t, 0.5, got[voice.MetricKeyExclaimDensity], 1e-9)
	assert.InDelta(t, 0.1, got[voice.MetricKeyQuestionDensity], 1e-9)
}

func TestMergeVoiceProfile(t *testing.T) {
	assert.Nil(t, MergeVoiceProfile(nil, nil))

	statsItems := []voice.ProfileItem{{
		Dimension: string(voice.DimSentenceFingerprint),
		Content:   "确定性统计：均句长 8.7 字。",
		Metrics:   map[string]float64{voice.MetricKeyAvgSentLen: 8.7},
		Evidence:  &voice.ProfileEvidence{SampleIDs: []int64{1, 2}},
	}}
	llmItems := []voice.ProfileItem{
		{
			Dimension: string(voice.DimSentenceFingerprint),
			Content:   "短句为主，刀切斧凿。",
			Metrics:   map[string]float64{voice.MetricKeyAvgSentLen: 99, "extra_key": 1},
			Evidence:  &voice.ProfileEvidence{SampleIDs: []int64{7, 8}},
		},
		{Dimension: string(voice.DimHumor), Content: "冷幽默。"},
	}

	got := MergeVoiceProfile(statsItems, llmItems)
	require.Len(t, got, 2)

	fp := got[0]
	assert.Equal(t, "短句为主，刀切斧凿。", fp.Content, "content 取 LLM（§0.6）")
	assert.InDelta(t, 8.7, fp.Metrics[voice.MetricKeyAvgSentLen], 1e-9, "确定性键以 stats 为准")
	assert.InDelta(t, 1.0, fp.Metrics["extra_key"], 1e-9, "LLM 额外键保留")
	assert.Equal(t, []int64{7, 8}, fp.Evidence.SampleIDs, "evidence 取 LLM")

	assert.Equal(t, string(voice.DimHumor), got[1].Dimension, "LLM 新维度追加")

	// 单边为空时原样返回。
	assert.Equal(t, llmItems, MergeVoiceProfile(nil, llmItems))
	assert.Equal(t, statsItems, MergeVoiceProfile(statsItems, nil))
}

// TestExtract_VoiceProfileDualWrite 端到端（mock LLM）：素材提取成功后
// 经 sink 并行落库结构化维度（Stats 确定性 metrics + LLM voice_profile 合并），
// 风格画像产出（Name/Description/Content）一字不动。
func TestExtract_VoiceProfileDualWrite(t *testing.T) {
	out := StyleSkillOutput{
		Name:        "刀切斧凿",
		Description: "短句硬派",
		Content:     "# 刀切斧凿\n## 风格概述\n硬。",
		VoiceProfile: []voice.ProfileItem{
			{Dimension: string(voice.DimSentenceFingerprint), Content: "短句为主，刀切斧凿。",
				Metrics: map[string]float64{voice.MetricKeyAvgSentLen: 99}},
			{Dimension: string(voice.DimHumor), Content: "冷幽默。"},
		},
	}
	argsJSON, _ := json.Marshal(out)

	samples := []Sample{{ID: 11, Content: "他走了。"}, {ID: 22, Content: "风停了！"}} // 2 个锚点
	var gotItems []voice.ProfileItem
	var sinkCalls int
	res, err := Extract(context.Background(), newStyleMockLLM(t, string(argsJSON)), samples,
		"mock", "test-model", "",
		func(items []voice.ProfileItem) {
			sinkCalls++
			gotItems = items
		})
	require.NoError(t, err)

	// 风格画像产出路径一字不动。
	assert.Equal(t, "刀切斧凿", res.Name)
	assert.Equal(t, "短句硬派", res.Description)
	assert.Equal(t, "# 刀切斧凿\n## 风格概述\n硬。", res.Content)

	require.Equal(t, 1, sinkCalls)
	byDim := map[string]voice.ProfileItem{}
	for _, it := range gotItems {
		byDim[it.Dimension] = it
	}
	require.Len(t, byDim, 4, "3 个 Stats 维 + LLM 新增 humor")

	fp := byDim[string(voice.DimSentenceFingerprint)]
	assert.Equal(t, "短句为主，刀切斧凿。", fp.Content, "content 取 LLM")
	wantStats := ComputeStats(samples)
	assert.InDelta(t, wantStats.AvgSentLen, fp.Metrics[voice.MetricKeyAvgSentLen], 1e-9,
		"确定性键以实时 Stats 为准（覆盖 LLM 估值 99）")
	require.NotNil(t, fp.Evidence)
	assert.Equal(t, []int64{11, 22}, fp.Evidence.SampleIDs, "LLM 无 evidence → 回落 Stats 锚点")

	// LLM 未产出 voice_profile → sink 仍有 Stats 确定性条目（含 metrics 数值）。
	plain := StyleSkillOutput{Name: "n", Description: "d", Content: "c"}
	plainJSON, _ := json.Marshal(plain)
	var plainItems []voice.ProfileItem
	_, err = Extract(context.Background(), newStyleMockLLM(t, string(plainJSON)), samples,
		"mock", "test-model", "",
		func(items []voice.ProfileItem) { plainItems = items })
	require.NoError(t, err)
	require.Len(t, plainItems, 3)
	assert.NotEmpty(t, plainItems[0].Metrics, "Stats 确定性 metrics 数值照常落库")
}

// newStyleMockLLM 构造返回固定 output_style_skill 工具调用的 mock llm.Client。
func newStyleMockLLM(t *testing.T, argsJSON string) *llm.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		var req struct {
			Tools []map[string]any `json:"tools"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		if len(req.Tools) > 0 {
			for _, chunk := range []map[string]any{
				{"choices": []map[string]any{{"delta": map[string]any{"tool_calls": []map[string]any{{
					"index": 0, "id": "call_mock",
					"function": map[string]any{"name": "output_style_skill"},
				}}}}}},
				{"choices": []map[string]any{{"delta": map[string]any{"tool_calls": []map[string]any{{
					"index": 0, "function": map[string]any{"arguments": argsJSON},
				}}}}}},
			} {
				b, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", b)
				flusher.Flush()
			}
		} else {
			b, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": map[string]any{"content": ""}}},
			})
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(server.Close)
	providers := map[string]llm.Provider{
		"mock": {
			Name:    "mock",
			ChatURL: server.URL,
			APIKey:  "test-key",
			Models:  []llm.ModelInfo{{ID: "test-model", ContextWindow: 128_000}},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return llm.NewClient(providers, logger)
}
