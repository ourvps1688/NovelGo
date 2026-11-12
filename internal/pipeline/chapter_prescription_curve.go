package pipeline

// chapter_prescription_curve.go —— #114 P3「处方与判定同源」。
//
// 手动重检时 RecalcChapterL0FromDisk 按**当前落盘正文**现场跑 RunL0Checks
// （爽点/拖沓/开篇坡度都吃这条现场曲线），但按项目纪律**不刷新** chapter_tension
// 的 DB 曲线——那张表只在写章评分链路落库，chat 定点 patch 改稿后会停在旧快照。
// 旧 GetChapterQualityArchive 的处方恰读 DB 的 source=l0 行，于是出现
// 「判定按新正文、处方按旧曲线」：LLM 拿到过期段位与过期实测值，改哪儿/差多少全错。
//
// 本文件把处方曲线收口成同一份现场计算：BuildTensionCurve(落盘正文)，与
// RunL0Checks 现场判定逐字同源；读盘失败才降级到 DB 旧行（宁缺勿错、绝不臆造）。
// 纯只读：零写库、零派发、零自动修复。

import (
	"context"

	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/quality"
)

// prescriptionCurveForChapter 返回装配验收处方用的 L0 基线曲线。
func prescriptionCurveForChapter(ctx context.Context, s *Service, novelID, chapterID int64) []quality.TensionPoint {
	if content, err := git.ReadFile(novelID, git.ChapterPath(chapterID)); err == nil && content != "" {
		return quality.BuildTensionCurve(content)
	}
	// 降级：磁盘不可读（未配置 vault / 章节文件缺失）时用 DB 的 L0 基线行，
	// 不取会被 L1 人工标注覆盖的生效曲线。
	if s == nil || s.qualityStore == nil {
		return nil
	}
	rows, err := s.qualityStore.ListTensionCurve(ctx, novelID, chapterID)
	if err != nil {
		return nil
	}
	l0 := make([]quality.ChapterTension, 0, len(rows))
	for _, r := range rows {
		if r.Source == quality.TensionSourceL0 {
			l0 = append(l0, r)
		}
	}
	return quality.TensionPointsFromRows(l0)
}
