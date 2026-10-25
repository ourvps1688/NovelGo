package structure

// plan_check.go —— 章纲级写前检查 L1.5（R2b-4，提案 §9.1 写前把关核心层）。
//
// 章纲任务卡/章节计划 vs 坑表/书写不变量/题材卡禁忌/角色台账的确定性对账：
//   - plan_pit_missing   钩子任务挂的伏笔不在坑表（存在性对账）；
//   - plan_pit_window    挂坑的预期收章与本章不符（预期收章区间对账）；
//   - plan_taboo         计划文本命中题材卡禁忌；
//   - plan_alive_violation 计划让承诺窗口内不死的角色死亡（书写不变量对账）；
//   - plan_name_drift    同一角色多种称呼混用（角色台账对账，R2b-5）；
//   - plan_dead_appearance 计划让已死角色出镜（角色台账对账，R2b-5）。
//
// 只报告不拦截（P2 纪律）：发现恒 P1 落工具回执告警，不拒收立卡/更新。
// 确定性启发式、宁缺勿错：引号提取不到锚点/意图词不明/名称过短（<2 字）一律跳过，
// 与结构块同属 S 系命名空间（一次运行内 S1…Sn 顺编，不与块内告警同框）。
//
// 坑表对账只认钩子任务文本（「本章埋/收哪个坑」的机制化落点）；
// 禁忌/不变量对账扫全部计划文本。战力上限（capability_ceiling）与 custom 不变量
// 在计划自由文本上不可数值化/机器不判，跳过（宁缺勿错）。

import (
	"fmt"
	"sort"
	"strings"
)

// 写前对账检查项标识单源（发现 Check 字段取自此，禁两处各写一份）。
const (
	CheckPlanPitMissing     = "plan_pit_missing"     // 钩子任务挂的伏笔不在坑表
	CheckPlanPitWindow      = "plan_pit_window"      // 挂坑的预期收章区间不符
	CheckPlanTaboo          = "plan_taboo"           // 计划文本命中题材卡禁忌
	CheckPlanAlive          = "plan_alive_violation" // 计划让窗口内不死角色死亡
	CheckPlanNameDrift      = "plan_name_drift"      // 计划混用同一角色多种称呼（R2b-5）
	CheckPlanDeadAppearance = "plan_dead_appearance" // 计划让已死角色出镜（R2b-5）
)

// planMinNameRunes 是对账名称最短长度（rune）：单字名/单字引号内容噪声大，
// 宁可漏拦不误报（只报告场景，漏一条无害、错一条误导）。
const planMinNameRunes = 2

// planIntentWindow 是埋/收意图词扫描窗口（引号名/坑名命中点前后 rune 数）。
const planIntentWindow = 24

// planHarvestSignals 是「收坑」意图词（保守高精度表）。
var planHarvestSignals = []string{
	"收坑", "填坑", "回收", "揭晓", "揭开", "解开", "了结", "兑现", "爆坑", "收线", "回收", "收束",
}

// planBurySignals 是「埋坑」意图词（保守高精度表）。
var planBurySignals = []string{
	"埋坑", "埋下", "埋设", "埋线", "种下", "铺垫", "设局", "开坑", "埋入",
}

// planDeathSignals 是角色死亡意图词（保守高精度表：多字词，避免单字「死」误伤）。
var planDeathSignals = []string{
	"击杀", "杀死", "之死", "身亡", "牺牲", "阵亡", "殒命", "陨落", "丧命", "毙命",
	"自尽", "同归于尽", "战死", "毒死", "处死", "赐死", "死于", "赴死", "送命", "折在",
}

// PlanField 是待对账的计划文本片段（带字段名供告警归因）。
type PlanField struct {
	Name string // 如 hook_task / beat_task / content
	Text string
}

// PlanCheckInput 是写前对账输入（全部确定性投影，零 LLM）。
type PlanCheckInput struct {
	ReadingNumber int         // 本卡/计划对应的章阅读序号（>0 时参与收章区间判定）
	HookText      string      // 钩子任务全文（坑表对账唯一作用域）
	Texts         []PlanField // 待禁忌/死亡对账的全部计划文本
	Foreshadows   []ForeshadowEntry
	Invariants    []PinnedInvariant
	Characters    []CharacterState
	Taboos        []string // 题材卡禁忌清单（已归一或原始，内部去空去重）
	WindowGrace   int      // 预期收章区间宽限章数（题材卡 WindowN；0=零宽限）
}

// CheckChapterPlan 对章纲任务卡/章节计划做写前确定性对账，返回 S 系告警清单。
// 纯函数、从不返回错误：缺输入的检查自然跳过，整体最多零告警（降级纪律）。
// 确定性：检查项固定顺序（存在性→区间→禁忌→不变量→称呼混用→死人出镜）、
// 条目 ID 升序、文本字段给定序，同输入同输出。
func CheckChapterPlan(in PlanCheckInput) []StructFinding {
	grace := in.WindowGrace
	if grace < 0 {
		grace = 0
	}
	var findings []StructFinding
	add := func(check, desc, ref string) {
		findings = append(findings, StructFinding{
			ID:    fmt.Sprintf("S%d", len(findings)+1),
			Check: check,
			Level: FindingLevelDefault,
			Desc:  desc,
			Ref:   ref,
		})
	}

	// 1. 存在性：钩子任务引号里的坑名须在坑表有对应条目（标题双向包含即算存在）。
	for _, name := range extractQuotedNames(in.HookText) {
		if matchForeshadowTitle(in.Foreshadows, name) == nil {
			add(CheckPlanPitMissing,
				fmt.Sprintf("钩子任务挂的伏笔「%s」不在坑表（先 create_timeline_entry 登记，或改正名）", name),
				"")
		}
	}

	// 2. 预期收章区间：坑名出现在钩子任务（=挂了该坑）且意图词明确时对账收章窗口。
	sorted := append([]ForeshadowEntry(nil), in.Foreshadows...)
	sortForeshadowByID(sorted)
	for _, e := range sorted {
		if e.Title == "" || len([]rune(e.Title)) < planMinNameRunes {
			continue
		}
		idx := strings.Index(in.HookText, e.Title)
		if idx < 0 {
			continue
		}
		switch pitIntent(in.HookText, idx, len(e.Title)) {
		case "收":
			switch {
			case e.TargetReadingNumber <= 0:
				add(CheckPlanPitWindow,
					fmt.Sprintf("钩子任务计划回收伏笔「%s」，但坑表未设预期收章（先补 target_chapter）", e.Title),
					fmt.Sprintf("time_entries:%d", e.ID))
			case in.ReadingNumber > 0 && absInt(in.ReadingNumber-e.TargetReadingNumber) > grace:
				add(CheckPlanPitWindow,
					fmt.Sprintf("钩子任务计划回收伏笔「%s」（预期第 %d 章收），本卡第 %d 章超出预期收章区间（宽限 %d 章）",
						e.Title, e.TargetReadingNumber, in.ReadingNumber, grace),
					fmt.Sprintf("time_entries:%d", e.ID))
			}
		case "埋":
			if e.TargetReadingNumber > 0 && in.ReadingNumber > 0 && e.TargetReadingNumber <= in.ReadingNumber {
				add(CheckPlanPitWindow,
					fmt.Sprintf("钩子任务计划埋设伏笔「%s」，但坑表预期第 %d 章收（不晚于本卡第 %d 章），预期收章须晚于埋设",
						e.Title, e.TargetReadingNumber, in.ReadingNumber),
					fmt.Sprintf("time_entries:%d", e.ID))
			}
		}
	}

	// 3. 题材卡禁忌：逐字段逐词确定性扫描（清单序）。
	terms := dedupePlanTerms(in.Taboos)
	for _, f := range in.Texts {
		text := strings.TrimSpace(f.Text)
		if text == "" {
			continue
		}
		for _, term := range terms {
			if strings.Contains(text, term) {
				add(CheckPlanTaboo,
					fmt.Sprintf("%s 命中题材卡禁忌词「%s」（题材卡优先级最高，请改写计划规避）", f.Name, term),
					"novel_checker_configs:taboo_list")
			}
		}
	}

	// 4. 书写不变量：character_alive_before 承诺窗口内不得计划该角色死亡
	//    （名称/别名 + 死亡词同字段共现才算，宁缺勿错；custom/ceiling 计划级不判）。
	//    无章号锚点（远期计划）不做窗口判定——承诺可能在计划落地前过期，宁可漏拦。
	if in.ReadingNumber > 0 {
		for _, pin := range in.Invariants {
			if pin.Kind != KindAliveBefore {
				continue
			}
			params, ok := ParseAliveBeforeParams(pin.ParamsJSON)
			if !ok || strings.TrimSpace(params.Character) == "" {
				continue
			}
			// 「第 X 章前不死」：before<=0=全书不死；本章已过窗口（N>=X）承诺失效不判。
			if params.BeforeReadingNumber > 0 && in.ReadingNumber >= params.BeforeReadingNumber {
				continue
			}
			names := append([]string{params.Character}, aliasesOf(in.Characters, params.Character)...)
			for _, f := range in.Texts {
				text := strings.TrimSpace(f.Text)
				if text == "" {
					continue
				}
				if hitName := matchPlanNames(text, names); hitName != "" && containsAny(text, planDeathSignals) {
					add(CheckPlanAlive,
						fmt.Sprintf("%s 计划让「%s」在承诺窗口内死亡，违反书写不变量「%s」（第 %d 章前不死，请改写计划）",
							f.Name, hitName, pin.Statement, params.BeforeReadingNumber),
						fmt.Sprintf("pinned_invariants:%d", pin.ID))
					break // 同一不变量同文本多命中只报一次（宁缺勿错）
				}
			}
		}
	}

	// 5. 称呼混用（R2b-5 计划级同判）：同一角色正名/别名 ≥2 种形式出现在计划文本
	//    （全卡合并扫描：同一章计划跨字段混用也算；不带字段归因）。
	//    正向命中判定，不做「未知名字」检查（宁缺勿错）。
	joined := joinPlanTexts(in.Texts)
	for _, c := range in.Characters {
		matched := dropSubsumedPlanForms(matchedPlanForms(joined, planCharForms(c)))
		if len(matched) < 2 {
			continue
		}
		add(CheckPlanNameDrift,
			fmt.Sprintf("章纲计划混用「%s」的多种称呼：%s（同一章内建议统一称呼，或确认别名表是否该更新）",
				strings.TrimSpace(c.Name), strings.Join(matched, "、")),
			fmt.Sprintf("characters:%d", c.ID))
	}

	// 6. 死人出镜（R2b-5 计划级同判）：已死角色（Alive=false）的正名/别名出现在计划文本。
	//    每角色只报一次（首命中字段归因）；nil=未标注、true=在世不判；
	//    远期计划照判（死亡与章号无关）。回忆/闪回由作者显式标注，此处只报风险。
	for _, c := range in.Characters {
		if c.Alive == nil || *c.Alive {
			continue
		}
		for _, f := range in.Texts {
			text := strings.TrimSpace(f.Text)
			if text == "" {
				continue
			}
			if hit := matchPlanNames(text, planCharForms(c)); hit != "" {
				add(CheckPlanDeadAppearance,
					fmt.Sprintf("%s 计划让已死角色「%s」出镜（命中「%s」，台账生死态=已死；回忆/闪回请显式标注，若复活请先更新台账）",
						f.Name, strings.TrimSpace(c.Name), hit),
					fmt.Sprintf("characters:%d", c.ID))
				break // 同一角色多字段命中只报一次（宁缺勿错）
			}
		}
	}
	return findings
}

// extractQuotedNames 提取文本中「」/“”引号内的名称（确定性锚点）。
// 引号不成对/内容过短跳过；同一名称去重保首次出现序。
func extractQuotedNames(text string) []string {
	var out []string
	seen := map[string]bool{}
	pairs := [][2]string{{"「", "」"}, {"“", "”"}}
	for _, p := range pairs {
		rest := text
		for {
			i := strings.Index(rest, p[0])
			if i < 0 {
				break
			}
			rest = rest[i+len(p[0]):]
			j := strings.Index(rest, p[1])
			if j < 0 {
				break
			}
			name := strings.TrimSpace(rest[:j])
			rest = rest[j+len(p[1]):]
			if len([]rune(name)) < planMinNameRunes || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// matchForeshadowTitle 在坑表里找与名称互为包含的条目（标题含名称或名称含标题
// 均算存在，容忍「三年之约」vs「三年之约的真相」这类长短引用）；找不到返回 nil。
func matchForeshadowTitle(entries []ForeshadowEntry, name string) *ForeshadowEntry {
	for i := range entries {
		title := entries[i].Title
		if title == "" {
			continue
		}
		if strings.Contains(title, name) || strings.Contains(name, title) {
			return &entries[i]
		}
	}
	return nil
}

// pitIntent 判定挂坑意图：坑名命中点 ± 窗口内出现收/埋意图词；
// 两可或无锚点返回 ""（跳过区间对账，宁缺勿错）。窗口按字节近似取（启发式，
// 命中词是子串 Contains 判定，边界不必精确到 rune）。
func pitIntent(text string, idx, titleLen int) string {
	start := idx - planIntentWindow
	if start < 0 {
		start = 0
	}
	end := idx + titleLen + planIntentWindow
	if end > len(text) {
		end = len(text)
	}
	window := text[start:end]
	harvest := containsAny(window, planHarvestSignals)
	bury := containsAny(window, planBurySignals)
	switch {
	case harvest && !bury:
		return "收"
	case bury && !harvest:
		return "埋"
	default:
		return ""
	}
}

// aliasesOf 取台账中该角色的别名（精确名匹配；找不到返回 nil）。
func aliasesOf(chars []CharacterState, name string) []string {
	for _, c := range chars {
		if strings.TrimSpace(c.Name) == strings.TrimSpace(name) {
			return c.Aliases
		}
	}
	return nil
}

// matchPlanNames 返回文本命中的第一个名称（顺序=参数序），未命中返回空串。
func matchPlanNames(text string, names []string) string {
	for _, n := range names {
		n = strings.TrimSpace(n)
		if len([]rune(n)) < planMinNameRunes {
			continue
		}
		if strings.Contains(text, n) {
			return n
		}
	}
	return ""
}

// planCharForms 投影角色的称呼形式（正名+别名，TrimSpace 去空去重保序，
// <planMinNameRunes 跳过——单字名噪声大，宁可漏拦不误报）。
func planCharForms(c CharacterState) []string {
	terms := append([]string{c.Name}, c.Aliases...)
	out := make([]string, 0, len(terms))
	seen := map[string]bool{}
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] || len([]rune(t)) < planMinNameRunes {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// joinPlanTexts 合并全部计划文本（字段序，换行分隔）供跨字段称呼混用扫描。
func joinPlanTexts(fields []PlanField) string {
	var parts []string
	for _, f := range fields {
		if t := strings.TrimSpace(f.Text); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

// matchedPlanForms 返回文本命中的称呼形式（保形式序：正名先于别名）。
func matchedPlanForms(text string, forms []string) []string {
	var out []string
	for _, f := range forms {
		if strings.Contains(text, f) {
			out = append(out, f)
		}
	}
	return out
}

// dropSubsumedPlanForms 剔除被更长命中形式包含的短形式（「林澈」⊂「林澈哥哥」只算
// 一种称呼）：长短并用的真实混用会漏报，但误报误导、漏报无害（宁缺勿错，与 L0 同口径）。
func dropSubsumedPlanForms(matched []string) []string {
	out := make([]string, 0, len(matched))
	for i, f := range matched {
		subsumed := false
		for j, g := range matched {
			if i != j && strings.Contains(g, f) {
				subsumed = true
				break
			}
		}
		if !subsumed {
			out = append(out, f)
		}
	}
	return out
}

// containsAny 文本含任一信号词。
func containsAny(text string, signals []string) bool {
	for _, s := range signals {
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// dedupePlanTerms 归一化词表：TrimSpace 去空去重保序（与 L0 dedupeTerms 同口径，
// 各包自持小工具，不跨包导出）。
func dedupePlanTerms(terms []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// sortForeshadowByID 按条目 ID 升序（确定性遍历序）。
func sortForeshadowByID(entries []ForeshadowEntry) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
}

// absInt 整数绝对值。
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
