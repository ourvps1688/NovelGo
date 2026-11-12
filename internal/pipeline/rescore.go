package pipeline

// rescore.go —— 显式重判分（#115 P0）。
//
// 背景：「修复前后对比」读 chapter_score_events，而该表此前唯一写入点是 edit 工具
// 的成章写评分门禁，硬门只对「新建章节 / full_replace」生效；chat 里的定点改稿
// （search_replace / line_range_replace）与手动章节检测都不评分。于是正文改了多次，
// 历史轴仍停在唯一那次全文重写，用户看不到「这次修复到底提分了没有」。
//
// 本入口是**人工在环的读-评-记**：只在用户点「重新判分」时调用一次判官，用当前
// 落盘正文 + 与评分门禁同源的字数口径/验收处方，追加一条 source=rescore 的历史。
// 不改正文、不触发审批、不派发修复、不动工单，也不放宽 edit 的评分门禁
// （门禁带「不达标退回重写」的阻断语义，不能套到局部改稿上）。

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/review"
	"github.com/novelgo/novelgo/internal/text"
)

// RescoreChapter 用当前落盘正文显式重判分并追加一条评分历史。
// 返回新事件视图供前端即时反馈；失败如实返回错误，不静默造记录。
func RescoreChapter(ctx context.Context, db *gorm.DB, client review.LLMClient, logger *slog.Logger,
	provider, model string, novelID, chapterID int64) (*ScoreEventView, error) {
	if db == nil || client == nil {
		return nil, fmt.Errorf("重判分：LLM 或数据库未就绪")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if novelID <= 0 || chapterID <= 0 || provider == "" || model == "" {
		return nil, fmt.Errorf("重判分：章节或模型参数不完整，请在设置中检查模型")
	}
	content, err := git.ReadFile(novelID, git.ChapterPath(chapterID))
	if err != nil {
		return nil, fmt.Errorf("重判分：读取章节正文失败: %w", err)
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("重判分：本章正文为空，无法评分")
	}
	outline, _ := git.ReadFile(novelID, git.OutlinePath(chapterID))
	stats := text.ComputeStats(content)

	qs := quality.NewStore(db, logger)
	// #113 单章字数口径单源：与检测 L0 / 写作系统 / 评分门禁完全同源。
	words := qs.ChapterWords(ctx, novelID)

	// 验收处方与 finding key 快照同源于 quality（与 rw_tools 评分门禁相同算法）。
	// 重判分读的是「当前」档案，即用户这次改稿后重新检测过的结果。
	var lines []string
	var keys []string
	if row, rerr := qs.GetByChapter(ctx, novelID, chapterID); rerr == nil && row != nil {
		l0 := quality.ParseFindings(row.L0Findings)
		l1 := quality.ParseFindings(row.L1Findings)
		keys = quality.FindingKeys(l0, l1)
		all := append(l0, l1...)
		if len(all) > 0 {
			lines = quality.RenderPrescriptions(quality.BuildPrescriptions(all,
				quality.BuildTensionCurve(content), stats.WordCount, words))
		}
	}
	scored, err := review.ScoreChapterWithPrescriptions(ctx, client, provider, model,
		content, outline, stats.WordCount, words.SystemLine(), lines)
	if err != nil {
		return nil, fmt.Errorf("重判分：判官调用失败: %w", err)
	}

	cs := chapter.NewStore(db, logger)
	ch, err := cs.GetByID(ctx, nil, novelID, chapterID)
	if err != nil || ch == nil {
		return nil, fmt.Errorf("重判分：章节不存在（chapter_id=%d）", chapterID)
	}
	readingNumber, _ := cs.GetReadingNumberByID(ctx, nil, novelID, chapterID)
	now := time.Now()
	// 当前快照与历史轴同源写入（口径同 rw_tools.persistChapterScore）。
	if err := db.WithContext(ctx).Model(&chapter.Chapter{}).Where("id = ?", ch.ID).
		Updates(map[string]any{
			"quality_score":     scored.Overall,
			"quality_report":    scored.Critique,
			"quality_scored_at": now,
		}).Error; err != nil {
		logger.Warn("重判分：落库章节评分快照失败", "chapter_id", ch.ID, "err", err)
	}
	ev := &quality.ChapterScoreEvent{
		NovelID:       novelID,
		ChapterID:     ch.ID,
		ReadingNumber: readingNumber,
		Overall:       scored.Overall,
		Dimensions:    quality.MarshalScoreDims(scored.Dimensions),
		Verdict:       scored.Verdict,
		Critique:      scored.Critique,
		FindingKeys:   quality.MarshalFindingKeys(keys),
		Source:        quality.ScoreSourceRescore,
		CreatedAt:     now,
	}
	if err := qs.AppendScoreEvent(ctx, ev); err != nil {
		return nil, fmt.Errorf("重判分：追加评分历史失败: %w", err)
	}
	return &ScoreEventView{
		ID:          ev.ID,
		Overall:     scored.Overall,
		Verdict:     scored.Verdict,
		Critique:    scored.Critique,
		Source:      quality.ScoreSourceRescore,
		CreatedAt:   now.Format("2006-01-02 15:04:05"),
		JudgeDims:   scored.Dimensions,
		AuditDims:   review.JudgeToAuditScores(scored.Dimensions),
		FindingKeys: keys,
	}, nil
}
