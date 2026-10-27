package pipeline

// R2b-9 §9.4 告警→真问题命中率度量测试：raised/confirmed/dismissed 挂点记数
// （曝光口径）+ IssueHitStats 校准取数聚合。纯本地度量（只记 key+次数）。

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
)

// sinkKeys 是 featureSink 捕获小桩（自带锁）。
type sinkKeys struct {
	mu   sync.Mutex
	keys []string
}

func (s *sinkKeys) record(_ context.Context, k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = append(s.keys, k)
}

func (s *sinkKeys) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

// TestRecordFeatureUsageN 条数语义：n 次记数，n<=0 不记。
func TestRecordFeatureUsageN(t *testing.T) {
	svc := NewService(newTestDB(t), nil, nil, nil, "", "", context.Background(), slog.Default())
	sk := &sinkKeys{}
	svc.SetFeatureSink(sk.record)
	svc.recordFeatureUsageN(context.Background(), featureAlertRaisedL2, 3)
	svc.recordFeatureUsageN(context.Background(), featureAlertRaisedL2, 0)
	svc.recordFeatureUsageN(context.Background(), featureAlertRaisedL2, -2)
	require.Equal(t, []string{featureAlertRaisedL2, featureAlertRaisedL2, featureAlertRaisedL2}, sk.snapshot())
}

// TestAlertMetricsRaisedL2OnAudit L2 告警产生（曝光口径）：体检 issue 落表成功即
// 按条数记 raised.l2；零 issue 不记。
func TestAlertMetricsRaisedL2OnAudit(t *testing.T) {
	auditWith2 := "结论：\n```json\n" +
		`{"verdict":"needs_fix","score":20,"summary":"两条问题","schema_version":3,"issues":[` +
		`{"id":"I1","level":"P1","area":"长线","desc":"d1","fix_type":"ledger","target_scope":"active_volume","dimension":"timeline","direction":"forward_fix","chapter_ids":[]},` +
		`{"id":"I2","level":"P1","area":"节奏","desc":"d2","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[]}]}` +
		"\n```"
	rec := &dualRecorder{fullText: auditWith2}
	svc, runID := newDualTest(t, 820, "milestone", false, rec)
	require.NoError(t, svc.store.db.AutoMigrate(&PipelineIssue{}))
	svc.executeAudit(runID, 820)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Equal(t, []string{featureAlertRaisedL2, featureAlertRaisedL2}, rec.keys,
		"2 条 issue → raised.l2 记 2 次（曝光口径）")
}

// TestAlertMetricsFixReceiptFlow confirmed/dismissed：修复回执 fixed 逐条记确认、
// skipped 逐条记未确认。
func TestAlertMetricsFixReceiptFlow(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	sk := &sinkKeys{}
	svc.SetFeatureSink(sk.record)
	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"两条待修","schema_version":3,"issues":[` +
		`{"id":"I1","level":"P0","area":"a","desc":"d1","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[1]},` +
		`{"id":"I2","level":"P1","area":"b","desc":"d2","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[5]}]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			return SkillOutput{Text: "已修复：\n```json\n" +
				`{"fixed":["I1"],"skipped":[{"id":"I2","reason":"查无此问题"}]}` + "\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}
	ctx := context.Background()

	// R5-2：S5 不再自动派发修复——路由为待修工单后由显式「去修复」触发，修复回执度量不变。
	runID := startS5Run(t, svc, 903, auditJSON)
	svc.executeFix(runID, 903) // I1/I2 路由为待修工单
	require.NoError(t, svc.StartIssueFixTickets(ctx, 903, []IssueTicketRef{
		{RunID: runID, IssueKey: "I1"}, {RunID: runID, IssueKey: "I2"},
	}))

	keys := sk.snapshot()
	require.Contains(t, keys, featureAlertConfirmedL2, "fixed 1 条 → confirmed 记 1 次")
	require.Contains(t, keys, featureAlertDismissedL2, "skipped 1 条 → dismissed 记 1 次")
	require.Equal(t, 1, countKey(keys, featureAlertConfirmedL2))
	require.Equal(t, 1, countKey(keys, featureAlertDismissedL2))
}

// TestAlertMetricsRaisedStructOnStructureBlock 结构告警产生（曝光口径）：
// 告警块非空按条数记 raised.struct；零告警不记。
func TestAlertMetricsRaisedStructOnStructureBlock(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	sk := &sinkKeys{}
	svc.SetFeatureSink(sk.record)
	seedForeshadowData(t, db, 821)

	block := svc.structureBlock(context.Background(), 821)
	require.Contains(t, block, "【结构告警】")
	require.Len(t, sk.snapshot(), 3, "seed 3 条伏笔告警 → raised.struct 记 3 次")

	// 零告警：干净伏笔 → 不记。
	sk2 := &sinkKeys{}
	svc.SetFeatureSink(sk2.record)
	require.Empty(t, svc.structureBlock(context.Background(), 9999))
	require.Empty(t, sk2.snapshot(), "零告警不记 raised（宁缺勿错）")
}

// TestAlertMetricsRaisedL1OnL0Sweep L0 规则发现落档（曝光口径）：发现数记 raised.l1，
// 零告警不记。
func TestAlertMetricsRaisedL1OnL0Sweep(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &quality.ChapterQuality{}, &volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	sk := &sinkKeys{}
	svc.SetFeatureSink(sk.record)
	novelID := int64(947)
	ids := seedPropChapters(t, db, novelID, 2)
	svc.SetChapterTextReader(func(_ context.Context, _ int64, chapterID int64) (string, error) {
		if chapterID == ids[0] {
			return "", nil // 空章 → empty_chapter 发现
		}
		return strings.Repeat("他望着远处的山，心中忽而一动。", 200) + "？", nil
	})

	seg := svc.l0RuleSweep(ctx, novelID)
	require.Contains(t, seg, "规则告警")
	require.GreaterOrEqual(t, len(sk.snapshot()), 1, "L0 发现落档 → raised.l1 按条数记")

	// 零告警：全部规则通过 → 不记。
	sk2 := &sinkKeys{}
	svc.SetFeatureSink(sk2.record)
	svc.SetChapterTextReader(func(_ context.Context, _ int64, _ int64) (string, error) {
		return strings.Repeat("他望着远处的山，心中忽而一动。", 200) + "？", nil
	})
	seg2 := svc.l0RuleSweep(ctx, novelID)
	require.Contains(t, seg2, "零规则告警")
	require.Empty(t, sk2.snapshot(), "零告警不记 raised.l1")
}

// TestIssueHitStats 校准取数：按 dimension 聚合 fixed/skipped/pending（字典序确定）；
// 空书 nil。
func TestIssueHitStats(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	require.NoError(t, db.AutoMigrate(&PipelineIssue{}))
	ctx := context.Background()
	rows := []PipelineIssue{
		{NovelID: 1, RunID: "r", IssueKey: "I1", Level: "P1", Status: IssueStatusFixed, Dimension: "timeline"},
		{NovelID: 1, RunID: "r", IssueKey: "I2", Level: "P1", Status: IssueStatusSkipped, Dimension: "timeline"},
		{NovelID: 1, RunID: "r", IssueKey: "I3", Level: "P1", Status: IssueStatusPending},
		{NovelID: 1, RunID: "r", IssueKey: "I4", Level: "P1", Status: IssueStatusFixing},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	got, err := svc.IssueHitStats(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []IssueHitStat{
		{Dimension: "none", Total: 2, Pending: 2}, // 空 dimension 归入 none
		{Dimension: "timeline", Total: 2, Fixed: 1, Skipped: 1},
	}, got, "按 dimension 字典序聚合")

	got2, err := svc.IssueHitStats(ctx, 999)
	require.NoError(t, err)
	require.Nil(t, got2, "零工单=空结果（nil，零提示）")
}

// countKey 统计 keys 中某 key 的出现次数。
func countKey(keys []string, key string) int {
	n := 0
	for _, k := range keys {
		if k == key {
			n++
		}
	}
	return n
}
