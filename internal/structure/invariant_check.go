package structure

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CheckPinnedInvariants 对书写不变量做确定性比对（提案 §8.2 用户书写不变量 / §9.2 吃书）：
// 规则可判 kind 对台账投影比对产出违例告警，custom/未知 kind/坏参数/角色不在台账 →
// 跳过不判（宁缺勿错，不猜违例）。
//
// 判定口径：
//   - character_alive_before：角色已标记死亡（Alive=false）且承诺窗口未过
//     （before<=0=全书不死，或当前进度 < before）→ 违例；nil=未标注不算死亡。
//   - capability_ceiling：账面天花板与钉住上限可比且账面 > 上限 → 违例；
//     两侧同为数值文本按数值比（现行口径），同为已登记境界名按境界数值锚比
//     （R2b-6 合流）；混比/任一不可解析跳过不判（不同刻度不混比，宁缺勿错）。
//     levels 为空=无境界表=只有数值可比（缺省=现行行为逐字一致）。
//
// 确定性：按不变量 ID 升序产出，同输入同输出。
func CheckPinnedInvariants(invariants []PinnedInvariant, chars []CharacterState, nowReadingNumber int, levels []PowerLevel) []StructFinding {
	sorted := append([]PinnedInvariant(nil), invariants...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	charsByID := append([]CharacterState(nil), chars...)
	sort.SliceStable(charsByID, func(i, j int) bool { return charsByID[i].ID < charsByID[j].ID })

	var findings []StructFinding
	add := func(p *PinnedInvariant, desc string) {
		findings = append(findings, StructFinding{
			ID:    fmt.Sprintf("S%d", len(findings)+1),
			Check: p.Kind,
			Level: FindingLevelDefault,
			Desc:  desc,
			Ref:   fmt.Sprintf("pinned_invariants:%d", p.ID),
		})
	}
	for i := range sorted {
		p := &sorted[i]
		switch p.Kind {
		case KindAliveBefore:
			params, ok := ParseAliveBeforeParams(p.ParamsJSON)
			if !ok {
				continue
			}
			c := matchCharacter(charsByID, params.Character)
			if c == nil || c.Alive == nil || *c.Alive {
				continue // 角色不在台账 / 未标注生死 / 仍在世 → 未违例（宁缺勿错）
			}
			if params.BeforeReadingNumber > 0 && nowReadingNumber >= params.BeforeReadingNumber {
				continue // 承诺窗口已过，之后死亡不算违例
			}
			if params.BeforeReadingNumber > 0 {
				add(p, fmt.Sprintf("不变量「%s」违例：角色「%s」已标记死亡，早于第 %d 章前不死的承诺（当前进度第 %d 章）",
					p.Statement, params.Character, params.BeforeReadingNumber, nowReadingNumber))
			} else {
				add(p, fmt.Sprintf("不变量「%s」违例：角色「%s」已标记死亡，违反全书不死承诺（当前进度第 %d 章）",
					p.Statement, params.Character, nowReadingNumber))
			}
		case KindCapabilityCeiling:
			params, ok := ParseCapabilityCeilingParams(p.ParamsJSON)
			if !ok {
				continue
			}
			c := matchCharacter(charsByID, params.Character)
			if c == nil {
				continue
			}
			ledgerNum, pinNum, ok := resolveCeilingPair(c.CapabilityCeiling, params.Ceiling, levels)
			if !ok {
				continue // 自由文本天花板/境界未登记/刻度混比 → 跳过不判
			}
			if ledgerNum > pinNum {
				add(p, fmt.Sprintf("不变量「%s」违例：角色「%s」账面战力天花板（%s）超过钉住上限（%s）",
					p.Statement, params.Character, strings.TrimSpace(c.CapabilityCeiling), params.Ceiling))
			}
		}
	}
	return findings
}

// matchCharacter 按角色名/别名精确匹配台账投影（匹配不到返回 nil，宁缺勿错）。
func matchCharacter(chars []CharacterState, name string) *CharacterState {
	target := strings.TrimSpace(name)
	for i := range chars {
		c := &chars[i]
		if strings.TrimSpace(c.Name) == target {
			return c
		}
		for _, alias := range c.Aliases {
			if strings.TrimSpace(alias) == target {
				return c
			}
		}
	}
	return nil
}

// parseCeilingNumber 解析战力天花板数值（纯数字文本）；自由文本返回 ok=false。
func parseCeilingNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// resolveCeilingPair 解析账面天花板与钉住上限两侧（R2b-6 合流）：
// 同为数值文本按数值（现行口径逐字一致优先），否则同为已登记境界名按境界数值锚；
// 刻度混比（数值 vs 境界名）/任一不可解析 → ok=false（宁缺勿错，不混刻度）。
func resolveCeilingPair(ledger, pin string, levels []PowerLevel) (float64, float64, bool) {
	if a, ok := parseCeilingNumber(ledger); ok {
		if b, ok2 := parseCeilingNumber(pin); ok2 {
			return a, b, true
		}
		return 0, 0, false // 数值 vs 非数值：不同刻度不混比
	}
	a, ok1 := RankOfLevel(levels, ledger)
	b, ok2 := RankOfLevel(levels, pin)
	if ok1 && ok2 {
		return float64(a), float64(b), true
	}
	return 0, 0, false
}
