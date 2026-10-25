package structure

// propagation.go —— 修复传播检查的依赖闭包计算（R2b-8，提案 §8.4 组件 C）。
//
// 依赖边来自台账：章节 ch 引用台账条目 X → 引用 X 的章节是 ch 的下游依赖
// （伏笔/时间线/人物状态表同理）。修复/重写的副作用靠依赖闭包定向复验，
// 不做全量复评也不裸奔；闭包超阈值（>8 章）= 结构性牵连，升级 L2 深审/人工，不硬修。
//
// 纯函数零依赖：边装配由调用方（pipeline）完成；确定性（边/种子归一排序、
// 传递扩展定序、输出升序），同输入同输出。宁缺勿错：空条目引用/非法章节 ID 的
// 边不参与（不造边）；闭包只扩大验证面、不产生任何写入。

import (
	"sort"
	"strings"
)

// PropagationClosureCap 是依赖闭包阈值（提案 §8.4「超阈值（如 >8 章）」）：
// 闭包超过该章数 = 结构性牵连，升级 L2 深审/人工裁决，不自动硬修。
const PropagationClosureCap = 8

// DepEdge 是章节↔台账条目的一条引用边：章节 ChapterID 引用台账条目 EntryRef
// （"表名:id"，与 ledger_provenance/ledger_change_history 同款泛化定位）。
type DepEdge struct {
	EntryRef  string
	ChapterID int64
}

// Dependent 是闭包内一个下游依赖章：章节 + 共引条目（提示词归因）。
type Dependent struct {
	ChapterID int64
	// EntryRefs 是把该章牵入闭包的共引台账条目（"表名:id"，升序去重）：
	// 闭包内被激活（种子或连带激活）且绑定该章的条目全部计入。
	EntryRefs []string
}

// DependencyClosure 计算改动的传播依赖闭包：
//   - 种子章节（改动/修复过的章节）→ 共引台账条目的其他章节；
//   - 种子条目（改动过的台账条目）→ 绑定该条目的章节；
//   - 传递扩展：依赖章共引的其他条目 → 更远的依赖章（visited 集防环）。
//
// 返回下游依赖章（不含种子章节），按章节 ID 升序；每章附进入闭包的共引条目
// （升序去重）。确定性：输出与 edges/种子输入顺序无关（内部归一排序）。
func DependencyClosure(edges []DepEdge, seedChapters []int64, seedEntries []string) []Dependent {
	// 边归一：条目→章节集、章节→条目集（双向索引，去重后排序保证确定性遍历序）。
	chaptersOf := map[string][]int64{}
	entriesOf := map[int64][]string{}
	seenEdge := map[DepEdge]bool{}
	for _, e := range edges {
		ref := strings.TrimSpace(e.EntryRef)
		if ref == "" || e.ChapterID <= 0 {
			continue // 空条目引用/非法章节 ID 的边不参与（宁缺勿错，不造边）
		}
		key := DepEdge{EntryRef: ref, ChapterID: e.ChapterID}
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true
		chaptersOf[ref] = append(chaptersOf[ref], e.ChapterID)
		entriesOf[e.ChapterID] = append(entriesOf[e.ChapterID], ref)
	}
	for ref := range chaptersOf {
		chaptersOf[ref] = sortedUniqueInt64(chaptersOf[ref])
	}
	for ch := range entriesOf {
		entriesOf[ch] = sortedUniqueStrings(entriesOf[ch])
	}

	seedSet := map[int64]bool{}
	for _, ch := range seedChapters {
		if ch > 0 {
			seedSet[ch] = true
		}
	}
	visitedChapters := map[int64]bool{}
	visitedEntries := map[string]bool{}
	links := map[int64][]string{} // 依赖章 → 进入闭包的共引条目
	var frontier []int64

	// 先标记种子章节（不计入依赖），并进前沿（它们的共引条目向外扩展）。
	for _, ch := range sortedUniqueInt64(seedChapters) {
		if ch <= 0 || visitedChapters[ch] {
			continue
		}
		visitedChapters[ch] = true
		frontier = append(frontier, ch)
	}
	// 种子条目：条目被改动 → 绑定该条目的章节直接受影响（种子章除外）。
	for _, ref := range sortedUniqueStrings(seedEntries) {
		if ref == "" || visitedEntries[ref] {
			continue
		}
		visitedEntries[ref] = true
		frontier = expandEntry(ref, chaptersOf, visitedChapters, seedSet, links, frontier)
	}
	// 传递扩展（FIFO + visited 集防环）：依赖章的共引条目拉出更远的依赖章。
	for len(frontier) > 0 {
		ch := frontier[0]
		frontier = frontier[1:]
		for _, ref := range entriesOf[ch] {
			if visitedEntries[ref] {
				continue
			}
			visitedEntries[ref] = true
			frontier = expandEntry(ref, chaptersOf, visitedChapters, seedSet, links, frontier)
		}
	}

	if len(links) == 0 {
		return nil
	}
	out := make([]Dependent, 0, len(links))
	for ch, refs := range links {
		out = append(out, Dependent{ChapterID: ch, EntryRefs: sortedUniqueStrings(refs)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ChapterID < out[j].ChapterID })
	return out
}

// expandEntry 展开一个台账条目的绑定章节：未访问的章节记为依赖并进前沿；
// 已访问的非种子章补记共引条目归因。返回更新后的前沿。
func expandEntry(ref string, chaptersOf map[string][]int64, visitedChapters map[int64]bool,
	seedSet map[int64]bool, links map[int64][]string, frontier []int64) []int64 {
	for _, ch := range chaptersOf[ref] {
		switch {
		case !visitedChapters[ch]:
			visitedChapters[ch] = true
			links[ch] = append(links[ch], ref)
			frontier = append(frontier, ch)
		case !seedSet[ch]:
			links[ch] = append(links[ch], ref)
		}
	}
	return frontier
}

// PropagationEscalates 判定依赖闭包是否超阈值（>PropagationClosureCap 章 =
// 结构性牵连，升级 L2 深审/人工，不硬修）。
func PropagationEscalates(deps []Dependent) bool {
	return len(deps) > PropagationClosureCap
}

// sortedUniqueInt64 升序去重（确定性输出）。
func sortedUniqueInt64(vals []int64) []int64 {
	if len(vals) == 0 {
		return nil
	}
	seen := make(map[int64]bool, len(vals))
	out := make([]int64, 0, len(vals))
	for _, v := range vals {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// sortedUniqueStrings 升序去重（确定性输出）。
func sortedUniqueStrings(vals []string) []string {
	if len(vals) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(vals))
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
