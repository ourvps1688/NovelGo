package quality

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// skill_effect_wiring_test.go —— #116 P1 静态接线闸。
// 锁住「改稿轮发现项快照 → 最新一轮收敛快照 → 纯本地只读聚合 → Wails 出口
// → 前端消费面 → 中英双语文案」全链；并明令零自动修复、零上传、零写侧执行体。
// 与 observability_wiring_test.go 同风格：读源码断言（Windows CRLF 归一）。

func readSkillEffectSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读取源码失败 %s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestSkillEffectWiredEndToEnd(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"finding_round.go", []string{"Resolved  []string `json:\"resolved\"`", "Persisted []string `json:\"persisted\"`"}},
		{"../activity/skill_invocation.go", []string{"FindingKeys string", "TurnID      int"}},
		{"../activity/skill_effect.go", []string{
			"SkillEffectStat", "SummarizeSkillEffects",
			"Table(\"chapter_quality\")", `"resolved"`, `"persisted"`,
			"skill_invocations", "finding_keys <> ''", "seen[dedup]",
		}},
		{"../../app/activity.go", []string{"GetSkillEffectStats", "SummarizeSkillEffects"}},
		{"../../frontend/src/components/skill/SkillEffectPanel.tsx", []string{
			"GetSkillEffectStats", `["feature-usage"`, "staleTime: 0",
			"skill-effect-panel", "skillMatrix.effects.hint",
			"skillMatrix.effects.rateValue", "skillMatrix.effects.noSnapshot",
		}},
		{"../../frontend/src/components/skill/SkillMatrixView.tsx", []string{
			"SkillEffectPanel", "<SkillEffectPanel novelId={novelId} />",
		}},
		{"../../frontend/src/lib/wailsjs/go/app/App.js", []string{"GetSkillEffectStats"}},
		{"../../frontend/src/lib/wailsjs/go/app/App.d.ts", []string{"GetSkillEffectStats", "activity.SkillEffectStat"}},
		{"../../frontend/src/lib/wailsjs/go/models.ts", []string{
			"export class SkillEffectStat", "rounds: number;", "resolved: number;", "persisted: number;",
		}},
		{"../../frontend/src/i18n/locales/zh-CN.json", []string{
			`"effects"`, "最近一轮近似", "roundsTimes", "rateValue", "noSnapshot",
		}},
		{"../../frontend/src/i18n/locales/en.json", []string{
			`"effects"`, "latest-round approximation", "roundsTimes", "rateValue", "noSnapshot",
		}},
	}
	for _, c := range checks {
		src := readSkillEffectSource(t, c.path)
		for _, want := range c.want {
			assert.Contains(t, src, want, "%s 缺少接线符号 %q", c.path, want)
		}
	}
}

func TestSkillEffectNeverAutoFixesOrUploads(t *testing.T) {
	banned := []string{
		"StartIssueFixTickets", "StartManualIssueFixTickets",
		"taskKindIssueFix", "onAutoFix", "autoFix", "RewriteItem",
		"http.Post", "http.Get", "fetch(",
	}
	for _, rel := range []string{
		"../activity/skill_effect.go",
		"../../app/activity.go",
		"../../frontend/src/components/skill/SkillEffectPanel.tsx",
	} {
		src := readSkillEffectSource(t, rel)
		for _, b := range banned {
			assert.NotContains(t, src, b, "%s 不得出现自动修复/上传符号 %q", rel, b)
		}
	}
	// 度量只读：聚合里不得出现任何写库动作。
	agg := readSkillEffectSource(t, "../activity/skill_effect.go")
	for _, b := range []string{"Create(", "Save(", "Updates(", "Delete(", "Clauses("} {
		assert.NotContains(t, agg, b, "技能效果聚合必须纯只读，不得写库 %q", b)
	}
	// 口径诚实：面板说明必须标注「最近一轮近似」，不得声称逐轮历史。
	panel := readSkillEffectSource(t, "../../frontend/src/components/skill/SkillEffectPanel.tsx")
	assert.Contains(t, panel, "最近一轮近似")
	assert.NotContains(t, panel, "Date.now()")
	assert.NotContains(t, panel, "new Date()")
	// 退役死链不得重新产生假用量。
	retired := readSkillEffectSource(t, "../pipeline/rewrite_skills.go")
	assert.NotContains(t, retired, "RecordSkillInvocation")
	assert.Contains(t, retired, "#116 退役状态")
}
