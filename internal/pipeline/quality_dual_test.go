package pipeline

// R2-4 里程碑档案汇总快审（双轨只读对照）测试：
//   - milestone 口径：档案存在 →「质量档案汇总」块进 L2 输入 + 并行快审记 verdict；
//   - 裁决仍由旧全量通读把持（快审只对照，不改状态）；
//   - 一致/差异记本地度量（只记 key）；无档案/非 milestone/回执坏/派发失败 → 降级零提示。

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/stretchr/testify/require"
)

// dualRecorder 双轨测试捕获桩：区分全量/快审派发（按提示词快审标记），抓双方提示词、
// 快审次数与本地度量 key。桩自带锁（快审与全量并行派发）。
type dualRecorder struct {
	mu          sync.Mutex
	fullPrompt  string
	quickPrompt string
	quickCalls  int
	quickText   string // 快审桩回执文本
	quickErr    error  // 非 nil 时快审派发失败
	keys        []string
}

func (r *dualRecorder) runner(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "signability-audit" && strings.Contains(input.Prompt, "质量档案汇总快审") {
		r.quickCalls++
		r.quickPrompt = input.Prompt
		if r.quickErr != nil {
			return SkillOutput{}, r.quickErr
		}
		return SkillOutput{Text: r.quickText}, nil
	}
	r.fullPrompt = input.Prompt
	return SkillOutput{Text: "结论：\n```json\n" +
		`{"verdict":"pass","score":31,"summary":"能签","issues":[]}` + "\n```"}, nil
}

// newDualTest 装配双轨测试：建表、可选档案行、注入捕获桩，返回 svc 与置为
// S4b running 的 runID（contextJSON 携带给定触发原因）。
func newDualTest(t *testing.T, novelID int64, reason string, withRows bool, rec *dualRecorder) (*Service, string) {
	t.Helper()
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	require.NoError(t, db.AutoMigrate(&quality.ChapterQuality{}))
	ctx := context.Background()
	if withRows {
		qs := quality.NewStore(db, slog.Default())
		require.NoError(t, qs.UpsertL0(ctx, &quality.ChapterQuality{
			NovelID: novelID, ChapterID: 11, ReadingNumber: 1,
			L0Metrics:  quality.MarshalL0Metrics(quality.L0Metrics{WordCount: 2100, HasTailHook: true}),
			L0Findings: quality.MarshalFindings(nil),
			L1Status:   quality.L1StatusChecked,
		}))
		require.NoError(t, qs.UpsertL0(ctx, &quality.ChapterQuality{
			NovelID: novelID, ChapterID: 12, ReadingNumber: 2,
			L0Metrics: quality.MarshalL0Metrics(quality.L0Metrics{WordCount: 1500}),
			L0Findings: quality.MarshalFindings([]quality.Finding{{ID: "C1", Check: quality.CheckWordCount,
				Level: quality.FindingLevelDefault, Desc: "字数偏短", Status: quality.FindingStatusPending}}),
			L1Status: quality.L1StatusPending,
		}))
	}
	svc.skillRunner = rec.runner
	svc.SetFeatureSink(func(_ context.Context, k string) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.keys = append(rec.keys, k)
	})
	ctxt := `{"audit_reason":"` + reason + `"}`
	runID := startS3Run(t, svc, novelID, ctxt)
	require.NoError(t, svc.store.SetStageStatus(ctx, runID, StageS4b, StatusRunning, ctxt))
	return svc, runID
}

const dualQuickPass = "快审结论：\n```json\n" + `{"verdict":"pass","summary":"整体可控"}` + "\n```"

// TestMilestoneAuditLedgerBlockAndQuickAgree 主路径：milestone + 有档案 →
// 汇总块进全量输入、并行快审跑一次、verdict 一致记 agree。
func TestMilestoneAuditLedgerBlockAndQuickAgree(t *testing.T) {
	rec := &dualRecorder{quickText: dualQuickPass}
	svc, runID := newDualTest(t, 801, "milestone", true, rec)
	svc.executeAudit(runID, 801)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	// 全量体检输入追加「质量档案汇总」块（章节行 + 发现投影）。
	require.Contains(t, rec.fullPrompt, "【质量档案汇总】")
	require.Contains(t, rec.fullPrompt, "- #1｜2100 字｜章尾钩子:有")
	require.Contains(t, rec.fullPrompt, "  - C1[word_count][pending] 字数偏短")
	// 快审轨恰跑一次，以档案汇总为唯一输入，并引用回执契约格式串（单源）。
	require.Equal(t, 1, rec.quickCalls)
	require.Contains(t, rec.quickPrompt, "质量档案汇总快审")
	require.Contains(t, rec.quickPrompt, "【质量档案汇总】")
	require.Contains(t, rec.quickPrompt, quickAuditFormatLine())
	// verdict 一致 → 只记 agree 一条（只记 key+次数，不记内容）。
	require.Equal(t, []string{featureQuickAuditAgree}, rec.keys)
}

// TestMilestoneQuickAuditDiffRecorded 快审与全量裁决不一致 → 记 diff。
func TestMilestoneQuickAuditDiffRecorded(t *testing.T) {
	rec := &dualRecorder{quickText: "快审结论：\n```json\n" +
		`{"verdict":"needs_fix","summary":"风险偏高"}` + "\n```"}
	svc, runID := newDualTest(t, 802, "milestone", true, rec)
	svc.executeAudit(runID, 802)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Equal(t, []string{featureQuickAuditDiff}, rec.keys)
}

// TestQuickAuditBadReceiptSilent 回执坏 → 静默不记，主流程零影响（降级零提示）。
func TestQuickAuditBadReceiptSilent(t *testing.T) {
	rec := &dualRecorder{quickText: "没有回执块，也没给结论"}
	svc, runID := newDualTest(t, 803, "milestone", true, rec)
	svc.executeAudit(runID, 803)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Equal(t, 1, rec.quickCalls, "快审照常跑")
	require.Empty(t, rec.keys, "回执坏应静默不记")
	run, err := svc.store.GetByRunID(context.Background(), runID)
	require.NoError(t, err)
	require.Equal(t, StatusAwaitingGate, run.Status, "快审失败不得影响全量体检落闸")
}

// TestQuickAuditDispatchFailSilent 快审派发失败 → 静默不记，全量裁决照常。
func TestQuickAuditDispatchFailSilent(t *testing.T) {
	rec := &dualRecorder{quickErr: errors.New("provider unavailable")}
	svc, runID := newDualTest(t, 804, "milestone", true, rec)
	svc.executeAudit(runID, 804)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Equal(t, 1, rec.quickCalls)
	require.Empty(t, rec.keys)
	run, err := svc.store.GetByRunID(context.Background(), runID)
	require.NoError(t, err)
	require.Equal(t, StatusAwaitingGate, run.Status)
}

// TestNoLedgerDegradesZeroPrompt 无档案 → 全量提示词与无该块逐字一致、快审不跑、零度量。
func TestNoLedgerDegradesZeroPrompt(t *testing.T) {
	rec := &dualRecorder{quickText: dualQuickPass}
	svc, runID := newDualTest(t, 805, "milestone", false, rec)
	svc.executeAudit(runID, 805)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	// 逐字一致：与空块构造器输出（即改造前文案）完全相同。
	expected := signabilityPrompt(805, "", "", contextasm.ContextBlocks{})
	require.Equal(t, expected, rec.fullPrompt, "无档案时提示词应与无该块逐字一致")
	require.Equal(t, 0, rec.quickCalls, "无档案不得起快审轨")
	require.Empty(t, rec.keys)
}

// TestNonMilestoneSkipsQuickAudit 非 milestone 口径（golden3/volume）：有档案也不
// 注入汇总块、不起快审（milestone 口径专轨）。
func TestNonMilestoneSkipsQuickAudit(t *testing.T) {
	for _, reason := range []string{"golden3", "volume", "final"} {
		rec := &dualRecorder{quickText: dualQuickPass}
		svc, runID := newDualTest(t, 806, reason, true, rec)
		svc.executeAudit(runID, 806)

		rec.mu.Lock()
		require.NotContains(t, rec.fullPrompt, "【质量档案汇总】", reason+" 不应注入汇总块")
		require.Equal(t, 0, rec.quickCalls, reason+" 不应起快审轨")
		require.Empty(t, rec.keys)
		rec.mu.Unlock()
	}
}

// TestParseQuickAudit 快审回执解析：合法双 verdict / 无块 / 坏 JSON / 非法 verdict。
func TestParseQuickAudit(t *testing.T) {
	got := parseQuickAudit("结论\n```json\n{\"verdict\":\"pass\",\"summary\":\"ok\"}\n```")
	require.True(t, got.ok)
	require.Equal(t, "pass", got.verdict)

	got = parseQuickAudit("结论\n```json\n{\"verdict\":\"needs_fix\",\"summary\":\"r\"}\n```")
	require.True(t, got.ok)
	require.Equal(t, "needs_fix", got.verdict)

	require.False(t, parseQuickAudit("没有回执块").ok)
	require.False(t, parseQuickAudit("```json\n{坏 json}\n```").ok)
	require.False(t, parseQuickAudit("```json\n{\"verdict\":\"maybe\"}\n```").ok, "非法 verdict 不猜结论")
}

// TestLedgerQuickAuditPromptContract 快审提示词引用回执契约格式串（契约单源锁定）
// 且以质量档案汇总块为输入。
func TestLedgerQuickAuditPromptContract(t *testing.T) {
	p := ledgerQuickAuditPrompt(9, "【质量档案汇总】…块…")
	require.Contains(t, p, quickAuditFormatLine(), "快审提示词应引用契约格式串（单源）")
	require.Contains(t, p, "【质量档案汇总】…块…")
	require.Contains(t, p, "novel_id=9")
}
