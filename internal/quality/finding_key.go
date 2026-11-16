package quality

import (
	"sort"
	"strconv"
	"strings"
)

// finding_key.go —— #114 P1 发现项稳定身份单源（纯本地、零 LLM、零派发）。
//
// 旧链路里 Finding.ID 是每次 RunL0Checks 顺编的 C1…、L1 回执按序补的 L1-n，
// UpsertL0/UpsertL1 又整行覆盖 JSON，导致「上一轮那条 tail_hook 和这一轮是不是
// 同一条」在物理上无法回答，改了 8 轮用户也看不到哪条被消掉。
//
// 稳定 key = 作用域 + 检查项 + 归一化命中特征 + 同组序号：
//   - 作用域 l0/l1：避免同 check 跨层撞号；
//   - Check：全部检测/处方/复验代码本来就以它做身份判定（单源）；
//   - 归一化命中特征：只取 LedgerRefs（trim+排序），取不到就留空；
//     L1 语义发现若无台账引用但有本地锚点，改用原句短哈希（见下），让 key 携带
//     真实问题身份——否则「上一轮台阶数、这一轮步数折算」都叫 l1|continuity|#0；
//   - 同组序号：同一 (scope, check, feature) 内按出现顺序编号。
//
// 绝不进 key：顺编 ID、Desc 全文、字数/段序/升幅/峰值数等本轮实测值、Status、
// Level、ChapterIDs（随章节移动变化）、时间戳、锚点原句本身（只进 8 位哈希）。
// 固定阈值也不必进——它们只影响文案措辞，不影响「是不是同一条问题」。

// FindingKeysMarker 是 #114 P2 改稿消息里「本轮目标发现项」块的标记。
// 前端把 P1 单源生成的稳定 key 逐行列出，app 侧据此把本轮目标绑定进技能账本
// （skill_invocations.finding_keys），用于回答「本轮注入了什么、对应哪条发现」。
// 与 AcceptanceMarker 同纪律：中英双语 locale 都必须字面包含该标记。
const FindingKeysMarker = "【本章目标发现项】"

// findingFeature 返回归一化命中特征；无台账引用时为空串。
func findingFeature(f Finding) string {
	if len(f.LedgerRefs) == 0 {
		return ""
	}
	refs := append([]string(nil), f.LedgerRefs...)
	for i := range refs {
		refs[i] = strings.TrimSpace(refs[i])
	}
	sort.Strings(refs)
	return strings.Join(refs, ",")
}

// findingKeysForScope 把一层发现清单投影成稳定 key（保序、组内编号）。
func findingKeysForScope(scope string, list []Finding) []string {
	seen := make(map[string]int, len(list))
	out := make([]string, 0, len(list))
	for _, f := range list {
		check := f.Check
		if check == "" {
			check = "unspecified"
		}
		feature := findingFeature(f)
		// 无台账引用的 L1 语义发现原本恒为 l1|check|#0/#1：改好上一条后，本轮
		// 换一批全新的 continuity 问题仍撞同一 key，被算成「一直没修」，
		// repeat_count 每轮清零、3 轮保护永远触发不了（第 12 章 9 轮实证）。
		// 有本地锚点时带一段原句短哈希：原句被改掉 → 该 key 自然消失（resolved）；
		// 原句没动 → key 不变（真 persisted）。L0 与已有台账引用的发现逐字不变。
		if scope == "l1" && feature == "" && f.Anchor != "" {
			feature = "q" + AnchorHash(f.Anchor)
		}
		base := scope + "|" + check + "|" + feature
		n := seen[base]
		seen[base] = n + 1
		out = append(out, base+"#"+strconv.Itoa(n))
	}
	return out
}

// FindingKeys 返回一轮档案的完整稳定 key 集合（L0 在前、L1 在后，保序）。
// 它是跨轮对齐、评分事件快照、轮次保护三处共用的唯一身份口径。
func FindingKeys(l0, l1 []Finding) []string {
	a := findingKeysForScope("l0", l0)
	return append(a, findingKeysForScope("l1", l1)...)
}

// FindingDiff 是相邻两轮 key 集合的三态差（保序：Resolved 按上一轮，其余按本轮）。
type FindingDiff struct {
	Resolved  []string `json:"resolved"`
	Persisted []string `json:"persisted"`
	New       []string `json:"new"`
}

// DiffFindingKeys 计算 resolved / persisted / new（纯函数、确定性、零副作用）。
func DiffFindingKeys(prev, current []string) FindingDiff {
	prevSet := make(map[string]bool, len(prev))
	for _, k := range prev {
		prevSet[k] = true
	}
	curSet := make(map[string]bool, len(current))
	for _, k := range current {
		curSet[k] = true
	}
	out := FindingDiff{
		Resolved:  []string{},
		Persisted: []string{},
		New:       []string{},
	}
	seen := map[string]bool{}
	for _, k := range prev {
		if seen[k] {
			continue
		}
		seen[k] = true
		if !curSet[k] {
			out.Resolved = append(out.Resolved, k)
		}
	}
	seen = map[string]bool{}
	for _, k := range current {
		if seen[k] {
			continue
		}
		seen[k] = true
		if prevSet[k] {
			out.Persisted = append(out.Persisted, k)
		} else {
			out.New = append(out.New, k)
		}
	}
	return out
}
