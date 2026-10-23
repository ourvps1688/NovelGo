package pipeline

import (
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/voice"
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
