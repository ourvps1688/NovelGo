package pipeline

// drift.go —— P2 文风漂移检测的采样装配与 V 系 issue 成形（WBS p2-style-gate §0.2）。
//
// 确定性代码（非 LLM）：章节正文→style.ComputeStats→八键 current map；
// 漂移发现→AuditIssue 成形。本文件零写入（A5「只报告不改写」机械保证）。

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/style"
	"github.com/novelgo/novelgo/internal/voice"
)

// chapterCurrentMetrics 对单章正文做确定性统计并装配八键 current map
// （键取 style.StatsMetrics / voice.MetricKey* 单源）。
func chapterCurrentMetrics(chapterText string) map[string]float64 {
	st := style.ComputeStats([]style.Sample{{Content: chapterText}})
	return style.StatsMetrics(st)
}

// shapeDriftIssues 把漂移发现成形为 V 系 issue（WBS §0.2 契约）：
//   - ID=V{seqStart+i} 顺编（V 系命名空间，与 LLM I 系/D 蓝图系/T 巡检系不混排）；
//   - Level 恒 P1（warning 级起步，不上 error/P0，不阻断）；
//   - Dimension 恒 none（文风非四维 series bible 事实）；fix_type 恒 body；
//   - TargetScope/Direction 按目标卷冻结态映射：frozen→frozen_volume+retcon_candidate
//     （写保护命中）；active→active_volume+forward_fix（缺省）；
//   - Desc 含指标名/基线值/当前值/容差四要素。
//
// findings 为空返回 nil（零 issue，降级纪律）。
func shapeDriftIssues(findings []voice.DriftFinding, chapterNum int, frozen bool, seqStart int) []AuditIssue {
	if len(findings) == 0 {
		return nil
	}
	scope, dir := TargetScopeActiveVolume, DirectionForwardFix
	if frozen {
		scope, dir = TargetScopeFrozenVolume, DirectionRetconCandidate
	}
	out := make([]AuditIssue, 0, len(findings))
	for i, f := range findings {
		out = append(out, AuditIssue{
			ID:    fmt.Sprintf("V%d", seqStart+i),
			Level: "P1", // warning 级恒定（WBS §0.2）
			Area:  fmt.Sprintf("文风漂移·章节#%d", chapterNum),
			Desc: fmt.Sprintf("文风指标 %s 偏离基线：基线 %.2f，当前 %.2f，容差 %.2f",
				f.Metric, f.Baseline, f.Current, f.Tol),
			FixType:     FixTypeBody,
			TargetScope: scope,
			Dimension:   DimensionNone,
			Direction:   dir,
		})
	}
	return out
}
