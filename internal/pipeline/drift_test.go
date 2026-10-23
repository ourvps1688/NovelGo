package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/voice"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChapterCurrentMetrics 章节正文 → 八键 current map（键全集、值来自确定性统计）。
func TestChapterCurrentMetrics(t *testing.T) {
	got := chapterCurrentMetrics("他说：「你好。」天气不错，我们走吧。真的吗？太好了！")
	require.Len(t, got, 8, "应装配八键全集")
	for _, k := range []string{
		voice.MetricKeyAvgSentLen, voice.MetricKeySentLenStdDev, voice.MetricKeyShortSentPct,
		voice.MetricKeyQuoteDensity, voice.MetricKeyCommaDensity, voice.MetricKeyPeriodDensity,
		voice.MetricKeyExclaimDensity, voice.MetricKeyQuestionDensity,
	} {
		_, ok := got[k]
		assert.True(t, ok, "缺键 %s", k)
	}
	// 空正文 → 八键零值照常装配（是否成 issue 由 DetectDrift 的跳过/限内规则决定）。
	empty := chapterCurrentMetrics("")
	require.Len(t, empty, 8)
	assert.Equal(t, 0.0, empty[voice.MetricKeyAvgSentLen])
}

// TestIssueSampleNamespaces ID 命名空间互不冲突（WBS p2 §0.2 裁决：V 文风 / D 蓝图 / T 巡检 / I 常规；风险 G3）。
func TestIssueSampleNamespaces(t *testing.T) {
	samples := map[string]issueSample{
		"default": issueSampleDefault,
		"drift":   issueSampleDrift,
		"patrol":  issueSamplePatrol,
		"style":   issueSampleStyle,
	}
	seen := map[string]string{}
	for name, s := range samples {
		require.NotEmpty(t, s.ID)
		prefix := s.ID[:1]
		if prev, ok := seen[prefix]; ok {
			t.Fatalf("ID 前缀 %q 冲突：%s 与 %s", prefix, prev, name)
		}
		seen[prefix] = name
	}
	assert.Equal(t, "P1", issueSampleStyle.Level, "V 系 warning 级恒定")
}

// TestShapeDriftIssues_Active 活跃卷成形：V 系顺编 + 三枚举缺省映射 + Desc 四要素。
func TestShapeDriftIssues_Active(t *testing.T) {
	findings := []voice.DriftFinding{
		{Metric: voice.MetricKeyAvgSentLen, Baseline: 12.5, Current: 20, Tol: 3.12},
		{Metric: voice.MetricKeyQuoteDensity, Baseline: 3.2, Current: 9, Tol: 3},
	}
	got := shapeDriftIssues(findings, 5, false, 1)
	require.Len(t, got, 2)
	assert.Equal(t, "V1", got[0].ID)
	assert.Equal(t, "V2", got[1].ID)
	assert.Equal(t, "P1", got[0].Level) // warning 级恒定
	assert.Equal(t, "文风漂移·章节#5", got[0].Area)
	assert.Equal(t, FixTypeBody, got[0].FixType)
	assert.Equal(t, TargetScopeActiveVolume, got[0].TargetScope)
	assert.Equal(t, DimensionNone, got[0].Dimension)
	assert.Equal(t, DirectionForwardFix, got[0].Direction)
	// Desc 四要素：指标名/基线/当前/容差。
	for _, want := range []string{voice.MetricKeyAvgSentLen, "12.50", "20.00", "3.12"} {
		assert.True(t, strings.Contains(got[0].Desc, want), "Desc 缺要素 %q：%s", want, got[0].Desc)
	}
}

// TestShapeDriftIssues_Frozen 冻结卷成形：写保护映射 frozen_volume + retcon_candidate。
func TestShapeDriftIssues_Frozen(t *testing.T) {
	got := shapeDriftIssues([]voice.DriftFinding{
		{Metric: voice.MetricKeyCommaDensity, Baseline: 4, Current: 40, Tol: 1.6},
	}, 12, true, 1)
	require.Len(t, got, 1)
	assert.Equal(t, TargetScopeFrozenVolume, got[0].TargetScope)
	assert.Equal(t, DirectionRetconCandidate, got[0].Direction)
	assert.Equal(t, FixTypeBody, got[0].FixType)
}

// TestShapeDriftIssues_EmptyAndSeq 空 findings 零 issue；顺编从 seqStart 起。
func TestShapeDriftIssues_EmptyAndSeq(t *testing.T) {
	assert.Nil(t, shapeDriftIssues(nil, 1, false, 1))
	assert.Nil(t, shapeDriftIssues([]voice.DriftFinding{}, 1, false, 1))
	got := shapeDriftIssues([]voice.DriftFinding{
		{Metric: voice.MetricKeyShortSentPct, Baseline: 20, Current: 1, Tol: 8},
	}, 3, false, 3)
	require.Len(t, got, 1)
	assert.Equal(t, "V3", got[0].ID)
}

// ── T2.1 executeAudit 漂移检测接线 ─────────────────────────────────

// driftTestText 是显著偏离基线（均句长 10）的单句长文，制造 avg_sent_len 超限。
const driftTestText = "这是一个很长很长的句子测试样本用来制造显著的句长漂移偏差表现。"

// driftTestBaseline 只给 avg_sent_len 一键（其余键缺跳过，每章恰 1 条 issue）。
func driftTestBaseline() map[string]float64 {
	return map[string]float64{voice.MetricKeyAvgSentLen: 10}
}

// TestRunStyleDriftCheck_ScopeAndFrozen 作用域定界与冻结态三枚举映射。
func TestRunStyleDriftCheck_ScopeAndFrozen(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(730)
	v1, v2 := seedFreezeFixtures(t, db, novelID)
	// v1 已冻结（→ frozen_volume/retcon_candidate），v2 活跃（→ active_volume/forward_fix）。
	require.NoError(t, db.Model(&volume.Volume{}).Where("id = ?", v1).Update("frozen", true).Error)
	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return driftTestText, nil
	})
	svc.SetVoiceMetricsProvider(func(_ context.Context, _ int64) (map[string]float64, error) {
		return driftTestBaseline(), nil
	})

	all := svc.runStyleDriftCheck(ctx, novelID, nil) // 全书 3 章各 1 条
	require.Len(t, all, 3)
	assert.Equal(t, "V1", all[0].ID)
	assert.Equal(t, "V3", all[2].ID)
	assert.Equal(t, "文风漂移·章节#1", all[0].Area)
	assert.Equal(t, TargetScopeFrozenVolume, all[0].TargetScope)
	assert.Equal(t, DirectionRetconCandidate, all[0].Direction)
	assert.Equal(t, TargetScopeActiveVolume, all[2].TargetScope)
	assert.Equal(t, DirectionForwardFix, all[2].Direction)

	scoped := svc.runStyleDriftCheck(ctx, novelID, &v2) // scope=v2 仅当前卷 1 章
	require.Len(t, scoped, 1)
	assert.Equal(t, "V1", scoped[0].ID, "顺编在作用域内起编")
	assert.Equal(t, "文风漂移·章节#3", scoped[0].Area, "Area 用全局阅读序号")
}

// TestRunStyleDriftCheck_Degrades 降级零 issue（宁缺勿错）：未注入/空基线/出错/空正文。
func TestRunStyleDriftCheck_Degrades(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(731)
	seedFreezeFixtures(t, db, novelID)

	assert.Nil(t, svc.runStyleDriftCheck(ctx, novelID, nil), "provider 未注入 → 零 issue")

	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return driftTestText, nil
	})
	assert.Nil(t, svc.runStyleDriftCheck(ctx, novelID, nil), "基线 provider 未注入 → 零 issue")

	svc.SetVoiceMetricsProvider(func(_ context.Context, _ int64) (map[string]float64, error) {
		return map[string]float64{}, nil
	})
	assert.Nil(t, svc.runStyleDriftCheck(ctx, novelID, nil), "空基线零 issue 不造默认基线")

	svc.SetVoiceMetricsProvider(func(_ context.Context, _ int64) (map[string]float64, error) {
		return nil, assert.AnError
	})
	assert.Nil(t, svc.runStyleDriftCheck(ctx, novelID, nil), "基线出错 → 零 issue")

	svc.SetVoiceMetricsProvider(func(_ context.Context, _ int64) (map[string]float64, error) {
		return driftTestBaseline(), nil
	})
	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return "", nil
	})
	assert.Nil(t, svc.runStyleDriftCheck(ctx, novelID, nil), "空正文跳过 → 零 issue")
}

// TestAuditMergesStyleDriftIssues ①V 系自动进清单尾部（既有路由容器自然收口）；
// ③verdict 不翻转（drift-only 保持 LLM pass，不触发收口修复循环）。
func TestAuditMergesStyleDriftIssues(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(732)
	_, v2 := seedFreezeFixtures(t, db, novelID)
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "结论：\n```json\n" +
			`{"verdict":"pass","score":31,"summary":"能签","issues":[` +
			`{"id":"I1","level":"P1","area":"节奏","desc":"x","fix_type":"ledger",` +
			`"target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}` + "\n```"}, nil
	}
	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return driftTestText, nil
	})
	svc.SetVoiceMetricsProvider(func(_ context.Context, _ int64) (map[string]float64, error) {
		return driftTestBaseline(), nil
	})

	runID := startS3Run(t, svc, novelID, `{}`)
	book, err := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, err)
	svc.enterAudit(ctx, book, "golden3") // 定界当前卷 v2
	scoped, err := svc.store.GetActiveVolumeRun(ctx, novelID, v2)
	require.NoError(t, err)
	waitForStageStatus(t, svc, scoped.RunID, StatusAwaitingGate)

	after, err := svc.store.GetByRunID(ctx, scoped.RunID)
	require.NoError(t, err)
	var sc GateContext
	require.NoError(t, json.Unmarshal([]byte(after.ContextJSON), &sc))
	var ad AuditData
	require.NoError(t, json.Unmarshal([]byte(sc.Audit), &ad))
	require.Len(t, ad.Issues, 2, "LLM I1 + 漂移 V1")
	assert.Equal(t, "I1", ad.Issues[0].ID)
	assert.Equal(t, "V1", ad.Issues[1].ID, "V 系追加在尾部")
	assert.Equal(t, "文风漂移·章节#3", ad.Issues[1].Area)
	assert.Equal(t, "pass", ad.Verdict, "verdict 不翻转（③）")

	// 自动进既有路由：V 系 body → RewriteBacklog（T2.2 派发豁免另锁）。
	require.Len(t, sc.RewriteBacklog, 1)
	assert.Equal(t, "V1", sc.RewriteBacklog[0].ID)
}

// TestAuditZeroDriftWithoutProviders ②无 provider → issue 零新增，verdict 与旧行为一致。
func TestAuditZeroDriftWithoutProviders(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(733)
	_, v2 := seedFreezeFixtures(t, db, novelID)
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "结论：\n```json\n" +
			`{"verdict":"needs_fix","score":28,"summary":"差口气","issues":[` +
			`{"id":"I1","level":"P1","area":"开篇","desc":"y","fix_type":"ledger"}]}` + "\n```"}, nil
	}

	runID := startS3Run(t, svc, novelID, `{}`)
	book, err := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, err)
	svc.enterAudit(ctx, book, "golden3")
	scoped, err := svc.store.GetActiveVolumeRun(ctx, novelID, v2)
	require.NoError(t, err)
	waitForStageStatus(t, svc, scoped.RunID, StatusAwaitingGate)

	after, err := svc.store.GetByRunID(ctx, scoped.RunID)
	require.NoError(t, err)
	var sc GateContext
	require.NoError(t, json.Unmarshal([]byte(after.ContextJSON), &sc))
	var ad AuditData
	require.NoError(t, json.Unmarshal([]byte(sc.Audit), &ad))
	require.Len(t, ad.Issues, 1, "无 provider 零新增（仅 LLM I1）")
	assert.Equal(t, "I1", ad.Issues[0].ID)
	assert.NotContains(t, sc.Audit, "文风漂移", "降级输出与旧行为逐字一致")
	assert.Equal(t, "needs_fix", ad.Verdict)
}

// TestIsStyleExempt 验证派发豁免谓词（WBS §0.3）只认 V 系命名空间：
// V 归文风漂移独占，I/D/T 系与空 ID 一律不豁免（派发行为逐字不变）。
func TestIsStyleExempt(t *testing.T) {
	for _, id := range []string{"V1", "V12", "V999"} {
		assert.True(t, isStyleExempt(id), id)
	}
	for _, id := range []string{"I1", "D1", "T1", "G1", ""} {
		assert.False(t, isStyleExempt(id), id)
	}
}

// TestStyleIssueRoutingContainers 验证「自动进路由」契约①：V 系经既有容器
// 自然收口，零新路由——body 级 V 系进 RewriteBacklog（bodyIssuesOf），
// 写保护 V 系（frozen_volume/retcon_candidate）进 RetconBacklog（retconItemsOf），
// 两清单互不相交。
func TestStyleIssueRoutingContainers(t *testing.T) {
	audit := AuditData{Issues: []AuditIssue{
		{ID: "V1", Level: "P1", Area: "文风漂移·章节#3", Desc: "第3章文风指标偏离基线",
			FixType: FixTypeBody, TargetScope: TargetScopeActiveVolume,
			Dimension: DimensionNone, Direction: DirectionForwardFix},
		{ID: "V2", Level: "P1", Area: "文风漂移·章节#1", Desc: "第1章文风指标偏离基线",
			FixType: FixTypeBody, TargetScope: TargetScopeFrozenVolume,
			Dimension: DimensionNone, Direction: DirectionRetconCandidate},
	}}
	body := bodyIssuesOf(audit)
	assert.Len(t, body, 1)
	assert.Equal(t, "V1", body[0].ID, "V 系 body 级应自然收口进重写清单")
	ret := retconItemsOf(audit)
	assert.Len(t, ret, 1)
	assert.Equal(t, "V2", ret[0].ID, "写保护 V 系应自然收口进 retcon 清单")
}
