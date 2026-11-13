package quality

// #116 静态接线契约：写前硬约束必须真的进常驻 system；开篇加压的判定 /
// 处方 / 图表摘要 / 去重必须共用同一份口径；技能文案不得另写判定式数字；
// 整条链路零自动修复、零新增执行体。
// Windows CRLF 归一后再断言（沿用 prescription_wiring_test.go 的既有纪律）。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWritingConstraintsWiredIntoProfile(t *testing.T) {
	profile := readPrescriptionSource(t, "../agentcfg/novel_profile.go")
	assert.Contains(t, profile, "quality.WritingConstraints()",
		"写前硬约束必须注入常驻 system，否则写作侧仍不知道 L0 判定式")
	assert.Contains(t, profile, "ChapterWords(ctx, novelID)", "字数单源不得被替换")
	assert.Less(t, strings.Index(profile, "quality.WritingConstraints()"),
		strings.Index(profile, "b.WriteString(setText)"),
		"硬约束须排在设定/偏好自由文本之前，避免被旧卡数字盖过")
}

func TestOpeningSlopeOneSourceAcrossDetectorPrescriptionSummary(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"tension_checks.go", []string{
			"func openingSlopeMetrics(", "func openingSlopeOK(",
			"openingPressureMin = tensionFlatBelow", "func dedupeOpeningFindings(",
		}},
		{"prescription.go", []string{
			"openingSlopeMetrics(curve)", "openingPressureMin", "openingSlopeMinRise",
			"curveRange(curve", "beatActionHitsNeeded()",
		}},
		{"tension_block.go", []string{
			"openingSlopeMetrics(slopePts)", "openingSlopeOK(rise, peak)", "TensionSourceL0",
		}},
		{"l0.go", []string{"dedupeOpeningFindings(raw)"}},
		{"writing_constraints.go", []string{
			"func WritingConstraints()", "openingSlopeWindowRunes", "openingPressureMin",
			"openingSlopeMinRise", "tensionActionCap", "tensionShortRatioCap",
			"structuralPeakAt", "tailHookWindow",
		}},
		{"../../internal/skill/builtin/opening-chapters.md", []string{
			"与检测 L0 同源", "【写前硬约束】", "已废弃",
		}},
	}
	for _, c := range checks {
		src := readPrescriptionSource(t, c.path)
		for _, want := range c.want {
			assert.Contains(t, src, want, "%s 缺少接线 %q", c.path, want)
		}
	}
}

func TestOpeningSlopeNoLegacyFormulaOrAutoFix(t *testing.T) {
	checks := readPrescriptionSource(t, "tension_checks.go")
	assert.NotContains(t, checks, "segs[1].Tension - segs[0].Tension",
		"旧「末段−首段」公式不得复活")
	skill := readPrescriptionSource(t, "../../internal/skill/builtin/opening-chapters.md")
	for _, banned := range []string{"升幅 ≥ 5.0", "tension ≥25", "500 字窗口按 400 字"} {
		assert.NotContains(t, skill, banned, "技能不得另写判定式数字：%s", banned)
	}
	for _, path := range []string{"writing_constraints.go", "../agentcfg/novel_profile.go"} {
		src := readPrescriptionSource(t, path)
		for _, banned := range []string{
			"StartIssueFixTickets", "StartManualIssueFixTickets", "autoFix",
			"RunSubAgent", "UpsertL0", "ReplaceIssuesByPrefix",
		} {
			assert.NotContains(t, src, banned, "%s 不得复活后台自动修复/派发：%s", path, banned)
		}
	}
}
