package pipeline

// drift.go —— P2 文风漂移检测的采样装配与 V 系 issue 成形（WBS p2-style-gate §0.2）。
//
// 确定性代码（非 LLM）：章节正文→style.ComputeStats→八键 current map；
// 漂移发现→AuditIssue 成形。本文件零写入（A5「只报告不改写」机械保证）。

import (
	"context"
	"fmt"
	"strconv"
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

// isStyleExempt 判定 V 系文风漂移 issue：派发豁免谓词（WBS §0.3 分级纪律消解）。
// 文风漂移「只报告不改写」（A5 同族纪律）——检测器零写入之外，自动修复批次与
// 自动章节重写派发也一律跳过 V 系项；作者显式推进（手动重写/润色）不受此限。
//
// ID 前缀 "V" 单源判别：V 归文风漂移独占命名空间（D 归 G3 蓝图漂移、T 归巡检、
// I 归常规 LLM 体检——见 issueSample 命名空间裁决注释），绝不混排。
func isStyleExempt(id string) bool {
	return strings.HasPrefix(id, "V")
}

// mergeDriftIssues 把检测器 V 系产出并入 LLM 体检结果清单尾部，编号避让既有 ID：
// LLM 主观 V 系先行（V1 起编），检测器 V 系自动续编到既有最大 V 序号 +1，同 run
// 内绝不撞号（WBS §0.2「同 run 内不混排」机械保证，防 upsert/FixStates 按 ID
// 吞项）。既有无 V 系时原样尾部追加（与旧 append 行为逐字一致）。
func mergeDriftIssues(existing, drift []AuditIssue) []AuditIssue {
	if len(drift) == 0 {
		return existing
	}
	maxV := 0
	for _, it := range existing {
		if n, ok := styleIssueIndex(it.ID); ok && n > maxV {
			maxV = n
		}
	}
	if maxV == 0 {
		return append(existing, drift...)
	}
	out := append(existing, make([]AuditIssue, len(drift))...)
	for i, d := range drift {
		d.ID = fmt.Sprintf("V%d", maxV+1+i)
		out[len(existing)+i] = d
	}
	return out
}

// styleIssueIndex 解析 V 系 issue ID 的序号（"V3"→3）；非 V 前缀或非数字尾缀不认。
func styleIssueIndex(id string) (int, bool) {
	if !isStyleExempt(id) || len(id) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(id[1:])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
