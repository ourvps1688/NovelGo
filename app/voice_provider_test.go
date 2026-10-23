package app

// P1 WBS T3.2 验收 golden：voice provider 升级（§0.4 消费端契约）。
// ①台账空 → provider 输出与 P0 逐字一致；②台账非空 → P0 输出为前缀一字不动
// + 台账块按序追加；③维度乱序落库 → 渲染按 §0.1 固定顺序。
// 「P0 行为」的 golden 基准 = agentcfg.BuildPersonaMindText 原路径输出
// （handler.go 原闭包体，一字不动迁入 personaMindText 后以此对照）。

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/agentcfg"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/personasetting"
	"github.com/novelgo/novelgo/internal/voice"
)

// enableTestPersona 写入一个 mental 技能并启用本书 persona（skill 引用）。
func enableTestPersona(t *testing.T, a *App, novelID int64) {
	t.Helper()
	writeUserSkillFile(t, "X 的创作心智", "mental", "刘慈欣", "X_MENTAL_BODY", false)
	require.NoError(t, a.skill.ReloadUser(config.UserSkillsDir()))
	require.NoError(t, personasetting.NewStore(a.db, a.logger).Save(context.Background(),
		&personasetting.PersonaSetting{NovelID: novelID, Enabled: true, Kind: "skill", Ref: "X 的创作心智"}))
}

// p0MindGolden 是 P0 心智正文的逐字基准（技能正文 + 诚实边界声明，BuildPersonaMindText 口径）。
const p0MindGolden = "X_MENTAL_BODY\n\n" + agentcfg.PersonaHonestyNote

// TestVoiceText_EmptyLedger_MatchesP0Verbatim ①台账空 → 与 P0 逐字一致：
// persona 启用时为纯心智正文（连前缀/分隔符/块头都不得多一个字），persona 未启用时为空串。
func TestVoiceText_EmptyLedger_MatchesP0Verbatim(t *testing.T) {
	a := setupTestApp(t)
	n := createTestNovel(t, a)
	ctx := context.Background()

	// persona 未启用：P0 行为 = 空串，台账空时必须仍是空串。
	got, err := a.voiceText(ctx, n.ID)
	require.NoError(t, err)
	assert.Equal(t, "", got, "台账空 + persona 未启用 → 空串（与 P0 逐字一致）")

	// persona 启用：台账空 → 输出与 P0 心智正文逐字一致（golden 锁定）。
	enableTestPersona(t, a, n.ID)
	got, err = a.voiceText(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, p0MindGolden, got, "台账空 → provider 输出与 P0 逐字一致")
}

// TestVoiceText_LedgerAppendedAfterMind ②台账非空 → P0 输出为前缀一字不动 + 台账块按序追加。
func TestVoiceText_LedgerAppendedAfterMind(t *testing.T) {
	a := setupTestApp(t)
	n := createTestNovel(t, a)
	ctx := context.Background()
	enableTestPersona(t, a, n.ID)

	a.upsertVoiceProfileSource(n.ID, voice.SourcePatternMental, []voice.ProfileItem{
		{Dimension: string(voice.DimRhythm), Content: "先结论后铺垫"},
		{Dimension: string(voice.DimPOV), Content: "第三人称限知"},
	})

	got, err := a.voiceText(ctx, n.ID)
	require.NoError(t, err)

	// P0 前缀一字不动。
	require.True(t, strings.HasPrefix(got, p0MindGolden), "P0 输出必须是逐字前缀")
	require.Equal(t, "\n\n", got[len(p0MindGolden):len(p0MindGolden)+2], "前缀后恰一个空行分隔")

	// 台账块按 §0.1 序追加（golden 全文锁定）。
	want := p0MindGolden + "\n\n" + strings.Join([]string{
		voice.LedgerHeader,
		"- 节奏：先结论后铺垫",
		"- POV 与视角处理：第三人称限知",
	}, "\n")
	require.Equal(t, want, got, "台账非空 → P0 前缀一字不动 + 台账块按序追加")
}

// TestVoiceText_LedgerOnly_WhenNoPersona P0 输出为空（persona 未启用）时，
// 台账非空 → 台账块单独输出（空前缀仍是前缀），Voice 块照常携带台账。
func TestVoiceText_LedgerOnly_WhenNoPersona(t *testing.T) {
	a := setupTestApp(t)
	n := createTestNovel(t, a)
	ctx := context.Background()

	a.upsertVoiceProfileSource(n.ID, voice.SourceStyleSample, []voice.ProfileItem{
		{Dimension: string(voice.DimTense), Content: "过去时为主"},
	})

	got, err := a.voiceText(ctx, n.ID)
	require.NoError(t, err)
	want := strings.Join([]string{voice.LedgerHeader, "- 时态：过去时为主"}, "\n")
	require.Equal(t, want, got, "P0 前缀为空时台账块单独输出，不得带前导空行")
	assert.False(t, strings.HasPrefix(got, "\n"), "不得有多余前导空白")
}

// TestVoiceText_ScrambledInsert_FixedOrder ③维度乱序落库 → 渲染按 §0.1 固定顺序。
func TestVoiceText_ScrambledInsert_FixedOrder(t *testing.T) {
	a := setupTestApp(t)
	n := createTestNovel(t, a)
	ctx := context.Background()
	enableTestPersona(t, a, n.ID)

	// 故意乱序写入（尾部维度在前、中部维度殿后）。
	a.upsertVoiceProfileSource(n.ID, voice.SourceManual, []voice.ProfileItem{
		{Dimension: string(voice.DimDialogueRatio), Content: "对话多"},
		{Dimension: string(voice.DimSentenceFingerprint), Content: "短句多"},
		{Dimension: string(voice.DimCertaintyTone), Content: "留余地"},
		{Dimension: string(voice.DimHighFreqWords), Content: "高频词表"},
	})

	got, err := a.voiceText(ctx, n.ID)
	require.NoError(t, err)

	ledger := strings.TrimPrefix(got, p0MindGolden+"\n\n")
	want := strings.Join([]string{
		voice.LedgerHeader,
		"- 句式指纹：短句多",
		"- 高频词：高频词表",
		"- 确定性语气：留余地",
		"- 对话-叙述比：对话多",
	}, "\n")
	require.Equal(t, want, ledger, "乱序落库仍按 §0.1 固定顺序渲染")
}

// TestVoiceText_LedgerQueryFailure_DegradesToP0 台账读取失败 → 只放弃台账块，
// 心智正文（P0 前缀）必须保住（坏数据宁缺勿错，不拖垮整块）。
func TestVoiceText_LedgerQueryFailure_DegradesToP0(t *testing.T) {
	a := setupTestApp(t)
	n := createTestNovel(t, a)
	ctx := context.Background()
	enableTestPersona(t, a, n.ID)

	a.upsertVoiceProfileSource(n.ID, voice.SourceManual, []voice.ProfileItem{
		{Dimension: string(voice.DimHumor), Content: "冷幽默"},
	})
	// 制造台账表不可读（rename 后 List 必失败）。
	require.NoError(t, a.db.Exec("ALTER TABLE voice_dimensions RENAME TO voice_dimensions_broken").Error)

	got, err := a.voiceText(ctx, n.ID)
	require.NoError(t, err, "台账失败不得上抛拖垮 provider")
	require.Equal(t, p0MindGolden, got, "台账读取失败 → 降级为 P0 逐字输出")
}
