package pipeline

// #115 P0 显式重判分静态契约：局部改稿 / 重新检测都不写 chapter_score_events，
// 用户点「重新判分」是唯一补记录的入口。必须真实接线、与评分门禁同源、
// append-only，且不得借机复活后台自动修复或自动派发。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRescoreChapterWiring(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"rescore.go", []string{
			"func RescoreChapter(", "git.ChapterPath(", "git.OutlinePath(",
			"text.ComputeStats", "ChapterWords(ctx, novelID)", "quality.BuildPrescriptions(",
			"ScoreChapterWithPrescriptions(", "ScoreSourceRescore", "AppendScoreEvent(",
			"quality_score", "JudgeToAuditScores(",
		}},
		{"../quality/score_event.go", []string{`ScoreSourceRescore     = "rescore"`}},
		{"../../app/detection.go", []string{
			"func (a *App) RescoreChapter(", "a.parseModelKey(", "pipeline.RescoreChapter(",
		}},
		{"../../frontend/src/components/skill/ScoreTrendPanel.tsx", []string{
			"RescoreChapter(", "historyHint", "rescoreDone", "score-history", "score-trend",
		}},
		{"../../frontend/src/lib/wailsjs/go/app/App.js", []string{"RescoreChapter"}},
		{"../../frontend/src/lib/wailsjs/go/app/App.d.ts", []string{"RescoreChapter"}},
		{"../../frontend/src/i18n/locales/zh-CN.json", []string{
			"historyHint", "rescoreDone", "rescorePending", `"rescore": "重新判分"`,
		}},
		{"../../frontend/src/i18n/locales/en.json", []string{
			"historyHint", "rescoreDone", "rescorePending",
		}},
	}
	for _, c := range checks {
		src := readChapterDetectSource(t, c.path)
		for _, want := range c.want {
			assert.Contains(t, src, want, "%s 缺少重判分接线 %q", c.path, want)
		}
	}
	// 显式入口不得顺手复活自动修复 / 自动派发，也不得改正文。
	rescore := readChapterDetectSource(t, "rescore.go")
	for _, banned := range []string{
		"StartIssueFixTickets", "StartManualIssueFixTickets", "autoFix",
		"StartPipeline", "git.WriteFile", "ScheduleChapterCI",
	} {
		assert.NotContains(t, rescore, banned, "重判分必须是只读-评分-记账，不得 %s", banned)
	}
	// edit 评分门禁的阻断语义不能被放宽到局部改稿上。
	rw := readChapterDetectSource(t, "../mcp_tools/rw_tools.go")
	assert.Contains(t, rw, `a.ChangeType != "full_replace"`,
		"成章写评分门禁不得放宽（重判分是独立显式入口）")
}

// TestRescoreChapterRejectsIncompleteInput 缺依赖/缺参数时如实报错，不静默造记录。
func TestRescoreChapterRejectsIncompleteInput(t *testing.T) {
	_, err := RescoreChapter(context.Background(), nil, nil, nil, "", "", 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未就绪")
}
