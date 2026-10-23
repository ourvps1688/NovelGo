package voice

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectDrift_RelAndAbsTolerancePaths 命中两条容差路径：
// rel 主导（avg_sent_len 基线 10 → eff=max(3, 2.5)=3）与 abs 主导（quote_density → eff=3）。
func TestDetectDrift_RelAndAbsTolerancePaths(t *testing.T) {
	got := DetectDrift(
		map[string]float64{MetricKeyAvgSentLen: 10, MetricKeyQuoteDensity: 5},
		map[string]float64{MetricKeyAvgSentLen: 14, MetricKeyQuoteDensity: 9},
	)
	require.Len(t, got, 2)
	assert.Equal(t, DriftFinding{Metric: MetricKeyAvgSentLen, Baseline: 10, Current: 14, Tol: 3}, got[0])
	assert.Equal(t, DriftFinding{Metric: MetricKeyQuoteDensity, Baseline: 5, Current: 9, Tol: 3}, got[1])
}

// TestDetectDrift_WithinTolerance 限内不报：差值等于容差按「严格大于」判定不命中。
func TestDetectDrift_WithinTolerance(t *testing.T) {
	// avg_sent_len 基线 10 → eff=3：差 2.9 与恰 3 都不命中；3.01 命中。
	assert.Empty(t, DetectDrift(
		map[string]float64{MetricKeyAvgSentLen: 10},
		map[string]float64{MetricKeyAvgSentLen: 13}, // diff=3 == eff → 不命中
	))
	assert.Empty(t, DetectDrift(
		map[string]float64{MetricKeyAvgSentLen: 10},
		map[string]float64{MetricKeyAvgSentLen: 12.9},
	))
	got := DetectDrift(
		map[string]float64{MetricKeyAvgSentLen: 10},
		map[string]float64{MetricKeyAvgSentLen: 13.01},
	)
	require.Len(t, got, 1)
}

// TestDetectDrift_MissingKeysSkipped 缺键跳过（宁缺勿错）：任一侧缺键不产出。
func TestDetectDrift_MissingKeysSkipped(t *testing.T) {
	assert.Empty(t, DetectDrift(
		map[string]float64{MetricKeyAvgSentLen: 10}, // 基线缺 quote_density
		map[string]float64{MetricKeyAvgSentLen: 10, MetricKeyQuoteDensity: 99},
	))
	assert.Empty(t, DetectDrift(
		map[string]float64{MetricKeyAvgSentLen: 10, MetricKeyQuoteDensity: 5}, // 当前缺 quote_density
		map[string]float64{MetricKeyAvgSentLen: 10},
	))
}

// TestDetectDrift_EmptyInputsZeroOutput 空基线 / 空当前 → 零输出（不造默认基线）。
func TestDetectDrift_EmptyInputsZeroOutput(t *testing.T) {
	cur := map[string]float64{MetricKeyAvgSentLen: 99}
	assert.Nil(t, DetectDrift(nil, cur))
	assert.Nil(t, DetectDrift(map[string]float64{}, cur))
	assert.Nil(t, DetectDrift(map[string]float64{MetricKeyAvgSentLen: 10}, nil))
	assert.Nil(t, DetectDrift(map[string]float64{MetricKeyAvgSentLen: 10}, map[string]float64{}))
}

// TestDetectDrift_BadValuesSkipped 坏数据跳过：NaN/Inf 不产出（宁缺勿错）。
func TestDetectDrift_BadValuesSkipped(t *testing.T) {
	assert.Empty(t, DetectDrift(
		map[string]float64{MetricKeyAvgSentLen: math.NaN()},
		map[string]float64{MetricKeyAvgSentLen: 99},
	))
	assert.Empty(t, DetectDrift(
		map[string]float64{MetricKeyAvgSentLen: 10},
		map[string]float64{MetricKeyAvgSentLen: math.Inf(1)},
	))
}

// TestDetectDrift_ZeroToleranceSkipped 容差非正跳过：rel-only 键基线为 0 → eff=0 无法判定。
func TestDetectDrift_ZeroToleranceSkipped(t *testing.T) {
	// sent_len_std_dev 仅给 Rel：基线 0 → eff=0，即便差值大也不产出（宁宽勿窄）。
	assert.Empty(t, DetectDrift(
		map[string]float64{MetricKeySentLenStdDev: 0},
		map[string]float64{MetricKeySentLenStdDev: 5},
	))
}

// TestDriftTolerances_MetricKeyCoverage 阈值表八键全表覆盖且无多余键（单源纪律）。
func TestDriftTolerances_MetricKeyCoverage(t *testing.T) {
	keys := []string{
		MetricKeyAvgSentLen, MetricKeySentLenStdDev, MetricKeyShortSentPct, MetricKeyQuoteDensity,
		MetricKeyCommaDensity, MetricKeyPeriodDensity, MetricKeyExclaimDensity, MetricKeyQuestionDensity,
	}
	require.Len(t, keys, 8)
	for _, k := range keys {
		_, ok := driftTolerances[k]
		assert.True(t, ok, "阈值表缺键 %s", k)
	}
	assert.Len(t, driftTolerances, len(keys), "阈值表不得有八键之外的键")
	assert.Equal(t, keys, metricCheckOrder, "检测顺序应与八键声明序一致")
}

// TestDetectDrift_StableOrder 多键命中输出按 metricCheckOrder 稳定序（V 系 issue 顺编不抖动）。
func TestDetectDrift_StableOrder(t *testing.T) {
	base := map[string]float64{
		MetricKeyAvgSentLen: 10, MetricKeyShortSentPct: 20,
		MetricKeyQuoteDensity: 5, MetricKeyCommaDensity: 4,
	}
	cur := map[string]float64{
		MetricKeyAvgSentLen: 20, MetricKeyShortSentPct: 1,
		MetricKeyQuoteDensity: 50, MetricKeyCommaDensity: 40,
	}
	got := DetectDrift(base, cur)
	require.Len(t, got, 4)
	wantOrder := []string{
		MetricKeyAvgSentLen, MetricKeyShortSentPct, MetricKeyQuoteDensity, MetricKeyCommaDensity,
	}
	for i, w := range wantOrder {
		assert.Equal(t, w, got[i].Metric)
	}
}
