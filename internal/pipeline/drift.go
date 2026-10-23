package pipeline

// drift.go —— P2 文风漂移检测的采样装配与 V 系 issue 成形（WBS p2-style-gate §0.2）。
//
// 确定性代码（非 LLM）：章节正文→style.ComputeStats→八键 current map；
// 漂移发现→AuditIssue 成形。本文件零写入（A5「只报告不改写」机械保证）。

import (
	"context"
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/style"
	"github.com/novelgo/novelgo/internal/voice"
	"github.com/novelgo/novelgo/internal/volume"
)

// runStyleDriftCheck 对检测范围章节逐章做文风漂移检测（WBS p2 §0.1–0.3），
// 返回 V 系 issue 清单（按章节阅读序稳定顺编）。范围=同作用域章节：
// scopeVolumeID 非 nil 只扫该卷（卷级 scoped run）；nil 扫全书（S6 终检）。
//
// 降级纪律（宁缺勿错，绝不阻断体检主链）：provider 为 nil、基线为空、章节列表/
// 正文读取失败、正文为空 → 对应部分跳过；整体最多零 issue，从不返回错误。
func (s *Service) runStyleDriftCheck(ctx context.Context, novelID int64, scopeVolumeID *int64) []AuditIssue {
	if s.chapterTextReader == nil || s.voiceMetricsProvider == nil || s.chapterStore == nil {
		return nil
	}
	baseline, err := s.voiceMetricsProvider(ctx, novelID)
	if err != nil || len(baseline) == 0 {
		return nil // 无基线零 issue，不造默认基线
	}
	chapters, err := s.chapterStore.ListAllByNovel(ctx, nil, novelID)
	if err != nil || len(chapters) == 0 {
		return nil
	}
	// 冻结态按卷查（volume.Frozen）；查不到按活跃卷缺省（forward_fix 安全值）。
	frozenByVolume := map[int64]bool{}
	if vols, verr := volume.NewStore(s.store.db, s.logger).ListByNovel(ctx, nil, novelID); verr == nil {
		for _, v := range vols {
			frozenByVolume[v.ID] = v.Frozen
		}
	}
	var out []AuditIssue
	seq := 1
	for _, ch := range chapters {
		if scopeVolumeID != nil && (ch.VolumeID == nil || *ch.VolumeID != *scopeVolumeID) {
			continue
		}
		text, terr := s.chapterTextReader(ctx, novelID, ch.ID)
		if terr != nil || strings.TrimSpace(text) == "" {
			continue // 读不到正文跳过该章（宁缺勿错）
		}
		frozen := ch.VolumeID != nil && frozenByVolume[*ch.VolumeID]
		issues := shapeDriftIssues(
			voice.DetectDrift(baseline, chapterCurrentMetrics(text)), ch.ReadingNumber, frozen, seq)
		seq += len(issues)
		out = append(out, issues...)
	}
	return out
}

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
