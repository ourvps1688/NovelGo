package quality

import (
	"encoding/json"
	"strings"
)

// finding_round.go —— #114 P1 跨轮发现对齐快照（写侧推进，纯确定性）。
//
// chapter_quality 只有一行、UpsertL0/UpsertL1 分阶段整行覆盖，旧一轮 findings
// 在覆盖瞬间就没了。这里在覆盖「之前」由 Store 读旧行、按 FindingKeys 对齐，
// 把三态结果作为一个 JSON 文本列写回同一行：读侧零改动即可拿到「本轮消掉几条、
// 仍在几条、新增几条」，无需新表、无需后台任务、无需 LLM。
//
// 时序处理：一次「重新检测本章」会先 UpsertL0（重算 L0）再 UpsertL1（写回执），
// 若两次各自开一轮，L0 的 resolved 会被 L1 那次覆盖掉。故 UpsertL0 开启一轮并
// 置 PendingL1，UpsertL1 命中该标记时沿用同一份 PrevKeys 重算全量差，不开新轮。

// FindingGuardRounds 是同一 key 连续未消除触发「判定器冲突」提示的轮数。
const FindingGuardRounds = 3

// FindingRound 是 chapter_quality.finding_round 列的 JSON 载荷。
type FindingRound struct {
	Round int `json:"round"`
	// PrevKeys/CurrentKeys 是本轮 diff 的两侧基线（完整 key 集合，L0+L1）。
	PrevKeys    []string `json:"prev_keys"`
	CurrentKeys []string `json:"current_keys"`
	// Resolved/Persisted/New 是三态结果，供档案卡片与前后对比消费。
	Resolved  []string `json:"resolved"`
	Persisted []string `json:"persisted"`
	New       []string `json:"new"`
	// L0Keys 是本轮 L0 分量（诊断与测试用）；PendingL1 标记本轮 L1 尚未落档。
	L0Keys      []string       `json:"l0_keys"`
	PendingL1   bool           `json:"pending_l1,omitempty"`
	RepeatCount map[string]int `json:"repeat_count,omitempty"`
}

// MarshalFindingRound 序列化跨轮快照；失败返回空串（宁缺勿错）。
func MarshalFindingRound(r FindingRound) string {
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseFindingRound 解析跨轮快照；空串/坏数据返回零值（旧数据天然兼容）。
func ParseFindingRound(s string) FindingRound {
	var r FindingRound
	if strings.TrimSpace(s) == "" {
		return r
	}
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return FindingRound{}
	}
	return r
}

// GuardKeys 返回连续 FindingGuardRounds 轮仍未消除的 key（本轮仍在）。
// 纯本地确定性：调用方只用它给人工提示，不自动改阈值、不派发任何修复。
func (r FindingRound) GuardKeys() []string {
	out := []string{}
	for _, k := range r.Persisted {
		if r.RepeatCount[k] >= FindingGuardRounds {
			out = append(out, k)
		}
	}
	return out
}

func equalKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func nextRepeatCount(prev map[string]int, d FindingDiff) map[string]int {
	out := make(map[string]int, len(d.Persisted)+len(d.New))
	for _, k := range d.Persisted {
		out[k] = prev[k] + 1
	}
	for _, k := range d.New {
		out[k] = 1
	}
	// resolved 的计数随该 key 一起消失；重新出现时按 new 从 1 起算。
	return out
}

// carryRepeatCount 是同轮 L1 收口用的计数口径：只增删、不累加。
// 同轮收口不是「又改了一轮」，若在这里按 new 重置为 1，连续未消除轮数
// 会被 L0→L1 的两次写入来回清零，轮次保护永远触发不了。
func carryRepeatCount(prev map[string]int, d FindingDiff) map[string]int {
	out := make(map[string]int, len(prev))
	for k, v := range prev {
		out[k] = v
	}
	for _, k := range d.New {
		if out[k] == 0 {
			out[k] = 1
		}
	}
	for _, k := range d.Resolved {
		delete(out, k)
	}
	return out
}

// NextFindingRound 在覆盖前推进一轮（纯函数）。
//   - old == nil：首轮，全部 incoming 记 new；
//   - scope "l0"：开启新轮，PrevKeys 取旧行完整 keys，PendingL1=true；
//   - scope "l1"：若旧轮 PendingL1 则沿用 PrevKeys 重算（同一轮收口），否则新开一轮。
//
// incoming 是本阶段新写入的那一半清单（L0 或 L1），另一半从旧行沿用。
func NextFindingRound(old *ChapterQuality, scope string, incoming []Finding) FindingRound {
	if old == nil {
		var l0, l1 []Finding
		if scope == "l0" {
			l0 = incoming
		} else {
			l1 = incoming
		}
		cur := FindingKeys(l0, l1)
		d := DiffFindingKeys(nil, cur)
		return FindingRound{
			Round:       1,
			PrevKeys:    []string{},
			CurrentKeys: cur,
			Resolved:    d.Resolved,
			Persisted:   d.Persisted,
			New:         d.New,
			L0Keys:      FindingKeys(l0, nil),
			PendingL1:   scope == "l0",
			RepeatCount: nextRepeatCount(nil, d),
		}
	}

	oldRound := ParseFindingRound(old.FindingRound)
	oldL0 := ParseFindings(old.L0Findings)
	oldL1 := ParseFindings(old.L1Findings)
	prev := FindingKeys(oldL0, oldL1)

	var curL0, curL1 []Finding
	var l0Keys []string
	sameRound := false
	switch scope {
	case "l0":
		curL0, curL1 = incoming, oldL1
		l0Keys = FindingKeys(incoming, nil)
	case "l1":
		curL0, curL1 = oldL0, incoming
		l0Keys = FindingKeys(oldL0, nil)
		// 同一轮收口：UpsertL0 刚开启本轮，且旧行的 L0 分量没被换过。
		sameRound = oldRound.PendingL1 && oldRound.Round > 0 &&
			equalKeys(oldRound.L0Keys, l0Keys)
	default:
		curL0, curL1 = oldL0, oldL1
		l0Keys = FindingKeys(oldL0, nil)
	}
	if sameRound {
		prev = oldRound.PrevKeys
	}
	cur := FindingKeys(curL0, curL1)
	d := DiffFindingKeys(prev, cur)
	round := oldRound.Round
	if !sameRound {
		round++
	}
	repeat := nextRepeatCount(oldRound.RepeatCount, d)
	if sameRound {
		repeat = carryRepeatCount(oldRound.RepeatCount, d)
	}
	return FindingRound{
		Round:       round,
		PrevKeys:    prev,
		CurrentKeys: cur,
		Resolved:    d.Resolved,
		Persisted:   d.Persisted,
		New:         d.New,
		L0Keys:      l0Keys,
		PendingL1:   scope == "l0",
		RepeatCount: repeat,
	}
}
