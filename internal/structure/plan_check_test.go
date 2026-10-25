package structure

// plan_check_test.go —— 章纲级写前检查 L1.5（R2b-4）确定性对账契约：
// 存在性/预期收章区间/禁忌命中/不变量死亡对账；宁缺勿错跳过路径；确定性。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCheckChapterPlanPitMissing 存在性：引号坑名不在坑表→告警；
// 双向包含算存在；无引号/短名不检查（宁缺勿错）。
func TestCheckChapterPlanPitMissing(t *testing.T) {
	entries := []ForeshadowEntry{
		{ID: 1, Title: "黑衣人身份", Status: "pending", TargetReadingNumber: 20},
	}
	in := PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章回收「黑衣人身份」，并埋下「母亲死因」的新线索。",
		Foreshadows:   entries,
	}
	got := CheckChapterPlan(in)
	require.Len(t, got, 1)
	require.Equal(t, CheckPlanPitMissing, got[0].Check)
	require.Contains(t, got[0].Desc, "「母亲死因」")
	require.Contains(t, got[0].Desc, "不在坑表")
	require.Equal(t, FindingLevelDefault, got[0].Level)

	// 标题双向包含（长短引用）算存在。
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "埋下「黑衣人」的线索。",
		Foreshadows:   entries,
	})
	require.Empty(t, got)

	// 无引号锚点不猜测（宁缺勿错）。
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章埋一个关于母亲的新坑。",
		Foreshadows:   entries,
	})
	require.Empty(t, got)
}

// TestCheckChapterPlanPitWindow 预期收章区间：收坑超宽限/无预期收章、
// 埋坑收点不晚于本章 → 告警；宽限内/意图不明不告警。
func TestCheckChapterPlanPitWindow(t *testing.T) {
	entries := []ForeshadowEntry{
		{ID: 1, Title: "黑衣人身份", Status: "pending", TargetReadingNumber: 3},
		{ID: 2, Title: "母亲死因", Status: "pending", TargetReadingNumber: 0},
		{ID: 3, Title: "三年之约", Status: "pending", TargetReadingNumber: 30},
	}

	// 收坑：本卡第 5 章收预期第 3 章的坑（零宽限超区间）+ 收无预期收章的坑。
	got := CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章回收「黑衣人身份」，并了结「母亲死因」。",
		Foreshadows:   entries,
	})
	require.Len(t, got, 2)
	require.Equal(t, CheckPlanPitWindow, got[0].Check)
	require.Contains(t, got[0].Desc, "超出预期收章区间")
	require.Equal(t, "time_entries:1", got[0].Ref)
	require.Equal(t, CheckPlanPitWindow, got[1].Check)
	require.Contains(t, got[1].Desc, "未设预期收章")

	// 宽限内不告警（|5-3|=2 <= 3）。
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章回收「黑衣人身份」。",
		Foreshadows:   entries,
		WindowGrace:   3,
	})
	require.Empty(t, got)

	// 埋坑：预期收章（30）晚于本章（5）不告警；晚于不了才告警。
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章埋下「三年之约」的伏笔。",
		Foreshadows:   entries,
	})
	require.Empty(t, got)
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章埋下「黑衣人身份」的伏笔。",
		Foreshadows:   entries,
	})
	require.Len(t, got, 1)
	require.Equal(t, CheckPlanPitWindow, got[0].Check)
	require.Contains(t, got[0].Desc, "预期收章须晚于埋设")

	// 意图不明（无收/埋词）不对账。
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章继续推进「黑衣人身份」线。",
		Foreshadows:   entries,
	})
	require.Empty(t, got)

	// 同窗两词（收/埋信号都在窗口内）意图两可不对账（宁缺勿错：错一条误导、漏一条无害）。
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章回收「黑衣人身份」，埋下「三年之约」的伏笔。",
		Foreshadows:   entries,
	})
	require.Empty(t, got, "两可意图跳过区间对账")
}

// TestCheckChapterPlanTaboo 禁忌命中：逐字段逐词出发现；空清单/未命中零发现；
// 重复空词归一。
func TestCheckChapterPlanTaboo(t *testing.T) {
	texts := []PlanField{
		{Name: "hook_task", Text: "本章埋坑「虐主走向」"},
		{Name: "beat_task", Text: "爽点：反杀"},
	}
	got := CheckChapterPlan(PlanCheckInput{ReadingNumber: 1, Texts: texts, Taboos: []string{"虐主", "破墙", "虐主", " "}})
	require.Len(t, got, 1)
	require.Equal(t, CheckPlanTaboo, got[0].Check)
	require.Contains(t, got[0].Desc, "hook_task")
	require.Contains(t, got[0].Desc, "「虐主」")

	got = CheckChapterPlan(PlanCheckInput{ReadingNumber: 1, Texts: texts})
	require.Empty(t, got, "空清单不检查")
}

// TestCheckChapterPlanAlive 不变量死亡对账：窗口内计划死亡→告警（含别名匹配）；
// 窗口已过/无章号锚点/非死亡词/短名不判；custom 不判。
func TestCheckChapterPlanAlive(t *testing.T) {
	chars := []CharacterState{{ID: 1, Name: "林澈", Aliases: []string{"阿澈"}}}
	pins := []PinnedInvariant{
		{ID: 1, Kind: KindAliveBefore, Statement: "林澈第 50 章前不死",
			ParamsJSON: `{"character":"林澈","before_reading_number":50}`},
		{ID: 2, Kind: KindCustom, Statement: "全书不写虐主", ParamsJSON: "{}"},
	}

	// 窗口内（第 5 章）计划死亡：命中（别名）。
	got := CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		Texts:         []PlanField{{Name: "hook_task", Text: "本章让阿澈战死沙场"}},
		Invariants:    pins, Characters: chars,
	})
	require.Len(t, got, 1)
	require.Equal(t, CheckPlanAlive, got[0].Check)
	require.Contains(t, got[0].Desc, "承诺窗口内死亡")
	require.Contains(t, got[0].Desc, "林澈第 50 章前不死")
	require.Equal(t, "pinned_invariants:1", got[0].Ref)

	// 窗口已过（第 50 章）不判。
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 50,
		Texts:         []PlanField{{Name: "hook_task", Text: "本章让林澈战死沙场"}},
		Invariants:    pins, Characters: chars,
	})
	require.Empty(t, got)

	// 无章号锚点（远期计划）不判。
	got = CheckChapterPlan(PlanCheckInput{
		Texts:      []PlanField{{Name: "content", Text: "远期让林澈战死"}},
		Invariants: pins, Characters: chars,
	})
	require.Empty(t, got)

	// 有名字无死亡词不判。
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 5,
		Texts:         []PlanField{{Name: "hook_task", Text: "本章让林澈突破境界"}},
		Invariants:    pins, Characters: chars,
	})
	require.Empty(t, got)

	// 全书不死（before<=0）：任意章号都判。
	allBook := []PinnedInvariant{{ID: 3, Kind: KindAliveBefore, Statement: "主角全书不死",
		ParamsJSON: `{"character":"林澈","before_reading_number":0}`}}
	got = CheckChapterPlan(PlanCheckInput{
		ReadingNumber: 300,
		Texts:         []PlanField{{Name: "hook_task", Text: "终章让林澈牺牲"}},
		Invariants:    allBook, Characters: chars,
	})
	require.Len(t, got, 1)
	require.Equal(t, CheckPlanAlive, got[0].Check)
}

// TestCheckChapterPlanDeterminismAndNumbering 确定性：同输入两次逐字一致；
// 多发现 S 系顺编；空输入零告警；RenderPlanWarnings 空清单空串。
func TestCheckChapterPlanDeterminismAndNumbering(t *testing.T) {
	in := PlanCheckInput{
		ReadingNumber: 5,
		HookText:      "本章回收「黑衣人身份」的真相，另起「母亲死因」线索，全程虐主。",
		Texts: []PlanField{
			{Name: "hook_task", Text: "本章回收「黑衣人身份」的真相，另起「母亲死因」线索，全程虐主。"},
		},
		Foreshadows: []ForeshadowEntry{{ID: 1, Title: "黑衣人身份", Status: "pending", TargetReadingNumber: 3}},
		Taboos:      []string{"虐主"},
	}
	a := CheckChapterPlan(in)
	b := CheckChapterPlan(in)
	require.Equal(t, a, b)
	require.NotEmpty(t, a)
	for i, f := range a {
		require.Equal(t, "S"+string(rune('1'+i)), f.ID)
	}
	require.Equal(t, CheckPlanPitMissing, a[0].Check)
	require.Equal(t, CheckPlanPitWindow, a[1].Check)
	require.Equal(t, CheckPlanTaboo, a[2].Check)

	require.Empty(t, CheckChapterPlan(PlanCheckInput{}))
	require.Empty(t, RenderPlanWarnings(nil))
	require.Empty(t, RenderPlanWarnings([]StructFinding{}))

	block := RenderPlanWarnings(a)
	require.Contains(t, block, "【写前对账告警】")
	require.Contains(t, block, "S1[plan_pit_missing]")
	require.Contains(t, block, "（time_entries:1）")
	require.NotContains(t, block, "（）", "空 Ref 不渲染空括号")
}
