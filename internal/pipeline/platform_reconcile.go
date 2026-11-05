package pipeline

// P1-2 真实数据对账：「流失点章节 × 低分维」交叉诊断——把七猫后台逐章完读率/追读率
// 与 6 维判官分、质量档案发现对照，回答「读者在哪里流失、和哪些低分维显著相关」。
//
// 契约要点（勿改）：
//   - 流失章判定：相邻有数据章之间完读率/追读率下降 ≥5pp（platformChurnDropPP），
//     首章无前值不判；阈值是常量口径，实证调参前不动；
//   - 低分维：判官维 <platformLowJudgeScore 记低分，经 6↔8 映射（review.ScoreDimToAuditDims
//     单源）投影到体检维；分母只计**有评分**的章（无评分章无维可判，宁缺勿错）；
//   - lift = 流失章低分率 − 稳定章低分率（>0 = 该维在流失章更常见），按 lift 降序。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/review"
)

const (
	// platformChurnDropPP 是流失章判定阈值（百分点）。
	platformChurnDropPP = 5.0
	// platformLowJudgeScore 是判官维低分线（0-100 分制）。
	platformLowJudgeScore = 60
)

// PlatformChapterDiag 是一章的对账诊断行（仅有平台数据的章出现）。
type PlatformChapterDiag struct {
	ChapterID      int64    `json:"chapter_id"`
	ReadingNumber  int      `json:"reading_number"`
	Title          string   `json:"title"`
	CompletionRate *float64 `json:"completion_rate"`
	RetentionRate  *float64 `json:"retention_rate"`
	// CompletionDrop/RetentionDrop 是较前一有数据章的下降百分点（>0=下降；首章/缺率为 0）。
	CompletionDrop float64 `json:"completion_drop"`
	RetentionDrop  float64 `json:"retention_drop"`
	// Churn 是流失章判定（任一率下降 ≥5pp）。
	Churn bool `json:"churn"`
	// Scored/Score 是该章最新判官总分（0-100；未评分为 false/0）。
	Scored bool `json:"scored"`
	Score  int  `json:"score"`
	// LowJudgeDims 是低分判官维（<60）的键；LowAuditDims 是其映射的体检维（单源 review）。
	LowJudgeDims []string `json:"low_judge_dims"`
	LowAuditDims []string `json:"low_audit_dims"`
	// OpenFindings 是未修复发现的检查项（去重，质量档案）。
	OpenFindings []string `json:"open_findings"`
}

// PlatformDimStat 是「流失章 × 低分维」交叉统计一行。
type PlatformDimStat struct {
	// Dim 是体检维键（八键，review.AuditDimOrder）。
	Dim         string `json:"dim"`
	ChurnLow    int    `json:"churn_low"`
	ChurnTotal  int    `json:"churn_total"`
	StableLow   int    `json:"stable_low"`
	StableTotal int    `json:"stable_total"`
	// Lift = 流失章低分率 − 稳定章低分率（>0=该维在流失章更常见）。
	Lift float64 `json:"lift"`
	// JudgeSources 是投影到本维的判官维键（映射单源 review.ScoreDimToAuditDims）。
	JudgeSources []string `json:"judge_sources"`
}

// PlatformReconciliation 是「平台数据 × 评分/发现」对账诊断数据包（P1-2）。
type PlatformReconciliation struct {
	Chapters []PlatformChapterDiag `json:"chapters"`
	// DimStats 按 lift 降序、同 lift 按流失命中数降序（显著流失关联维在前）。
	DimStats []PlatformDimStat `json:"dim_stats"`
	// BookAuditDims 是全书级体检八维星级快照（最近一次书体检产物；未跑为空）。
	BookAuditDims []AuditDimScore `json:"book_audit_dims"`
	// JudgeDims/AuditDimOrder 是 6↔8 映射契约快照（单源 review，前端不硬编码维度键）。
	JudgeDims     []review.ScoreDimInfo `json:"judge_dims"`
	AuditDimOrder []string              `json:"audit_dim_order"`
	Summary       string                `json:"summary"`
}

// GetPlatformReconciliation 返回「流失章 × 低分维」交叉诊断数据包。
// 未录入平台数据时返回契约快照 + 空清单 + 摘要提示（不报错）。
func (s *Service) GetPlatformReconciliation(ctx context.Context, novelID int64) (*PlatformReconciliation, error) {
	out := &PlatformReconciliation{
		Chapters:      []PlatformChapterDiag{},
		DimStats:      []PlatformDimStat{},
		BookAuditDims: []AuditDimScore{},
		JudgeDims:     review.ScoreDimInfos(),
		AuditDimOrder: append([]string(nil), review.AuditDimOrder...),
	}
	qs, _ := s.scoreTrendStores()
	metrics, err := qs.ListPlatformMetrics(ctx, novelID)
	if err != nil {
		return nil, err
	}
	if len(metrics) == 0 {
		out.Summary = "尚未录入平台数据：粘贴七猫后台逐章完读率/追读率后可出对账诊断。"
		return out, nil
	}

	// 章节归属 + 发现 + 评分（一次装配）。
	idx, err := s.platformDataChapterIndex(ctx, novelID)
	if err != nil {
		return nil, err
	}
	findingsByChapter := map[int64][]string{}
	if rows, err := qs.ListByNovel(ctx, novelID); err == nil {
		for _, r := range rows {
			seen := map[string]bool{}
			for _, f := range append(quality.ParseFindings(r.L0Findings), quality.ParseFindings(r.L1Findings)...) {
				if f.Status == "" || f.Status == quality.FindingStatusPending {
					if !seen[f.Check] {
						seen[f.Check] = true
						findingsByChapter[r.ChapterID] = append(findingsByChapter[r.ChapterID], f.Check)
					}
				}
			}
		}
	}
	judgeDimsByChapter := map[int64]map[string]int{}
	scoreByChapter := map[int64]int{}
	if events, err := qs.ListScoreEventsByNovel(ctx, novelID); err == nil {
		for _, ev := range events { // 恒按 id 升序，最后写入者胜
			judgeDimsByChapter[ev.ChapterID] = quality.ParseScoreDims(ev.Dimensions)
			scoreByChapter[ev.ChapterID] = ev.Overall
		}
	}

	// 逐章诊断（按全局阅读序号升序；相邻=前一有数据章）。
	var prevCompletion, prevRetention *float64
	churnScored, churnTotal := 0, 0
	stableScored, stableTotal := 0, 0
	// 低分维命中章集合：audit dim → churn|stable → 章 ID 集合。
	lowTally := map[string]map[string]map[int64]bool{}
	markLow := func(dim, group string, chapterID int64) {
		if lowTally[dim] == nil {
			lowTally[dim] = map[string]map[int64]bool{}
		}
		if lowTally[dim][group] == nil {
			lowTally[dim][group] = map[int64]bool{}
		}
		lowTally[dim][group][chapterID] = true
	}
	for _, m := range metrics {
		d := PlatformChapterDiag{
			ChapterID:      m.ChapterID,
			ReadingNumber:  m.ReadingNumber,
			CompletionRate: m.CompletionRate,
			RetentionRate:  m.RetentionRate,
			LowJudgeDims:   []string{},
			LowAuditDims:   []string{},
			OpenFindings:   findingsByChapter[m.ChapterID],
		}
		if ch, ok := idx[m.ReadingNumber]; ok && ch.ID == m.ChapterID {
			d.Title = ch.Title
		}
		if m.CompletionRate != nil && prevCompletion != nil {
			d.CompletionDrop = *prevCompletion - *m.CompletionRate
		}
		if m.RetentionRate != nil && prevRetention != nil {
			d.RetentionDrop = *prevRetention - *m.RetentionRate
		}
		d.Churn = d.CompletionDrop >= platformChurnDropPP || d.RetentionDrop >= platformChurnDropPP
		if m.CompletionRate != nil {
			prevCompletion = m.CompletionRate
		}
		if m.RetentionRate != nil {
			prevRetention = m.RetentionRate
		}

		group := "stable"
		if d.Churn {
			group = "churn"
			churnTotal++
		} else {
			stableTotal++
		}
		if dims, ok := judgeDimsByChapter[m.ChapterID]; ok {
			d.Scored = true
			d.Score = scoreByChapter[m.ChapterID]
			if d.Churn {
				churnScored++
			} else {
				stableScored++
			}
			seenAudit := map[string]bool{}
			for key, v := range dims {
				if v >= platformLowJudgeScore {
					continue
				}
				d.LowJudgeDims = append(d.LowJudgeDims, key)
				for _, auditKey := range review.ScoreDimToAuditDims[key] {
					if !seenAudit[auditKey] {
						seenAudit[auditKey] = true
						d.LowAuditDims = append(d.LowAuditDims, auditKey)
					}
					markLow(auditKey, group, m.ChapterID)
				}
			}
			sort.Strings(d.LowJudgeDims)
			sort.Strings(d.LowAuditDims)
		}
		out.Chapters = append(out.Chapters, d)
	}

	// 交叉统计：分母只计有评分的章（无评分无维可判）。
	for _, dim := range review.AuditDimOrder {
		stat := PlatformDimStat{
			Dim:          dim,
			ChurnTotal:   churnScored,
			StableTotal:  stableScored,
			JudgeSources: review.AuditDimJudgeSources(dim),
		}
		stat.ChurnLow = len(lowTally[dim]["churn"])
		stat.StableLow = len(lowTally[dim]["stable"])
		if stat.ChurnTotal > 0 {
			stat.Lift += float64(stat.ChurnLow) / float64(stat.ChurnTotal)
		}
		if stat.StableTotal > 0 {
			stat.Lift -= float64(stat.StableLow) / float64(stat.StableTotal)
		}
		out.DimStats = append(out.DimStats, stat)
	}
	sort.SliceStable(out.DimStats, func(i, j int) bool {
		a, b := out.DimStats[i], out.DimStats[j]
		if a.Lift != b.Lift {
			return a.Lift > b.Lift
		}
		return a.ChurnLow > b.ChurnLow
	})

	out.BookAuditDims = s.latestBookAuditDims(ctx, novelID)
	out.Summary = platformReconcileSummary(out)
	return out, nil
}

// latestBookAuditDims 取最近一次书体检的八维星级快照（未跑/解析失败为空，宁缺勿错）。
func (s *Service) latestBookAuditDims(ctx context.Context, novelID int64) []AuditDimScore {
	run, err := s.store.GetLatestDetectRun(ctx, novelID, DetectionKindBook)
	if err != nil || run == nil || run.ContextJSON == "" {
		return []AuditDimScore{}
	}
	var ctxt GateContext
	if err := json.Unmarshal([]byte(run.ContextJSON), &ctxt); err != nil {
		return []AuditDimScore{}
	}
	audit := parseStoredAudit(ctxt.Audit)
	if audit == nil || len(audit.Dims) == 0 {
		return []AuditDimScore{}
	}
	return audit.Dims
}

// platformReconcileSummary 生成对账诊断一句话摘要（服务端口径文案，前端直接呈现）。
func platformReconcileSummary(r *PlatformReconciliation) string {
	churn := 0
	for _, c := range r.Chapters {
		if c.Churn {
			churn++
		}
	}
	if churn == 0 {
		return fmt.Sprintf("已录入 %d 章平台数据，无流失章（相邻章率下降均 <%.0fpp）。", len(r.Chapters), platformChurnDropPP)
	}
	s := fmt.Sprintf("已录入 %d 章，流失章 %d（相邻完读率/追读率下降 ≥%.0fpp）；", len(r.Chapters), churn, platformChurnDropPP)
	var top []string
	for _, st := range r.DimStats {
		if st.Lift > 0 && st.ChurnLow > 0 {
			top = append(top, st.Dim)
		}
		if len(top) >= 2 {
			break
		}
	}
	if len(top) > 0 {
		s += "流失关联最显著维：" + joinAuditDimLabels(top) + "。"
	} else {
		s += "流失章未见集中低分维（或评分覆盖不足），建议先跑章节评分补数据。"
	}
	return s
}

// joinAuditDimLabels 拼接体检维标签（review 单源 Label 优先，缺省回 key）。
func joinAuditDimLabels(keys []string) string {
	labels := review.AuditDimLabels()
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += "、"
		}
		if v, ok := labels[k]; ok && v != "" {
			out += v
		} else {
			out += k
		}
	}
	return out
}
