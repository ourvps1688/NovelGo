package pipeline

// #114 P3 静态接线契约：章节检测「点一次生效」与「处方与判定同源」两条链路
// 每一环都必须真实接线，且不许借机复活后台自动修复/自动派发。
// Windows CRLF 归一后再断言（项目既有静态断言纪律）。

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readChapterDetectSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	require.NoError(t, err, "读取接线源文件 %s", rel)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestChapterDetectP3Wiring(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"events.go", []string{
			`EvChapterDetectDone = "pipeline:chapter_detect_done"`, "func EmitChapterDetectDone(",
		}},
		{"service.go", []string{
			"emitChapterDetectDoneSlot", "func emitChapterDetectDone(", "func swapEmitChapterDetectDone(",
		}},
		{"detect.go", []string{
			"AlreadyRunning bool", "tryBeginDetect(", "res.AlreadyRunning = true",
			"emitChapterDetectDone(s.appCtx", "cancelled = ctx.Err() != nil",
		}},
		{"detect_status.go", []string{
			"func (s *Service) tryBeginDetect(", "LoadOrStore(", "prescriptionCurveForChapter(ctx, s, novelID, chapterID)",
		}},
		{"chapter_prescription_curve.go", []string{
			"git.ReadFile", "git.ChapterPath", "quality.BuildTensionCurve", "TensionSourceL0",
		}},
		{"../quality/tension.go", []string{
			"tensionActionCap        = 1.0", "tensionShortRatioCap    = 0.4", "structuralPeakAt       = 0.45",
		}},
		{"../quality/prescription.go", []string{
			"func beatSegmentDiagnostics(", "func beatActionHitsNeeded(", "离达标最近 seg", "不要整章重写",
		}},
		{"../../frontend/src/components/skill/DetectionWorkflowView.tsx", []string{
			"pipeline:chapter_detect_done", "invalidateChapterViews", "already_running",
			"chapterDoneToast", "chapterDoneFailed",
		}},
		{"../../frontend/src/i18n/locales/zh-CN.json", []string{
			"alreadyRunning", "chapterDoneToast", "chapterDoneFailed",
		}},
		{"../../frontend/src/i18n/locales/en.json", []string{
			"alreadyRunning", "chapterDoneToast", "chapterDoneFailed",
		}},
		{"../../frontend/src/lib/wailsjs/go/models.ts", []string{"already_running"}},
	}
	for _, c := range checks {
		src := readChapterDetectSource(t, c.path)
		for _, want := range c.want {
			assert.Contains(t, src, want, "%s 缺少接线 %q", c.path, want)
		}
	}
}

func TestChapterDetectP3NeverAutoFixes(t *testing.T) {
	src := readChapterDetectSource(t, "detect.go") +
		readChapterDetectSource(t, "chapter_prescription_curve.go") +
		readChapterDetectSource(t, "../../frontend/src/components/skill/DetectionWorkflowView.tsx")
	for _, banned := range []string{
		"StartIssueFixTickets", "StartManualIssueFixTickets", "onAutoFix", "autoFix",
		"StartPipeline", "PipelineWizard", "SignabilityAuditView",
	} {
		assert.NotContains(t, src, banned, "P3 链路不得复活后台自动修复/自动派发：%s", banned)
	}
}
