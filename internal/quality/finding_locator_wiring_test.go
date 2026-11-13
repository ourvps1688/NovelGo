package quality

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #120 静态接线契约：第5章《无档》的 ai_flavor 连挂 20/16 轮、pacing_drag
// 挂 9 轮，根因跨三处——advisory 不给命中位置、处方指向不存在的标注、
// 拖沓段处方缺段级实测。这里把「逐处定位 → desc 透出 → 处方同源」锁死，
// 并禁止布尔存在性判定 / 过宽正则 / 后台自动修复回潮。
// Windows CRLF 归一后再断言（既有纪律）。

func readLocatorSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	require.NoError(t, err, "读取接线源文件 %s", rel)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestFindingLocatorWiredEndToEnd(t *testing.T) {
	deslop := readLocatorSource(t, "../text/deslop.go")
	l0 := readLocatorSource(t, "l0.go")
	presc := readLocatorSource(t, "prescription.go")

	for _, want := range []string{
		"advisoryMinHits", `"tell-not-show": 2`,
		"locs := r.re.FindAllStringIndex(s, -1)",
		"Matched: strings.Join(dedupeStrings(matched)",
		"Context: strings.Join(dedupeStrings(contexts)",
		"func dedupeStrings(",
		"regexp.MustCompile(`带着(一丝|一抹|几分)`)",
	} {
		assert.Contains(t, deslop, want, "deslop.go 缺少逐处定位/密度门槛接线 %q", want)
	}
	assert.NotContains(t, deslop, "if r.re.MatchString(s)",
		"布尔存在性判定会让处方再次指向空位置，不得回潮")
	assert.NotContains(t, deslop, "regexp.MustCompile(`，带着|带着",
		"裸逗号通配会把具体感官白描误判成万能状语，不得回潮")

	for _, want := range []string{"it.Context != \"\"", "（所在句："} {
		assert.Contains(t, l0, want, "l0.go 必须把命中所在句透出到发现描述 %q", want)
	}
	for _, want := range []string{
		"dragSegmentDiagnostics(curve, start, end)", "func dragSegmentDiagnostics(",
		"该规则在全章清零", "逐处只改命中句", "只需把其中一段",
	} {
		assert.Contains(t, presc, want, "prescription.go 缺少同源可执行口径 %q", want)
	}

	// 零自动修复 / 零新增执行体：本轮只改判定与处方文本。
	for name, src := range map[string]string{"deslop.go": deslop, "l0.go": l0, "prescription.go": presc} {
		for _, banned := range []string{
			"StartIssueFixTickets", "StartPipeline", "FixRounds", "go func()",
		} {
			assert.NotContains(t, src, banned, "%s 不得引入 %q", name, banned)
		}
	}
}

// TestRealChapterWhiteDescriptionLocatorRegression 用第5章真实落盘片段做跨包
// 回归：正当白描不得再产出去 AI 味发现；明确的套话指纹仍必须产出，且描述里
// 必须带「命中 + 所在句」，让验收处方的 Where 真正有指向。
func TestRealChapterWhiteDescriptionLocatorRegression(t *testing.T) {
	white := "内环的风从码头那头刮过来，带着清洁剂的味，闻着比枪油还生。\n这十五公里，他知道自己这两条腿值多少。"
	_, fs := RunL0Checks(L0Input{Text: white, ReadingNumber: 5})
	for _, f := range fs {
		if f.Check == CheckAIFlavor {
			assert.NotContains(t, f.Desc, "universal-adverb", "具体感官白描不得误报")
			assert.NotContains(t, f.Desc, "tell-not-show", "单处角色经验判断不得误报")
		}
	}

	cliche := cleanBody(3) + "她带着一丝不易察觉的笑意，把表推回来。"
	_, fs2 := RunL0Checks(L0Input{Text: cliche, ReadingNumber: 5})
	found := false
	for _, f := range fs2 {
		if f.Check == CheckAIFlavor && strings.Contains(f.Desc, "universal-adverb") {
			found = true
			assert.Contains(t, f.Desc, "（命中：", "发现描述必须带实际命中片段")
			assert.Contains(t, f.Desc, "（所在句：", "发现描述必须带命中所在句")
		}
	}
	assert.True(t, found, "明确的抽象程度状语指纹仍必须被发现")
}
