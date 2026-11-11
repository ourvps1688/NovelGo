package mcp_tools

import (
	"context"
	"log/slog"

	"github.com/novelgo/novelgo/internal/quality"
)

// chapter_prescription.go —— #114 判官验收处方装配（只读、零派发）。
//
// 评分门禁发生在正文落盘「之前」，此时 chapter_quality 里存的正是上一轮
// 未消除的发现；用本轮 proposed 正文重算实测值后投影成处方，判官就能逐条
// 核验「这轮到底消掉了哪几条」，而不是每轮给出一套全新的主观意见。
// 读档案失败/新章无 id/零发现 → 空处方，评分行为逐字保持缺省（不阻断）。

// prescriptionLinesForChapter 返回可注入判官 user prompt 的验收条件行。
func prescriptionLinesForChapter(ctx context.Context, tc ToolContext, chapterID int64, proposed string, words quality.ChapterWordsTarget, wordCount int) []string {
	if chapterID <= 0 || tc.DB == nil {
		return nil
	}
	logger := tc.LoggerOrDefault()
	if logger == nil {
		logger = slog.Default()
	}
	row, err := quality.NewStore(tc.DB, logger).GetByChapter(ctx, tc.NovelID, chapterID)
	if err != nil || row == nil {
		return nil
	}
	findings := append(quality.ParseFindings(row.L0Findings), quality.ParseFindings(row.L1Findings)...)
	if len(findings) == 0 {
		return nil
	}
	curve := quality.BuildTensionCurve(proposed)
	return quality.RenderPrescriptions(quality.BuildPrescriptions(findings, curve, wordCount, words))
}

// findingKeysForChapter 返回评分时注入判官的那一组未消除发现的稳定 key 快照。
// 必须在审批/落盘「之前」取：ScheduleChapterCI 会在落盘后重算 L0，届时档案
// 已不是判官看到的那一轮。口径与处方装配同源于 FindingKeys，零新增算法。
// 读档案失败/新章无 id/零发现 → nil（旧数据语义：无快照，不反推）。
func findingKeysForChapter(ctx context.Context, tc ToolContext, chapterID int64) []string {
	if chapterID <= 0 || tc.DB == nil {
		return nil
	}
	logger := tc.LoggerOrDefault()
	if logger == nil {
		logger = slog.Default()
	}
	row, err := quality.NewStore(tc.DB, logger).GetByChapter(ctx, tc.NovelID, chapterID)
	if err != nil || row == nil {
		return nil
	}
	return quality.FindingKeys(
		quality.ParseFindings(row.L0Findings),
		quality.ParseFindings(row.L1Findings),
	)
}
