package quality

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/voice"
)

// cleanBody 生成一段不含 AI 味规则命中的中性正文（避免 advisory/blocking 词表干扰断言）。
func cleanBody(paragraphs int) string {
	const para = "清晨的风穿过巷口，摊贩支起炉灶，热气顺着屋檐往上飘。青石板上落着昨夜的雨痕，行人挑着担子走过，扁担压出细响。"
	return strings.Repeat(para+"\n", paragraphs)
}

// findingsByCheck 按检查项归拢发现，便于断言。
func findingsByCheck(list []Finding) map[string][]Finding {
	out := map[string][]Finding{}
	for _, f := range list {
		out[f.Check] = append(out[f.Check], f)
	}
	return out
}

// TestRunL0ChecksEmptyChapter 空章：字数 0、无钩、空章发现、C 系顺编 + 作用域。
func TestRunL0ChecksEmptyChapter(t *testing.T) {
	m, findings := RunL0Checks(L0Input{Text: "   \n  ", ReadingNumber: 12})

	assert.Equal(t, 0, m.WordCount)
	assert.False(t, m.HasTailHook)
	assert.NotNil(t, m.StyleMetrics, "指标全集照常返回（空文零值统计）")

	by := findingsByCheck(findings)
	require.Contains(t, by, CheckEmptyChapter, "空章是事实性发现（字数门槛）")
	require.NotContains(t, by, CheckTailHook, "空章不重复报章尾无钩")
	require.NotContains(t, by, CheckStyleDrift, "无基线零漂移发现")

	// C 系顺编 + 契约字段。
	for i, f := range findings {
		assert.Equal(t, "C"+string(rune('1'+i)), f.ID)
		assert.Equal(t, FindingLevelDefault, f.Level, "warning 级（P1）恒定")
		assert.Equal(t, FindingStatusPending, f.Status)
		assert.Equal(t, []int64{12}, f.ChapterIDs, "ReadingNumber>0 时回填作用域")
	}
}

// TestRunL0ChecksWordCount 字数门槛：偏短命中，达标不命中（默认目标 2000×70%）。
func TestRunL0ChecksWordCount(t *testing.T) {
	short := cleanBody(5) + "他推开门走进院子。"
	m, findings := RunL0Checks(L0Input{Text: short})
	by := findingsByCheck(findings)
	assert.Less(t, m.WordCount, int(float64(DefaultTargetWords)*shortWordRatio))
	require.Contains(t, by, CheckWordCount)
	assert.Contains(t, by[CheckWordCount][0].Desc, "偏短")

	// 自定义目标：字数达标则不再报偏短。
	m2, findings2 := RunL0Checks(L0Input{Text: short, TargetWords: 50})
	by2 := findingsByCheck(findings2)
	assert.NotContains(t, by2, CheckWordCount, "达到目标 70%% 即不报偏短")
	assert.Equal(t, m.WordCount, m2.WordCount)
}

// TestRunL0ChecksTailHook 章尾钩子：无信号报发现，悬念词/问句收尾不报。
func TestRunL0ChecksTailHook(t *testing.T) {
	// 无钩：以普通陈述收束，尾窗内无信号词。
	_, findings := RunL0Checks(L0Input{Text: cleanBody(20) + "他推开门走进院子。", TargetWords: 1})
	by := findingsByCheck(findings)
	require.Contains(t, by, CheckTailHook)

	// 有钩：悬念信号词收尾。
	mHook, findingsHook := RunL0Checks(L0Input{Text: cleanBody(20) + "就在这时，门被人从外面推开了。", TargetWords: 1})
	assert.True(t, mHook.HasTailHook)
	assert.NotContains(t, findingsByCheck(findingsHook), CheckTailHook)

	// 有钩：问句收尾。
	mQ, findingsQ := RunL0Checks(L0Input{Text: cleanBody(20) + "他真的要这么做吗？", TargetWords: 1})
	assert.True(t, mQ.HasTailHook)
	assert.NotContains(t, findingsByCheck(findingsQ), CheckTailHook)

	// 信号词在尾窗之外不计（窗=末尾 120 rune）。
	far := "就在这时，门被人推开了。" + strings.Repeat("青石板路一直延伸到巷子深处。", 30)
	mFar, findingsFar := RunL0Checks(L0Input{Text: far, TargetWords: 1})
	assert.False(t, mFar.HasTailHook, "尾窗外的信号词不作数")
	assert.Contains(t, findingsByCheck(findingsFar), CheckTailHook)
}

// TestRunL0ChecksAIFlavor AI 味套话：词表单源命中、题材豁免、rule 归因入 desc。
func TestRunL0ChecksAIFlavor(t *testing.T) {
	// advisory 命中（judgment-cliche）。
	_, findings := RunL0Checks(L0Input{Text: cleanBody(3) + "毫无疑问，这场比试他已经赢了。", TargetWords: 1})
	by := findingsByCheck(findings)
	require.Contains(t, by, CheckAIFlavor)
	assert.Contains(t, by[CheckAIFlavor][0].Desc, "judgment-cliche", "rule 归因入 desc（Summary 可归因）")

	// blocking 命中（em-dash 破折号）。
	_, findingsB := RunL0Checks(L0Input{Text: cleanBody(3) + "推门而入——屋内空无一人。", TargetWords: 1})
	byB := findingsByCheck(findingsB)
	require.Contains(t, byB, CheckAIFlavor)

	// 题材豁免：stock-body-fingertip 在网文题材放行、未选题材保守命中。
	_, withGenre := RunL0Checks(L0Input{Text: cleanBody(3) + "他攥着扶手，指节泛白。", TargetWords: 1, Genre: "动作"})
	for _, f := range findingsByCheck(withGenre)[CheckAIFlavor] {
		assert.NotContains(t, f.Desc, "stock-body-fingertip", "已选网文题材应豁免真人 idiom")
	}
	_, noGenre := RunL0Checks(L0Input{Text: cleanBody(3) + "他攥着扶手，指节泛白。", TargetWords: 1})
	hit := false
	for _, f := range findingsByCheck(noGenre)[CheckAIFlavor] {
		if strings.Contains(f.Desc, "stock-body-fingertip") {
			hit = true
		}
	}
	assert.True(t, hit, "未选题材保守硬拒（保底行为不变）")
}

// TestRunL0ChecksStyleDrift 文风八键漂移：P2 口径复用；无基线零发现；同值基线零发现。
func TestRunL0ChecksStyleDrift(t *testing.T) {
	body := cleanBody(10) + "他推开门走进院子。"

	// 无基线零发现（不造默认基线）。
	m0, findings0 := RunL0Checks(L0Input{Text: body, TargetWords: 1})
	assert.NotContains(t, findingsByCheck(findings0), CheckStyleDrift)

	// 极端基线触发漂移（avg_sent_len 100 vs 当前 ~20）。
	baseline := map[string]float64{voice.MetricKeyAvgSentLen: 100}
	_, findings := RunL0Checks(L0Input{Text: body, TargetWords: 1, Baseline: baseline})
	by := findingsByCheck(findings)
	require.Contains(t, by, CheckStyleDrift)
	assert.Contains(t, by[CheckStyleDrift][0].Desc, voice.MetricKeyAvgSentLen, "desc 含指标名（基线/当前/容差四要素）")
	assert.Contains(t, by[CheckStyleDrift][0].Desc, "基线")
	assert.Contains(t, by[CheckStyleDrift][0].Desc, "容差")

	// 同值基线零漂移（current 本身作基线）。
	_, findingsSame := RunL0Checks(L0Input{Text: body, TargetWords: 1, Baseline: m0.StyleMetrics})
	assert.NotContains(t, findingsByCheck(findingsSame), CheckStyleDrift, "与自身统计一致不构成漂移")
}

// TestRunL0ChecksDeterministic 同输入两次运行结果逐字一致（纯函数）。
func TestRunL0ChecksDeterministic(t *testing.T) {
	in := L0Input{
		Text:          cleanBody(4) + "毫无疑问，他会回来的。",
		ReadingNumber: 3,
		Baseline:      map[string]float64{voice.MetricKeyAvgSentLen: 100},
	}
	m1, f1 := RunL0Checks(in)
	m2, f2 := RunL0Checks(in)
	assert.Equal(t, m1, m2)
	require.NotEmpty(t, f1)
	assert.Equal(t, f1, f2)
}
