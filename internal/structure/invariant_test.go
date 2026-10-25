package structure

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func alive(v bool) *bool { return &v }

// TestPinnedInvariantValidate 契约拒收：kind 非法 / 声明空 / 规则 kind 参数缺项。
func TestPinnedInvariantValidate(t *testing.T) {
	ok := PinnedInvariant{NovelID: 1, Kind: KindAliveBefore, Statement: " 主角第 50 章前不死 ",
		ParamsJSON: `{"character":"主角","before_reading_number":50}`}
	require.NoError(t, ok.Validate())
	require.Equal(t, "主角第 50 章前不死", ok.Statement, "声明原文 TrimSpace 归一")

	bad := []PinnedInvariant{
		{Kind: "unknown_kind", Statement: "x"},
		{Kind: KindCustom, Statement: "  "},
		{Kind: KindAliveBefore, Statement: "x", ParamsJSON: `{}`},
		{Kind: KindCapabilityCeiling, Statement: "x", ParamsJSON: `{"character":"主角"}`}, // 缺 ceiling
		{Kind: KindCustom, Statement: "x", ParamsJSON: `不是 json`},
	}
	for i := range bad {
		require.Error(t, bad[i].Validate(), "case %d 应拒收", i)
	}

	custom := PinnedInvariant{Kind: KindCustom, Statement: "全书不出现现代科技", ParamsJSON: ""}
	require.NoError(t, custom.Validate())
	require.Equal(t, "{}", custom.ParamsJSON, "空 params 归一为对象")
}

// TestParseInvariantParams 参数解析：坏 JSON / 缺必填 → ok=false（宁缺勿错）。
func TestParseInvariantParams(t *testing.T) {
	p, ok := ParseAliveBeforeParams(`{"character":" 主角 ","before_reading_number":12}`)
	require.True(t, ok)
	require.Equal(t, "主角", p.Character)
	require.Equal(t, 12, p.BeforeReadingNumber)

	_, ok = ParseAliveBeforeParams(`{坏 json}`)
	require.False(t, ok)
	_, ok = ParseAliveBeforeParams(`{"before_reading_number":12}`)
	require.False(t, ok, "缺角色名不猜")

	cp, ok := ParseCapabilityCeilingParams(`{"character":"反派","ceiling":"9000"}`)
	require.True(t, ok)
	require.Equal(t, "9000", cp.Ceiling)
	_, ok = ParseCapabilityCeilingParams(`{"character":"反派","ceiling":"  "}`)
	require.False(t, ok)
}

// TestCheckPinnedInvariantsAliveBefore 存活不变量：死亡且窗口未过=违例；窗口已过/
// 仍在世/未标注/角色不在台账=不判。
func TestCheckPinnedInvariantsAliveBefore(t *testing.T) {
	chars := []CharacterState{
		{ID: 1, Name: "主角", Alive: alive(false)},
		{ID: 2, Name: "女主", Alive: alive(true)},
		{ID: 3, Name: "反派", Alive: nil},
		{ID: 4, Name: "小七", Aliases: []string{"七爷"}, Alive: alive(false)},
	}
	pins := []PinnedInvariant{
		{ID: 1, Kind: KindAliveBefore, Statement: "主角第 50 章前不死",
			ParamsJSON: `{"character":"主角","before_reading_number":50}`},
		{ID: 2, Kind: KindAliveBefore, Statement: "女主第 5 章前不死",
			ParamsJSON: `{"character":"女主","before_reading_number":5}`},
		{ID: 3, Kind: KindAliveBefore, Statement: "反派第 8 章前不死",
			ParamsJSON: `{"character":"反派","before_reading_number":8}`},
		{ID: 4, Kind: KindAliveBefore, Statement: "小七全书不死",
			ParamsJSON: `{"character":"小七","before_reading_number":0}`},
		{ID: 5, Kind: KindAliveBefore, Statement: "七爷第 3 章前不死",
			ParamsJSON: `{"character":"七爷","before_reading_number":3}`},
		{ID: 6, Kind: KindAliveBefore, Statement: "路人第 2 章前不死",
			ParamsJSON: `{"character":"路人","before_reading_number":2}`},
	}
	// now=20：主角死于承诺 50 章前（违例）；小七全书不死（违例，别名匹配）；
	// 七爷别名匹配但第 3 章窗口已过（不判）；女主在世/反派未标注/路人不在台账（不判）。
	got := CheckPinnedInvariants(pins, chars, 20)
	require.Len(t, got, 2)
	require.Equal(t, "S1", got[0].ID)
	require.Equal(t, KindAliveBefore, got[0].Check)
	require.Equal(t, "pinned_invariants:1", got[0].Ref)
	require.Contains(t, got[0].Desc, "主角第 50 章前不死")
	require.Contains(t, got[0].Desc, "早于第 50 章前不死的承诺")
	require.Equal(t, "pinned_invariants:4", got[1].Ref)
	require.Contains(t, got[1].Desc, "违反全书不死承诺")
}

// TestCheckPinnedInvariantsCeiling 战力上限：数值可比且超限=违例；自由文本/未超限/custom=不判。
func TestCheckPinnedInvariantsCeiling(t *testing.T) {
	chars := []CharacterState{
		{ID: 1, Name: "主角", CapabilityCeiling: "12000"},
		{ID: 2, Name: "法师", CapabilityCeiling: "禁咒级"},
	}
	pins := []PinnedInvariant{
		{ID: 1, Kind: KindCapabilityCeiling, Statement: "力量体系上限 9000",
			ParamsJSON: `{"character":"主角","ceiling":"9000"}`},
		{ID: 2, Kind: KindCapabilityCeiling, Statement: "法师上限 100",
			ParamsJSON: `{"character":"法师","ceiling":"100"}`},
		{ID: 3, Kind: KindCapabilityCeiling, Statement: "主角下限 20000",
			ParamsJSON: `{"character":"主角","ceiling":"20000"}`},
		{ID: 4, Kind: KindCustom, Statement: "全书不出现现代科技", ParamsJSON: "{}"},
	}
	got := CheckPinnedInvariants(pins, chars, 30)
	require.Len(t, got, 1, "仅数值可比且超限违例；自由文本与未超限不判；custom 不判")
	require.Equal(t, KindCapabilityCeiling, got[0].Check)
	require.Equal(t, "pinned_invariants:1", got[0].Ref)
	require.Contains(t, got[0].Desc, "超过钉住上限（9000）")
}

// TestCheckPinnedInvariantsDeterminism 确定性：乱序输入产出逐字一致（按 ID 定序编号）。
func TestCheckPinnedInvariantsDeterminism(t *testing.T) {
	chars := []CharacterState{{ID: 1, Name: "主角", Alive: alive(false)}}
	pins := []PinnedInvariant{
		{ID: 9, Kind: KindAliveBefore, Statement: "B", ParamsJSON: `{"character":"主角","before_reading_number":0}`},
		{ID: 2, Kind: KindAliveBefore, Statement: "A", ParamsJSON: `{"character":"主角","before_reading_number":0}`},
	}
	a := CheckPinnedInvariants(pins, chars, 5)
	b := CheckPinnedInvariants([]PinnedInvariant{pins[1], pins[0]}, chars, 5)
	require.Equal(t, a, b)
	require.Equal(t, []string{"S1", "S2"}, []string{a[0].ID, a[1].ID})
	require.Equal(t, []string{"pinned_invariants:2", "pinned_invariants:9"}, []string{a[0].Ref, a[1].Ref})
}

// TestMergeFindings 跨检查器合并：组序保持、编号连续唯一；空组合并为空。
func TestMergeFindings(t *testing.T) {
	require.Empty(t, MergeFindings())
	require.Empty(t, MergeFindings(nil, []StructFinding{}))

	g1 := []StructFinding{{ID: "S1", Check: CheckForeshadowOverdue, Ref: "time_entries:1"},
		{ID: "S2", Check: CheckForeshadowNoTarget, Ref: "time_entries:2"}}
	g2 := []StructFinding{{ID: "S1", Check: KindAliveBefore, Ref: "pinned_invariants:1"}}
	merged := MergeFindings(g1, g2)
	require.Len(t, merged, 3)
	require.Equal(t, []string{"S1", "S2", "S3"}, []string{merged[0].ID, merged[1].ID, merged[2].ID})
	require.Equal(t, []string{"time_entries:1", "time_entries:2", "pinned_invariants:1"},
		[]string{merged[0].Ref, merged[1].Ref, merged[2].Ref})
}
