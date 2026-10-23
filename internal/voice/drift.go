package voice

import "math"

// drift.go —— P2 文风漂移检测的确定性数值比对（WBS p2-style-gate §0.1）。
//
// 纯函数、零写入（A5「只报告不改写」机械保证的检测器一半）：输入风格基线
// （voice_dimensions metrics）与当前章统计（style.ComputeStats 八键映射），
// 输出超限发现。缺键 / 坏值 / 容差非正一律跳过（宁缺勿错），空基线零输出——
// 无数据时不得产出任何猜测性漂移。

// DriftFinding 是一条超限的漂移发现（DetectDrift 输出项）。
type DriftFinding struct {
	Metric   string  // MetricKey* 键（八键之一）
	Baseline float64 // 基线值（voice_dimensions metrics）
	Current  float64 // 当前章统计值
	Tol      float64 // 命中时的生效容差 max(abs_tol, rel_tol×|baseline|)
}

// driftTolerance 是单指标容差。命中条件（WBS §0.1 统一规则）：
//
//	|current−baseline| > max(Abs, Rel×|baseline|)
//
// Rel/Abs 可只给其一（缺省 0）。
type driftTolerance struct{ Rel, Abs float64 }

// driftTolerances 是漂移阈值表**单源**（WBS §0.1）：保守初值只报明显偏离，
// P4 度量后调参——改动只允许发生在这里；键一律引用 MetricKey* 常量，
// 禁止字符串字面量两处各写。
var driftTolerances = map[string]driftTolerance{
	MetricKeyAvgSentLen:      {Rel: 0.25, Abs: 3},   // 句长：±25% 或 ±3 字
	MetricKeySentLenStdDev:   {Rel: 0.30},           // 句长波动：±30%
	MetricKeyShortSentPct:    {Abs: 8},              // 短句占比：±8 个百分点
	MetricKeyQuoteDensity:    {Abs: 3},              // 对话密度：±3 个百分点
	MetricKeyCommaDensity:    {Rel: 0.40, Abs: 0.5}, // 逗号密度：±40% 或 ±0.5 点
	MetricKeyPeriodDensity:   {Rel: 0.40, Abs: 0.5}, // 句号密度：±40% 或 ±0.5 点
	MetricKeyExclaimDensity:  {Rel: 0.40, Abs: 0.3}, // 感叹密度：±40% 或 ±0.3 点
	MetricKeyQuestionDensity: {Rel: 0.40, Abs: 0.3}, // 疑问密度：±40% 或 ±0.3 点
}

// metricCheckOrder 是固定检测顺序（八键全集）：输出顺序稳定，
// 供上层 V 系 issue 顺编（issue ID 不随 map 迭代序抖动）。
var metricCheckOrder = []string{
	MetricKeyAvgSentLen,
	MetricKeySentLenStdDev,
	MetricKeyShortSentPct,
	MetricKeyQuoteDensity,
	MetricKeyCommaDensity,
	MetricKeyPeriodDensity,
	MetricKeyExclaimDensity,
	MetricKeyQuestionDensity,
}

// DetectDrift 逐键比对基线与当前值，返回超限发现（按 metricCheckOrder 稳定序）。
//
// 宁缺勿错跳过规则（WBS §0.1）：
//   - 基线或当前缺键 → 该键跳过；
//   - 任一值 NaN/Inf（坏数据）→ 该键跳过；
//   - 生效容差非正（如 rel-only 键基线为 0）→ 无法判定，跳过；
//   - 基线整体为空 / current 为空 → 零输出（不造默认基线）。
func DetectDrift(baseline, current map[string]float64) []DriftFinding {
	if len(baseline) == 0 || len(current) == 0 {
		return nil
	}
	var out []DriftFinding
	for _, key := range metricCheckOrder {
		tol, ok := driftTolerances[key]
		if !ok {
			continue
		}
		base, okB := baseline[key]
		cur, okC := current[key]
		if !okB || !okC {
			continue
		}
		if !finite(base) || !finite(cur) {
			continue
		}
		eff := tol.Abs
		if rel := tol.Rel * math.Abs(base); rel > eff {
			eff = rel
		}
		if eff <= 0 {
			continue
		}
		if math.Abs(cur-base) > eff {
			out = append(out, DriftFinding{Metric: key, Baseline: base, Current: cur, Tol: eff})
		}
	}
	return out
}

// finite 判断 v 是有限数（非 NaN/Inf）。
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
