package pattern

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/novelgo/novelgo/internal/voice"
)

// TestFinalMentalSkill_VoiceProfileDualWrite 验证 P1 双写（WBS §0.3）：
// 带 voice_profile 的提取产出结构化维度，且 skill md 与不带时逐字一致
// （skill md 产出路径一字不动）；LLM 未产出 voice_profile → 产出为空（降级不落库）。
func TestFinalMentalSkill_VoiceProfileDualWrite(t *testing.T) {
	const content = "# X 的创作心智\n## 叙事心智模型\n冲突通过信息 withholding 生成。\n## 表达 DNA\n短句为主。"
	withProfile := MentalSkillOutput{
		Name:        "X 的创作心智",
		Description: "用 X 的叙事习惯写新章节时",
		Content:     content,
		VoiceProfile: []voice.ProfileItem{
			{Dimension: string(voice.DimSentenceFingerprint), Content: "均句长 8.7 字，短句为主",
				Metrics:  map[string]float64{"avg_sent_len": 8.7},
				Evidence: &voice.ProfileEvidence{ChapterIDs: []int64{3, 5}}},
			{Dimension: string(voice.DimDialogueRatio), Content: "对话稀疏，叙述为主"},
		},
	}
	withoutProfile := MentalSkillOutput{
		Name:        "X 的创作心智",
		Description: "用 X 的叙事习惯写新章节时",
		Content:     content,
	}
	withJSON, _ := json.Marshal(withProfile)
	withoutJSON, _ := json.Marshal(withoutProfile)

	chunks := []Chunk{
		{Name: "崛起", StartChapterID: 1, EndChapterID: 5, StartReadingNumber: 1, EndReadingNumber: 5, Content: "主角觉醒"},
	}
	input := defaultInput()
	input.Flavor = string(FlavorMental)

	outWith, err := testExtractor(mockToolHandler("output_skill", string(withJSON), "")).
		finalSkill(context.Background(), input, chunks, 30)
	if err != nil {
		t.Fatalf("finalSkill(with profile): %v", err)
	}
	outWithout, err := testExtractor(mockToolHandler("output_skill", string(withoutJSON), "")).
		finalSkill(context.Background(), input, chunks, 30)
	if err != nil {
		t.Fatalf("finalSkill(without profile): %v", err)
	}

	// skill md 与改造前逐字一致：带不带 voice_profile，md 必须一字不差。
	if outWith.Raw != outWithout.Raw {
		t.Errorf("skill md 必须与未产出 voice_profile 时逐字一致\nwith:\n%s\nwithout:\n%s", outWith.Raw, outWithout.Raw)
	}

	if len(outWith.VoiceProfile) != 2 {
		t.Fatalf("voice_profile 透传 = %d 维, want 2: %+v", len(outWith.VoiceProfile), outWith.VoiceProfile)
	}
	if outWith.VoiceProfile[0].Dimension != string(voice.DimSentenceFingerprint) ||
		outWith.VoiceProfile[1].Dimension != string(voice.DimDialogueRatio) {
		t.Errorf("voice_profile 维度错序: %+v", outWith.VoiceProfile)
	}
	if len(outWithout.VoiceProfile) != 0 {
		t.Errorf("LLM 未产出 voice_profile 应为空（降级不落库）: %+v", outWithout.VoiceProfile)
	}
}

// TestRunFinal_VoiceSink 验证 runFinal 把透传的 voice_profile 交由 VoiceSink
// 并行落库（落库接线点）；skill md 产物照常返回。
func TestRunFinal_VoiceSink(t *testing.T) {
	out := MentalSkillOutput{
		Name:        "X 的创作心智",
		Description: "用 X 的叙事习惯写新章节时",
		Content:     "# X 的创作心智\n## 叙事心智模型\n冲突通过 withholding 生成。",
		VoiceProfile: []voice.ProfileItem{
			{Dimension: string(voice.DimPOV), Content: "第三人称限知"},
		},
	}
	argsJSON, _ := json.Marshal(out)

	var gotItems []voice.ProfileItem
	var gotNovelID int64
	var sinkCalls int
	e := testExtractor(mockToolHandler("output_skill", string(argsJSON), ""))
	e.VoiceSink = func(novelID int64, items []voice.ProfileItem) {
		sinkCalls++
		gotNovelID = novelID
		gotItems = items
	}
	pr := &prefixResult{
		chunks: []Chunk{{Name: "全篇", StartChapterID: 1, EndChapterID: 5, Content: "主角觉醒"}},
		trace:  &Trace{FinalTokens: 10},
	}
	input := defaultInput()
	input.Flavor = string(FlavorMental)

	result, err := e.runFinal(context.Background(), input, pr, string(FlavorMental))
	if err != nil {
		t.Fatalf("runFinal: %v", err)
	}
	if sinkCalls != 1 {
		t.Fatalf("VoiceSink 调用次数 = %d, want 1", sinkCalls)
	}
	if gotNovelID != input.NovelID {
		t.Errorf("sink novelID = %d, want %d", gotNovelID, input.NovelID)
	}
	if len(gotItems) != 1 || gotItems[0].Dimension != string(voice.DimPOV) {
		t.Errorf("sink items 错误: %+v", gotItems)
	}
	if result.RawContent == "" {
		t.Error("skill md 产物应照常返回")
	}

	// LLM 未产出 → sink 不触发。
	without := MentalSkillOutput{Name: "X 的创作心智", Description: "d", Content: "# c"}
	withoutJSON, _ := json.Marshal(without)
	var withoutCalls int
	e2 := testExtractor(mockToolHandler("output_skill", string(withoutJSON), ""))
	e2.VoiceSink = func(int64, []voice.ProfileItem) { withoutCalls++ }
	if _, err := e2.runFinal(context.Background(), input, pr, string(FlavorMental)); err != nil {
		t.Fatalf("runFinal(without profile): %v", err)
	}
	if withoutCalls != 0 {
		t.Errorf("LLM 未产出 voice_profile 不应触发 VoiceSink，got %d", withoutCalls)
	}
}

// TestFinalMentalMultiLens_VoiceProfileMerge 验证多镜头 voice_profile 按镜头顺序
// 首次出现优先、同维度去重合并（后续由 voice.Store.Upsert 幂等兜底）。
func TestFinalMentalMultiLens_VoiceProfileMerge(t *testing.T) {
	mentalJSON := `{"name":"X 的创作心智","description":"用 X 的习惯","content":"## 叙事心智模型\n信息 withholding。","voice_profile":[{"dimension":"rhythm","content":"先结论后铺垫"}]}`
	exprJSON := `{"content":"## 表达 DNA\n短句为主。","voice_profile":[{"dimension":"rhythm","content":"应被去重（expr 镜头同维度）"},{"dimension":"sentence_fingerprint","content":"短句指纹"}]}`
	decisionJSON := `{"content":"## 创作决策启发式\n优先揭示。\n## 价值观与反模式\n克制。\n## 诚实边界\n虚构推断。"}`
	fidelityJSON, _ := json.Marshal(FidelityScorecard{KnownConsistency: 5, UnknownUncertainty: 4, Passed: true})

	e := testExtractor(sequenceToolHandler([3]string{mentalJSON, exprJSON, decisionJSON}, string(fidelityJSON)))
	input := defaultInput()
	input.Flavor = string(FlavorMental)
	input.DeepMultiLens = true
	chunks := []Chunk{
		{Name: "崛起", StartChapterID: 1, EndChapterID: 5, StartReadingNumber: 1, EndReadingNumber: 5, Content: "主角觉醒"},
	}
	outcome, err := e.finalMentalSkill(context.Background(), input, chunks, 30)
	if err != nil {
		t.Fatalf("finalMentalMultiLens: %v", err)
	}

	gotItems := outcome.VoiceProfile
	if len(gotItems) != 2 {
		t.Fatalf("merged items = %d, want 2（同维度去重）: %+v", len(gotItems), gotItems)
	}
	if gotItems[0].Dimension != string(voice.DimRhythm) || gotItems[0].Content != "先结论后铺垫" {
		t.Errorf("同维度应首次出现优先（mental 镜头）: %+v", gotItems[0])
	}
	if gotItems[1].Dimension != string(voice.DimSentenceFingerprint) {
		t.Errorf("第二维应为 sentence_fingerprint: %+v", gotItems[1])
	}
}

func TestMergeVoiceProfiles(t *testing.T) {
	if got := mergeVoiceProfiles(nil, nil); got != nil {
		t.Errorf("全空应返回 nil, got %+v", got)
	}
	a := []voice.ProfileItem{{Dimension: "pov", Content: "1"}}
	b := []voice.ProfileItem{{Dimension: "pov", Content: "2"}, {Dimension: "tense", Content: "3"}}
	got := mergeVoiceProfiles(a, b)
	if len(got) != 2 || got[0].Content != "1" || got[1].Dimension != "tense" {
		t.Errorf("mergeVoiceProfiles 去重/顺序错误: %+v", got)
	}
}
