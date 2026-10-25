package structure

// propagation_test.go —— 依赖闭包纯函数测试（R2b-8，提案 §8.4）：
// 种子条目直连、种子章节共引、传递扩展、种子排除、防环、确定性归一、阈值判定。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDependencyClosureEmpty(t *testing.T) {
	require.Nil(t, DependencyClosure(nil, nil, nil), "零边零种子=零依赖")
	require.Nil(t, DependencyClosure([]DepEdge{{EntryRef: "characters:1", ChapterID: 2}}, nil, nil),
		"零种子=零依赖（边不自发传播）")
	require.Nil(t, DependencyClosure(nil, []int64{1}, nil), "零边=零依赖")
	// 坏边不参与（宁缺勿错，不造边）：空条目引用/非法章节 ID。
	bad := []DepEdge{{EntryRef: "", ChapterID: 2}, {EntryRef: "characters:1", ChapterID: 0}}
	require.Nil(t, DependencyClosure(bad, []int64{1}, nil))
}

func TestDependencyClosureSeedChapterSharesEntry(t *testing.T) {
	// ch1 埋伏笔 X（time_entries:1），ch7 回收 X → ch7 是 ch1 的下游依赖。
	edges := []DepEdge{
		{EntryRef: "time_entries:1", ChapterID: 1},
		{EntryRef: "time_entries:1", ChapterID: 7},
	}
	got := DependencyClosure(edges, []int64{1}, nil)
	require.Equal(t, []Dependent{{ChapterID: 7, EntryRefs: []string{"time_entries:1"}}}, got)
}

func TestDependencyClosureSeedEntryPullsBoundChapters(t *testing.T) {
	// 条目 characters:3 被改动（种子条目）→ 绑定它的章节都是受影响章。
	edges := []DepEdge{
		{EntryRef: "characters:3", ChapterID: 4},
		{EntryRef: "characters:3", ChapterID: 9},
	}
	got := DependencyClosure(edges, nil, []string{"characters:3"})
	require.Equal(t, []Dependent{
		{ChapterID: 4, EntryRefs: []string{"characters:3"}},
		{ChapterID: 9, EntryRefs: []string{"characters:3"}},
	}, got)
}

func TestDependencyClosureSeedChaptersExcluded(t *testing.T) {
	// 种子章节永远不进依赖清单（它们已在定点复验作用域内）。
	edges := []DepEdge{
		{EntryRef: "time_entries:1", ChapterID: 1},
		{EntryRef: "time_entries:1", ChapterID: 7},
	}
	got := DependencyClosure(edges, []int64{1, 7}, nil)
	require.Empty(t, got, "两种子共引同一条目，无外部依赖")
	// 种子条目绑定的种子章节排除；同条目绑定的非种子章仍是受影响依赖。
	got = DependencyClosure(edges, []int64{7}, []string{"time_entries:1"})
	require.Equal(t, []Dependent{{ChapterID: 1, EntryRefs: []string{"time_entries:1"}}}, got)
}

func TestDependencyClosureTransitive(t *testing.T) {
	// ch1 -X- ch2 -Y- ch3：改 ch1 → 传递闭包 {ch2, ch3}。
	edges := []DepEdge{
		{EntryRef: "time_entries:1", ChapterID: 1},
		{EntryRef: "time_entries:1", ChapterID: 2},
		{EntryRef: "time_entries:2", ChapterID: 2},
		{EntryRef: "time_entries:2", ChapterID: 3},
	}
	got := DependencyClosure(edges, []int64{1}, nil)
	require.Equal(t, []Dependent{
		{ChapterID: 2, EntryRefs: []string{"time_entries:1", "time_entries:2"}},
		{ChapterID: 3, EntryRefs: []string{"time_entries:2"}},
	}, got, "传递扩展：依赖章共引的条目拉出更远依赖（归因含全部共引条目）")
}

func TestDependencyClosureCycleSafe(t *testing.T) {
	// 环：ch1 -X- ch2 -Y- ch1 与自环条目——visited 集防死循环，种子不回流。
	edges := []DepEdge{
		{EntryRef: "time_entries:1", ChapterID: 1},
		{EntryRef: "time_entries:1", ChapterID: 2},
		{EntryRef: "time_entries:2", ChapterID: 2},
		{EntryRef: "time_entries:2", ChapterID: 1},
		{EntryRef: "time_entries:3", ChapterID: 2}, // 自环：只绑 ch2
	}
	got := DependencyClosure(edges, []int64{1}, nil)
	require.Equal(t, []Dependent{
		{ChapterID: 2, EntryRefs: []string{"time_entries:1", "time_entries:2", "time_entries:3"}},
	}, got, "环不死循环；共引条目归因升序去重")
}

func TestDependencyClosureMultiLinkAttribution(t *testing.T) {
	// ch2 经两条条目进入闭包 → 两条归因都记（供提示词列出全部共引条目）。
	edges := []DepEdge{
		{EntryRef: "characters:1", ChapterID: 1},
		{EntryRef: "characters:1", ChapterID: 2},
		{EntryRef: "time_entries:9", ChapterID: 1},
		{EntryRef: "time_entries:9", ChapterID: 2},
	}
	got := DependencyClosure(edges, []int64{1}, nil)
	require.Equal(t, []Dependent{
		{ChapterID: 2, EntryRefs: []string{"characters:1", "time_entries:9"}},
	}, got)
}

func TestDependencyClosureDeterministic(t *testing.T) {
	edges := []DepEdge{
		{EntryRef: "time_entries:1", ChapterID: 3},
		{EntryRef: "time_entries:1", ChapterID: 1},
		{EntryRef: "time_entries:2", ChapterID: 5},
		{EntryRef: "time_entries:2", ChapterID: 3},
		{EntryRef: "time_entries:1", ChapterID: 3}, // 重复边
	}
	shuffled := []DepEdge{edges[4], edges[2], edges[0], edges[3], edges[1]}
	a := DependencyClosure(edges, []int64{1}, []string{"time_entries:2"})
	b := DependencyClosure(shuffled, []int64{1}, []string{"time_entries:2"})
	require.Equal(t, a, b, "输出与输入顺序无关（确定性）")
	require.Equal(t, []Dependent{
		{ChapterID: 3, EntryRefs: []string{"time_entries:1", "time_entries:2"}},
		{ChapterID: 5, EntryRefs: []string{"time_entries:2"}},
	}, a, "依赖按章节 ID 升序、共引条目升序去重")
}

func TestPropagationEscalates(t *testing.T) {
	makeDeps := func(n int) []Dependent {
		out := make([]Dependent, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, Dependent{ChapterID: int64(i + 2)})
		}
		return out
	}
	require.False(t, PropagationEscalates(makeDeps(0)))
	require.False(t, PropagationEscalates(makeDeps(8)), "8 章=阈值内（>8 才升级）")
	require.True(t, PropagationEscalates(makeDeps(9)), "9 章=超阈值，结构性牵连")
}
