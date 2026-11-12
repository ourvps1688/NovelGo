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
			"emitChapterDetectDone(s.appCtx", "errors.Is(ctxErr, context.Canceled)",
			"chapterDetectFailureReason(", `payload["reason"]`,
			"chapterDetectTimeout", `reason = "timeout"`,
		}},
		{"../../app/handler.go", []string{
			"a.runBackgroundSubAgent", "classifyChapterDetectError(",
		}},
		{"../../app/background_agent.go", []string{
			"ProviderModel(", "a.agent.RunSubAgent(ctx, opts, req)", "DetectReason() string",
		}},
		{"detect_status.go", []string{
			"func (s *Service) tryBeginDetect(", "LoadOrStore(", "prescriptionCurveForChapter(ctx, s, novelID, chapterID)",
			"chapterDetectTimeout < timeout",
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
			"chapterDoneToast", "chapterDoneFailed", "chapterDoneFailedModel",
			"CHAPTER_DETECT_TOAST_ID", "toastError(", "chapterDoneFailedTimeout",
			"agent:0", "CancelDetectionTask", "detectStep", "etaHint",
		}},
		{"../../frontend/src/i18n/locales/zh-CN.json", []string{
			"alreadyRunning", "chapterDoneToast", "chapterDoneFailed",
			"chapterDoneFailedModel", "chapterDoneFailedLLM", "chapterDoneFailedReceipt",
			"chapterDoneFailedTimeout", "etaHint", "progress",
		}},
		{"../../frontend/src/i18n/locales/en.json", []string{
			"alreadyRunning", "chapterDoneToast", "chapterDoneFailed",
			"chapterDoneFailedModel", "chapterDoneFailedLLM", "chapterDoneFailedReceipt",
			"chapterDoneFailedTimeout", "etaHint", "progress",
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

// TestChapterDetectFailureFeedbackIsActionable #115 P0：失败提示必须用户可执行，
// 不能把用户指向看不到的日志，也不能用成功 toast 显示失败。
func TestChapterDetectFailureFeedbackIsActionable(t *testing.T) {
	zh := readChapterDetectSource(t, "../../frontend/src/i18n/locales/zh-CN.json")
	en := readChapterDetectSource(t, "../../frontend/src/i18n/locales/en.json")
	assert.NotContains(t, zh, "详情见日志", "用户没有日志入口，不得把原因甩给日志")
	assert.NotContains(t, en, "see logs", "en 文案同样不得指向日志")
	for _, key := range []string{"chapterDoneFailedModel", "chapterDoneFailedLLM", "chapterDoneFailedReceipt"} {
		assert.Contains(t, zh, key)
		assert.Contains(t, en, key)
	}
	ci := readChapterDetectSource(t, "../mcp_tools/chapter_ci.go")
	assert.NotContains(t, ci, "record.L1Status = quality.L1StatusSkipped",
		"L1 失败不得把上一轮 checked 发现项清空")
	assert.Contains(t, ci, "保留上一轮 L1")
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
