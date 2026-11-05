package pipeline

// P0-3 评分打通：双轨评分趋势视图数据包（逐章分数/发现数曲线 + 单章修复前后对比），
// 回答「这次重写到底提分了没有」。
//
// 数据源双轨：
//   - 主轨 quality.ChapterScoreEvent（评分明细历史，append-only）：逐章最新/上一次
//     判官分、修复前后变化、6 维分维全量；
//   - 回落 chapters.quality_score 快照（本表落地前的旧数据）：无历史行的已评分章节
//     按快照回填（attempts=1、delta=0、无分维）。
//
// 6↔8 映射换算单源 review（JudgeToAuditScores / ScoreDimInfos）：此处只装配数据包，
// 不硬编码任何维度键或映射关系。

import (
	"context"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/review"
)

// ScoreTrendPoint 是趋势曲线的一个点（一章，随全书按全局阅读序号升序）。
type ScoreTrendPoint struct {
	ChapterID     int64  `json:"chapter_id"`
	ReadingNumber int    `json:"reading_number"`
	Title         string `json:"title"`
	// Scored 该章是否已有评分（false=未评分，Score/PrevScore/Delta 无意义）。
	Scored bool `json:"scored"`
	// Score 是最新判官总分（0-100）；PrevScore 是上一次判官总分（无上一次为 0）。
	Score     int `json:"score"`
	PrevScore int `json:"prev_score"`
	// Delta 是修复前后变化（score-prev_score；仅一次评分为 0）。
	Delta int `json:"delta"`
	// Attempts 是评分次数（评分明细历史行数；旧快照回填计 1）。
	Attempts int `json:"attempts"`
	// L0Findings/L1Findings 是当前发现数（质量档案），OpenFindings 是未修复发现数。
	L0Findings   int `json:"l0_findings"`
	L1Findings   int `json:"l1_findings"`
	OpenFindings int `json:"open_findings"`
}

// ScoreEventView 是单章评分历史的一条（修复前后对比）。
type ScoreEventView struct {
	ID        int64  `json:"id"`
	Overall   int    `json:"overall"` // 判官总分（0-100）
	Verdict   string `json:"verdict"`
	Critique  string `json:"critique"`
	Source    string `json:"source"` // 写入通道：new / full_replace / snapshot（旧快照回填）
	CreatedAt string `json:"created_at"`
	// JudgeDims 是 6 维判官原始分（0-100）；AuditDims 是换算后的 8 维体检口径
	// （1-5 星 + 贡献源，同源展示）。旧快照回填无分维（两者为空/nil）。
	JudgeDims map[string]int              `json:"judge_dims"`
	AuditDims []review.JudgeAuditDimScore `json:"audit_dims"`
}

// ScoreTrend 是全书评分趋势视图数据包（P0-3）。
type ScoreTrend struct {
	Points []ScoreTrendPoint `json:"points"`
	// JudgeDims/AuditDimOrder 是 6↔8 映射契约快照（单源 review），前端图例与
	// 同源分对照表直接消费，不在前端另行硬编码维度键。
	JudgeDims     []review.ScoreDimInfo `json:"judge_dims"`
	AuditDimOrder []string              `json:"audit_dim_order"`
}

// scoreSourceSnapshot 是旧快照回填事件的 Source 标记（区别于真实评分事件）。
const scoreSourceSnapshot = "snapshot"

// scoreTrendStores 汇总趋势查询所需的仓储（测试可注入 stub 语义一致）。
func (s *Service) scoreTrendStores() (*quality.Store, *chapter.Store) {
	qs := s.qualityStore
	if qs == nil {
		qs = quality.NewStore(s.store.db, s.logger)
	}
	cs := s.chapterStore
	if cs == nil {
		cs = chapter.NewStore(s.store.db, s.logger)
	}
	return qs, cs
}

// GetScoreTrend 返回全书评分趋势数据包：逐章最新/上一次判官分与修复前后变化、
// 发现数曲线，以及 6↔8 映射契约快照。按全局阅读序号升序。
func (s *Service) GetScoreTrend(ctx context.Context, novelID int64) (*ScoreTrend, error) {
	trend := &ScoreTrend{
		Points:        []ScoreTrendPoint{},
		JudgeDims:     review.ScoreDimInfos(),
		AuditDimOrder: append([]string(nil), review.AuditDimOrder...),
	}
	qs, cs := s.scoreTrendStores()

	chapters, err := cs.ListAllByNovel(ctx, nil, novelID)
	if err != nil {
		return nil, err
	}
	rnMap, err := cs.GetReadingNumbersByNovel(ctx, nil, novelID)
	if err != nil {
		return nil, err
	}
	events, err := qs.ListScoreEventsByNovel(ctx, novelID)
	if err != nil {
		return nil, err
	}
	rows, err := qs.ListByNovel(ctx, novelID)
	if err != nil {
		return nil, err
	}

	// 发现数按章索引（质量档案唯一行）。
	findings := map[int64][3]int{} // chapter_id → {l0, l1, open}
	for _, r := range rows {
		l0 := quality.ParseFindings(r.L0Findings)
		l1 := quality.ParseFindings(r.L1Findings)
		open := 0
		for _, f := range append(append([]quality.Finding{}, l0...), l1...) {
			if f.Status == "" || f.Status == quality.FindingStatusPending {
				open++
			}
		}
		findings[r.ChapterID] = [3]int{len(l0), len(l1), open}
	}

	// 评分事件按章分组（ListScoreEventsByNovel 恒按 id 升序，最新在尾）。
	type chapterEvents struct {
		all []quality.ChapterScoreEvent
	}
	byChapter := map[int64]*chapterEvents{}
	for _, ev := range events {
		b := byChapter[ev.ChapterID]
		if b == nil {
			b = &chapterEvents{}
			byChapter[ev.ChapterID] = b
		}
		b.all = append(b.all, ev)
	}

	for _, ch := range chapters {
		p := ScoreTrendPoint{
			ChapterID:     ch.ID,
			ReadingNumber: rnMap[ch.ID],
			Title:         ch.Title,
			L0Findings:    findings[ch.ID][0],
			L1Findings:    findings[ch.ID][1],
			OpenFindings:  findings[ch.ID][2],
		}
		if b := byChapter[ch.ID]; b != nil && len(b.all) > 0 {
			latest := b.all[len(b.all)-1]
			p.Scored = true
			p.Score = latest.Overall
			p.Attempts = len(b.all)
			if len(b.all) >= 2 {
				p.PrevScore = b.all[len(b.all)-2].Overall
				p.Delta = p.Score - p.PrevScore
			}
		} else if ch.QualityScore != nil {
			// 旧数据快照回填：只有 chapters.quality_score 一行、无明细历史。
			p.Scored = true
			p.Score = *ch.QualityScore
			p.Attempts = 1
		}
		trend.Points = append(trend.Points, p)
	}
	return trend, nil
}

// GetChapterScoreHistory 返回单章评分历史（修复前后对比），按时间正序。
// 无明细历史但有旧快照时回填单条 snapshot 事件（无分维），前端可统一渲染。
func (s *Service) GetChapterScoreHistory(ctx context.Context, novelID, chapterID int64) ([]ScoreEventView, error) {
	qs, cs := s.scoreTrendStores()
	events, err := qs.ListScoreEvents(ctx, novelID, chapterID)
	if err != nil {
		return nil, err
	}
	views := make([]ScoreEventView, 0, len(events)+1)
	for _, ev := range events {
		judgeDims := quality.ParseScoreDims(ev.Dimensions)
		v := ScoreEventView{
			ID:        ev.ID,
			Overall:   ev.Overall,
			Verdict:   ev.Verdict,
			Critique:  ev.Critique,
			Source:    ev.Source,
			CreatedAt: ev.CreatedAt.Format("2006-01-02 15:04:05"),
			JudgeDims: judgeDims,
			AuditDims: review.JudgeToAuditScores(judgeDims),
		}
		views = append(views, v)
	}
	if len(views) > 0 {
		return views, nil
	}
	// 旧快照回填（本表落地前的已评分章节）。
	ch, err := cs.GetByID(ctx, nil, novelID, chapterID)
	if err != nil || ch == nil || ch.QualityScore == nil {
		return []ScoreEventView{}, nil
	}
	v := ScoreEventView{
		Overall:  *ch.QualityScore,
		Critique: ch.QualityReport,
		Source:   scoreSourceSnapshot,
	}
	if ch.QualityScoredAt != nil {
		v.CreatedAt = ch.QualityScoredAt.Format("2006-01-02 15:04:05")
	}
	return append(views, v), nil
}
