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

// TestRunL0ChecksTailHookRequired 题材卡参数（R2b-3）：显式 false 跳过章尾钩子检查
// （指标照常计算）；nil=缺省 true=现行行为逐字一致。
func TestRunL0ChecksTailHookRequired(t *testing.T) {
	body := cleanBody(20) + "他推开门走进院子。"

	// nil（缺省）：照报缺钩（现行行为）。
	_, findingsNil := RunL0Checks(L0Input{Text: body, TargetWords: 1})
	assert.Contains(t, findingsByCheck(findingsNil), CheckTailHook, "nil=缺省 true=现行行为")

	// 显式 false：跳过钩子检查（其余检查照常）。
	f := false
	mOff, findingsOff := RunL0Checks(L0Input{Text: body, TargetWords: 1, TailHookRequired: &f})
	assert.False(t, mOff.HasTailHook, "指标照常计算")
	assert.NotContains(t, findingsByCheck(findingsOff), CheckTailHook, "显式 false 跳过钩子检查")

	// 显式 true：照报。
	on := true
	_, findingsOn := RunL0Checks(L0Input{Text: body, TargetWords: 1, TailHookRequired: &on})
	assert.Contains(t, findingsByCheck(findingsOn), CheckTailHook)
}

// TestRunL0ChecksTaboo 题材卡禁忌清单（R2b-3）：命中逐词出发现、空清单不检查
// （缺省逐字一致）、去重去空归一。
func TestRunL0ChecksTaboo(t *testing.T) {
	body := cleanBody(20) + "这一路虐主的展开让读者压抑。破墙的设定也冒了出来。"

	// 空清单=不检查=现行行为。
	_, findingsNone := RunL0Checks(L0Input{Text: body, TargetWords: 1})
	assert.NotContains(t, findingsByCheck(findingsNone), CheckTaboo, "空清单不检查")

	// 命中逐词出发现（清单序）。
	_, findings := RunL0Checks(L0Input{Text: body, TargetWords: 1, TabooList: []string{"虐主", "破墙"}})
	by := findingsByCheck(findings)
	require.Len(t, by[CheckTaboo], 2)
	assert.Contains(t, by[CheckTaboo][0].Desc, "「虐主」")
	assert.Contains(t, by[CheckTaboo][1].Desc, "「破墙」")
	assert.Contains(t, by[CheckTaboo][0].Desc, "题材卡")

	// 去重去空归一：重复/空白词不重复报。
	_, findingsDedup := RunL0Checks(L0Input{Text: body, TargetWords: 1,
		TabooList: []string{" 虐主 ", "", "虐主", "  "}})
	byDedup := findingsByCheck(findingsDedup)
	require.Len(t, byDedup[CheckTaboo], 1, "去重去空后单条")

	// 未命中零发现。
	_, findingsMiss := RunL0Checks(L0Input{Text: body, TargetWords: 1, TabooList: []string{"系统流"}})
	assert.NotContains(t, findingsByCheck(findingsMiss), CheckTaboo)

	// 空正文不扫（空章路径不叠禁忌发现）。
	_, findingsEmpty := RunL0Checks(L0Input{Text: "  ", TabooList: []string{"虐主"}})
	assert.NotContains(t, findingsByCheck(findingsEmpty), CheckTaboo)
}

// TestRunL0ChecksNameDrift 称呼混用（R2b-5）：同一角色 ≥2 种称呼形式同章出现→告警
// （命中形式保序、正名先于别名）；单一形式/被长形式吞没/短名/无台账零发现（缺省逐字一致）。
func TestRunL0ChecksNameDrift(t *testing.T) {
	chars := []L0Character{{Name: "林澈", Aliases: []string{"阿澈", "林公子"}}}

	// 混用：正名+别名同章出现。
	_, findings := RunL0Checks(L0Input{
		Text: cleanBody(3) + "林澈握紧了剑。阿澈知道这一战避无可避。", TargetWords: 1, Characters: chars,
	})
	by := findingsByCheck(findings)
	require.Len(t, by[CheckNameDrift], 1)
	assert.Contains(t, by[CheckNameDrift][0].Desc, "「林澈」")
	assert.Contains(t, by[CheckNameDrift][0].Desc, "林澈、阿澈", "命中形式保序（正名先于别名）")

	// 单一形式不告警。
	_, one := RunL0Checks(L0Input{
		Text: cleanBody(3) + "林澈握紧了剑。", TargetWords: 1, Characters: chars,
	})
	assert.NotContains(t, findingsByCheck(one), CheckNameDrift)

	// 长形式吞没短形式：只出现「林澈哥哥」不算混用（宁缺勿错）。
	long := []L0Character{{Name: "林澈", Aliases: []string{"林澈哥哥"}}}
	_, sub := RunL0Checks(L0Input{
		Text: cleanBody(3) + "林澈哥哥握紧了剑。", TargetWords: 1, Characters: long,
	})
	assert.NotContains(t, findingsByCheck(sub), CheckNameDrift)

	// 短名（<2 rune）不参与匹配。
	short := []L0Character{{Name: "林", Aliases: []string{"澈"}}}
	_, none := RunL0Checks(L0Input{Text: cleanBody(3) + "林和澈都来了。", TargetWords: 1, Characters: short})
	assert.NotContains(t, findingsByCheck(none), CheckNameDrift)

	// 无台账输入=零发现（缺省=现行行为逐字一致）。
	_, empty := RunL0Checks(L0Input{Text: cleanBody(3) + "林澈和阿澈都来了。", TargetWords: 1})
	assert.NotContains(t, findingsByCheck(empty), CheckNameDrift)
}

// TestRunL0ChecksDeadAppearance 死人出镜（R2b-5）：已死角色名/别名在正文出现→告警
// （每角色一条、正名先归因）；在世/未标注/未出现零告警。
func TestRunL0ChecksDeadAppearance(t *testing.T) {
	dead := false
	alive := true
	chars := []L0Character{
		{Name: "赵铁衣", Aliases: []string{"铁衣"}, Alive: &dead},
		{Name: "林澈", Aliases: []string{"阿澈"}, Alive: &alive},
		{Name: "柳氏"}, // nil=未标注
	}

	// 已死角色出镜：正名+别名都命中也只报一条（正名先归因）。
	_, findings := RunL0Checks(L0Input{
		Text: cleanBody(3) + "赵铁衣的传说仍在。铁衣若在，必不会坐视。", TargetWords: 1, Characters: chars,
	})
	by := findingsByCheck(findings)
	require.Len(t, by[CheckDeadAppearance], 1, "同一角色多形式命中只报一条")
	assert.Contains(t, by[CheckDeadAppearance][0].Desc, "「赵铁衣」")
	assert.Contains(t, by[CheckDeadAppearance][0].Desc, "已死")

	// 别名单独命中：按别名归因。
	_, aliasOnly := RunL0Checks(L0Input{
		Text: cleanBody(3) + "铁衣的名字没人再提起。", TargetWords: 1, Characters: chars,
	})
	byAlias := findingsByCheck(aliasOnly)
	require.Len(t, byAlias[CheckDeadAppearance], 1)
	assert.Contains(t, byAlias[CheckDeadAppearance][0].Desc, "「铁衣」")

	// 在世/未标注出镜不告警。
	_, clean := RunL0Checks(L0Input{
		Text: cleanBody(3) + "林澈和阿澈、柳氏一起喝茶。", TargetWords: 1, Characters: chars,
	})
	assert.NotContains(t, findingsByCheck(clean), CheckDeadAppearance)

	// 已死角色未出现不告警。
	_, absent := RunL0Checks(L0Input{Text: cleanBody(3) + "路上没有行人。", TargetWords: 1, Characters: chars})
	assert.NotContains(t, findingsByCheck(absent), CheckDeadAppearance)
}
