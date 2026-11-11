package quality

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #114 静态接线契约：处方必须在「检测 → 档案 DTO → 前端 chat → 判官 → 改稿轮」
// 每一环都真实接线，且不许借机复活后台自动修复。
// Windows CRLF 归一后再断言（.go 静态断言既有纪律）。

func readPrescriptionSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	require.NoError(t, err, "读取接线源文件 %s", rel)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestPrescriptionWiredEndToEnd(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"../pipeline/detect_status.go", []string{
			"Prescriptions []string", "BuildPrescriptions", "TensionSourceL0", "RenderPrescriptions",
		}},
		{"../review/score.go", []string{
			"ScoreChapterWithPrescriptions", "buildUserPromptWithPrescriptions", AcceptanceMarker, "逐条核验",
		}},
		{"../mcp_tools/rw_tools.go", []string{
			"ScoreChapterWithPrescriptions", "prescriptionLinesForChapter",
		}},
		{"../mcp_tools/chapter_prescription.go", []string{
			"GetByChapter", "BuildTensionCurve", "BuildPrescriptions",
		}},
		{"../../app/chat.go", []string{"buildAcceptanceGuard(input.Message)"}},
		{"../../app/finding_prescription_inject.go", []string{
			AcceptanceMarker, "quality.AcceptanceMarker", "严禁整章 full_replace",
		}},
		{"../../frontend/src/components/skill/DetectionWorkflowView.tsx", []string{
			"archive.prescriptions", "acceptanceTitle", "startsWith(`[${f.check}]`)",
		}},
		{"../../frontend/src/i18n/locales/zh-CN.json", []string{"acceptanceTitle", AcceptanceMarker}},
		{"../../frontend/src/i18n/locales/en.json", []string{"acceptanceTitle", AcceptanceMarker}},
		{"../../frontend/src/lib/wailsjs/go/models.ts", []string{"prescriptions: string[]"}},
	}
	for _, c := range checks {
		src := readPrescriptionSource(t, c.path)
		for _, want := range c.want {
			assert.Contains(t, src, want, "%s 缺少接线 %q", c.path, want)
		}
	}
}

func TestPrescriptionNeverAutoFixes(t *testing.T) {
	src := readPrescriptionSource(t, "prescription.go") +
		readPrescriptionSource(t, "reconcile.go")
	for _, banned := range []string{
		"StartIssueFixTickets", "StartManualIssueFixTickets", "autoFix", "onAutoFix",
		"RewriteItem", "FixRounds", "UpsertL0", "ReplaceIssuesByPrefix",
	} {
		assert.NotContains(t, src, banned, "处方链路不得复活后台自动修复/写状态：%s", banned)
	}
}

func TestDetectorAndPrescriptionShareOneSource(t *testing.T) {
	tension := readPrescriptionSource(t, "tension.go")
	checks := readPrescriptionSource(t, "tension_checks.go")
	platform := readPrescriptionSource(t, "platform_checks.go")
	presc := readPrescriptionSource(t, "prescription.go")
	assert.Contains(t, tension, "func beatPeakCount(", "爽点计数必须单源")
	assert.Contains(t, tension, "func structuralSignal(", "结构信号必须单源")
	assert.Contains(t, checks, "beatPeakCount(curve)", "beat_gap 须用单源爽点计数")
	assert.Contains(t, platform, "beatPeakCount(curve)", "beat_density 须用单源爽点计数")
	assert.Contains(t, checks, "不要只在首段堆冲突", "开篇坡度文案方向须与公式一致")
	assert.Contains(t, presc, "tailHookWindow", "处方阈值须引用检测常量")
	assert.Contains(t, presc, "tensionFlatBelow", "处方阈值须引用检测常量")
	assert.Contains(t, presc, "openingSlopeMinRise", "处方阈值须引用检测常量")
	assert.Contains(t, presc, "structuralPeakAt", "处方阈值须引用检测常量")
}

func TestWritingBundleKeepsExecutableChecklist(t *testing.T) {
	src := readPrescriptionSource(t, "../../app/slash_command.go")
	assert.Contains(t, src, "writingBundleCoreSkillCap = 2400",
		"固定 3 技法的可执行自检清单不得被 1500 帽截掉")
	assert.Contains(t, src, "writingBundleBlockCap = 9000",
		"#111 P1 整块帽 5000→9000 勿回退")
	assert.Contains(t, src, "isCoreWritingSkill(name)",
		"条件化技法仍守单技能帽，不得被核心技法挤掉")
}
