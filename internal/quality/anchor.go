package quality

// anchor.go —— 定向修复的「本地锚点」单源（纯本地、零 LLM、零写操作、零回执契约变更）。
//
// 旧链路里 L1 语义发现只有一句自由文本 Desc，处方层拿不到「改哪一句」，只能给
// 「二选一：把正文改成与台账一致，或更新/作废过时台账条目」这种通用模板；用户
// 勾 5 条发现，chat 里就拿到 5 行几乎逐字相同的模板，模型只能凭语感重写，改 A
// 引入 B。第 12 章连修 9 轮、每轮换一批新题正是这个缺口的后果。
//
// 这里不要求 LLM 多吐一个字段（#362 之后回执格式刚稳定，绝不再动契约）：直接
// 从 Desc 的成对引号里挑出**能在当前正文中逐字命中**的最长原句作为锚点。这一步
// 天然过滤掉「前章原句 / 计划卡原文 / 台账引用」——它们不在本章正文里，命中不了。
// 锚点只用于三处：处方的位置/动作、落盘后本地核验「原句改没改」、稳定 key 的身份。
//
// 抽不到就如实标「未定位」，绝不臆造位置、绝不拿描述里的非正文片段冒充锚点。

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode/utf8"
)

const (
	// 太短的片段（如「他」「五公里」）到处都是，定位价值低；太长的多半是整段引用，
	// 一次 search_replace 也改不动。只收这个窗口内的原句。
	anchorMinRunes = 8
	anchorMaxRunes = 120
)

// Anchor 是一条语义发现落到正文里的可定位原句。Seg 为 -1 表示没有可用曲线。
type Anchor struct {
	Quote     string
	StartRune int
	EndRune   int
	Seg       int
}

// Valid 报告是否真的定位到了正文原句。
func (a Anchor) Valid() bool { return a.Quote != "" }

// Where 渲染处方里的「位置」段：段号 + 字区间 + 可逐字复制的原句。
func (a Anchor) Where() string {
	if !a.Valid() {
		return "未定位（描述里没有能在正文逐字命中的原句；请按发现描述自行定位，不要猜）"
	}
	if a.Seg >= 0 {
		return fmt.Sprintf("seg %d（字 %d–%d）｜原句「%s」", a.Seg, a.StartRune, a.EndRune, a.Quote)
	}
	return fmt.Sprintf("字 %d–%d｜原句「%s」", a.StartRune, a.EndRune, a.Quote)
}

// quotePairs 覆盖中文直角/弯引号与英文引号。英文引号开闭同字符，扫描时按下一次
// 出现即闭合；中文开闭不同字符，内部出现其它引号不会误闭合。
var quotePairs = []struct{ open, close rune }{
	{'「', '」'},
	{'『', '』'},
	{'“', '”'},
	{'\'', '\''},
	{'"', '"'},
}

// quoteCandidates 按出现顺序抽出 Desc 里所有成对引号内的片段（不去重、不过滤）。
func quoteCandidates(desc string) []string {
	rs := []rune(desc)
	out := make([]string, 0, 4)
	open := -1
	var closeRune rune
	for i, r := range rs {
		if open < 0 {
			for _, p := range quotePairs {
				if r == p.open {
					open = i + 1
					closeRune = p.close
					break
				}
			}
			continue
		}
		if r != closeRune {
			continue
		}
		s := strings.TrimSpace(string(rs[open:i]))
		n := utf8.RuneCountInString(s)
		if n >= anchorMinRunes && n <= anchorMaxRunes {
			out = append(out, s)
		}
		open = -1
	}
	return out
}

// ExtractAnchor 从 Desc 里挑出能在 content 逐字命中的最长原句，并算出 rune 区间与
// 所属段。同一长度时取正文里更靠前的一处。curve 可为 nil（Seg 记为 -1）。
// 找不到任何可命中片段时返回 Valid()==false，调用方按「未定位」降级。
func ExtractAnchor(desc, content string, curve []TensionPoint) (Anchor, bool) {
	if strings.TrimSpace(desc) == "" || strings.TrimSpace(content) == "" {
		return Anchor{Seg: -1}, false
	}
	best, bestLen, bestIdx := "", 0, -1
	for _, q := range quoteCandidates(desc) {
		idx := strings.Index(content, q)
		if idx < 0 {
			continue // 前章引用 / 计划卡原文 / 台账条目天然不在本章正文里
		}
		n := utf8.RuneCountInString(q)
		if n > bestLen || (n == bestLen && idx < bestIdx) {
			best, bestLen, bestIdx = q, n, idx
		}
	}
	if best == "" {
		return Anchor{Seg: -1}, false
	}
	start := utf8.RuneCountInString(content[:bestIdx])
	a := Anchor{Quote: best, StartRune: start, EndRune: start + bestLen, Seg: -1}
	for _, pt := range curve {
		if start >= pt.StartRune && start < pt.EndRune {
			a.Seg = pt.Seg
			break
		}
	}
	return a, true
}

// LocateAnchor 对一条已知原句补算 rune 区间与段号（档案里已存 Anchor 时用，
// 不必再从 Desc 重抽）。原句不在正文里时返回 false，调用方按「未定位」降级。
func LocateAnchor(content, quote string, curve []TensionPoint) (Anchor, bool) {
	if !AnchorPresent(content, quote) {
		return Anchor{Seg: -1}, false
	}
	idx := strings.Index(content, quote)
	start := utf8.RuneCountInString(content[:idx])
	a := Anchor{
		Quote:     quote,
		StartRune: start,
		EndRune:   start + utf8.RuneCountInString(quote),
		Seg:       -1,
	}
	for _, pt := range curve {
		if start >= pt.StartRune && start < pt.EndRune {
			a.Seg = pt.Seg
			break
		}
	}
	return a, true
}

// AnchorHash 给原句一个 8 位稳定短哈希，供稳定 key 携带问题身份（不把 Desc 全文
// 塞进 key）。同一原句 → 同一哈希；原句被改掉 → 哈希随之消失，正是「这条消解了」。
func AnchorHash(quote string) string {
	if quote == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(quote))
	return fmt.Sprintf("%08x", h.Sum32())
}

// WithAnchors 给一组发现补上锚点（只填 L1 语义发现、只填空缺，绝不覆盖已有值）。
// 返回新切片，不改动入参；content 为空或抽不到时该条保持无锚点，处方自动降级。
func WithAnchors(findings []Finding, content string, curve []TensionPoint) []Finding {
	out := append([]Finding(nil), findings...)
	if strings.TrimSpace(content) == "" {
		return out
	}
	for i := range out {
		if out[i].Anchor != "" || !L1CheckValid(out[i].Check) {
			continue
		}
		if a, ok := ExtractAnchor(out[i].Desc, content, curve); ok {
			out[i].Anchor = a.Quote
		}
	}
	return out
}

// AnchorPresent 判断上轮目标原句在新正文里是否还在（本地确定性复验）。
// 原句消失 = 该条已被改动，待 L1 确认；仍在 = 该条本轮没动。
func AnchorPresent(content, quote string) bool {
	return quote != "" && strings.Contains(content, quote)
}
