package text

import (
	"fmt"
	"regexp"
	"strings"
)

// DeslopIssue 表示一条去 AI 味命中项。
type DeslopIssue struct {
	Rule    string // 规则标识（如 em-dash / fate-cliche）
	Matched string // 命中的实际文本片段（可能截断）
	Context string // 命中处所在句子的上下文片段（帮助 LLM 定位，只改命中句）
	Message string // 人类可读的修正建议
}

// ── Blocking：确定性的、无歧义的 AI 味硬伤，写正文时强制拒收 ──
//
// 选择标准（源自 zenstory-ai/oh-story-claudecode 的 banned-words.md 一级禁用词 / 禁用句式模板）：
// 真人语料里几乎不出现、AI 高频套用，且命中即要求改写、不会误伤正常表达。
// 作用域：仅章节正文（非大纲），在 edit 写入管线审批前硬校验。
//
// 校准原则（2026-10 实测复盘：过严会让 LLM 整章重写反复命中、10 次连续失败中断对话）：
// 只硬拒「真人几乎不写」的指纹；真人也会自然使用的词（多年以后/从这一刻开始/
// 深吸一口气/毫无疑问类判断套词）降为 advisory 密度提示，见下方 advisoryRegexRules。

var blockingRules = []struct {
	rule    string
	re      *regexp.Regexp
	message string
}{
	// 破折号（保留既有语义：U+2014 连用、U+2015、双连字符均计）
	{"em-dash", regexp.MustCompile(`——|――|--`), "正文禁用破折号，改用逗号、句号或动作断句"},

	// 比喻套话：带「一般」后缀的精致比喻，是最明确的 AI 指纹
	{"simile-general", regexp.MustCompile(`(仿佛|犹如|宛若|如同|宛如)[^。！？\n]{0,14}一般`), "避免使用「X 一般」的精致比喻套路，改为白描或具体动作"},

	// 无情绪声线描写
	{"voice-flat", regexp.MustCompile(`声音不大[，,]?却|语气毫无波澜|平静无波|声音平直|声音平平|听不出情绪`), "避免公式化的「声音/语气」描写，直接写说出的话或可见动作"},

	// 命运 / 开端收束
	{"fate-cliche", regexp.MustCompile(`命运的(齿轮|棋局|獠牙)|命运终于露出獠牙|早已布好的棋局`), "避免用「命运/齿轮/棋局/獠牙」作抽象收束，改回角色当下的动作、物件或后果"},
	{"just-began", regexp.MustCompile(`(反击|复仇|故事|一切)才刚刚开始|属于[^，。\n]{0,8}(反击|复仇|故事)[^，。\n]{0,6}才刚刚开始`), "章末避免「才刚刚开始」式预告收束，用具体钩子或悬念收尾"},
	{"epiphany", regexp.MustCompile(`这一刻(终于|，)?(他|她)?(终于)?明白`), "避免「这一刻终于明白」式升华收尾，用动作或场景收束"},

	// 上帝视角 / 预告（「多年以后」是合法叙事手法，降为 advisory，不再硬拒）
	{"god-view", regexp.MustCompile(`(他|她)不知道的是|殊不知|仿佛预示着`), "避免上帝视角剧透/预告，只写角色此刻知道的；悬念留给读者"},

	// 过渡模板
	{"transition-template", regexp.MustCompile(`取而代之的是`), "避免「取而代之的是」式 AI 过渡模板，直接写新状态"},

	// 模板化身体 / 表情描写（「心中暗道」是网文自然内心戏引导，降为 advisory 密度项）
	{"stock-body", regexp.MustCompile(`眼中闪过(一丝|一抹)|嘴角勾起(一抹|一丝)|心中涌起一股|心头一震|心底泛起|瞳孔(微缩|收缩|一缩)|指节泛白`), "避免模板化身体/微表情描写（眼中闪过/嘴角勾起/瞳孔…），写角色当场的行为或台词"},
}

// BlockingIssues 扫描正文中确定性的 AI 味硬伤，返回所有命中的规则（每项含一个示例片段）。
// 空切片表示无硬伤。调用方应据其拒绝写入。
func BlockingIssues(s string) []DeslopIssue {
	var issues []DeslopIssue
	for _, r := range blockingRules {
		if loc := r.re.FindStringIndex(s); loc != nil {
			issues = append(issues, DeslopIssue{
				Rule:    r.rule,
				Matched: truncate(s[loc[0]:loc[1]], 16),
				Context: contextAround(s, loc[0], loc[1]),
				Message: r.message,
			})
		}
	}
	return issues
}

// BlockingErrorMessage 把硬伤列表格式化为可回给 LLM 的拒绝原因。
// 末尾附「定向修补」引导：只改命中句、不要重写整章——避免整章重写重掷骰子
// 导致连环命中、连续失败耗尽触发对话中断。
func BlockingErrorMessage(issues []DeslopIssue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "正文命中 %d 处 AI 味硬伤，系统已拒绝写入。请改写后重提：\n", len(issues))
	for _, it := range issues {
		if it.Context != "" && it.Context != it.Matched {
			fmt.Fprintf(&b, "· %s（所在句：%s）：%s\n", it.Matched, it.Context, it.Message)
		} else {
			fmt.Fprintf(&b, "· %s：%s\n", it.Matched, it.Message)
		}
	}
	b.WriteString("改写时只替换上述命中句，其余正文原样保留再次提交，不要重写整章。\n")
	return b.String()
}

// CountEmDashes 统计正文中破折号出现次数。
// 「——」（U+2014 连用）与「――」（U+2015）均计入；单个 U+2014 不计，
// 避免误伤数字区间等罕见合法用法。三个连续「———」计 1 处。
func CountEmDashes(content string) int {
	runes := []rune(content)
	count := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] == '―' { // U+2015 horizontal bar
			count++
			continue
		}
		if runes[i] == '—' && i+1 < len(runes) && runes[i+1] == '—' {
			count++
			i++ // 跳过成对中的第二个
		}
	}
	return count
}

// ── Advisory：密度 / 启发式标记，仅供 self-check 复核，不作写入门槛 ──
//
// 这些项在正常写作中可能有合法用途（如台词里的「不是A而是B」、角色真实呼吸的
// 「深吸一口气」），因此只做提示、不做硬拒。密度阈值参考 oh-story 的 tic 判据。

var advisoryRegexRules = []struct {
	rule    string
	re      *regexp.Regexp
	message string
}{
	// 从 blocking 降级的合法常用语（真人写作也会用，只做密度/自检提示）
	{"future-flash", regexp.MustCompile(`多年以后`), "「多年以后」是合法叙事手法，少量可保留；成片出现则成上帝视角预告，改写为角色当下的感知或具体后果"},
	{"vow-opening", regexp.MustCompile(`从这一刻开始`), "「从这一刻开始」式立誓收束是 AI 常用套式，单处可留，复现时改为动作或台词承担转折"},
	{"judgment-cliche", regexp.MustCompile(`不容置疑|不容置喙|毫无疑问|毋庸置疑|显而易见|不可否认|前所未有|不言而喻`), "「毫无疑问/不容置疑」等书面化判断套词：台词或确需强调时可留，叙述中复现时用具体事实或对话表达"},

	{"negation-flip", regexp.MustCompile(`不是[^。！？\n]{1,10}(而)?是`), "「不是A而是B」是常见 AI 否定铺垫翻转句式，直接写肯定内容或用具象动作承担对比"},
	{"negation-parade", regexp.MustCompile(`没有[^。！？\n]{0,8}没有`), "「没有X没有Y」否定铺排是 AI 句式，压成一次直述或只留一项"},
	{"universal-adverb", regexp.MustCompile(`，带着|带着(一丝|一抹|几分)`), "「，带着…」万能状语是 AI 套路，删掉状语或换具体动作"},
	{"tell-not-show", regexp.MustCompile(`(他|她)(知道|感到|意识到|明白)`), "「他知道/感到/意识到」是 tell-not-show，用行为展示认知而非告知"},
}

var (
	microActionWords = []string{
		"深吸一口气", "不禁", "不由得", "指尖轻叩", "袖口里攥紧", "指节泛白",
		"目光移开", "眉头微皱", "眉眼低垂", "瞳孔微缩", "瞳孔收缩", "瞳孔一缩",
		"眼神锐利", "目光锐利", "喉结滚动", "无意识地",
	}
	clicheWords = []string{
		"仿佛", "犹如", "宛若", "如同", "一丝", "一抹", "些许", "几分",
		"隐约", "微不可察", "毫无征兆", "几不可闻", "眼中闪过", "嘴角勾起",
		"心中涌起", "心底泛起", "心头一震", "深吸一口气", "心中暗道",
	}
	simileWords = []string{
		"好像", "仿佛", "犹如", "如同", "宛若", "宛如",
	}
	weakAdverbs = []string{
		"微微", "淡淡", "缓缓", "轻轻",
	}
)

// AdvisoryIssues 返回正文里的去 AI 味启发式标记（非阻塞）。用于写后自检参考。
func AdvisoryIssues(s string) []DeslopIssue {
	var issues []DeslopIssue

	for _, r := range advisoryRegexRules {
		if r.re.MatchString(s) {
			issues = append(issues, DeslopIssue{Rule: r.rule, Message: r.message})
		}
	}

	if c := countAny(s, microActionWords); c >= 3 {
		issues = append(issues, DeslopIssue{Rule: "stock-reaction-tic", Matched: fmt.Sprintf("%d处", c),
			Message: "微动作/套式反应复现偏多，逐处删除只标注情绪却无信息的身体细节，不换部位或同义动作"})
	}
	if c := countAny(s, clicheWords); c >= 4 {
		issues = append(issues, DeslopIssue{Rule: "cliche-density-tic", Matched: fmt.Sprintf("%d处", c),
			Message: "套词（仿佛/一丝/一抹/深吸一口气等）密度偏高，整段回到角色当下的动作、物件、后果"})
	}
	if c := countAny(s, simileWords); c >= 6 {
		issues = append(issues, DeslopIssue{Rule: "metaphor-density-tic", Matched: fmt.Sprintf("%d处", c),
			Message: "比喻标记（好像/仿佛/如同）成片出现，保留一两个最有功能的，其余改白描"})
	}
	thousand := len([]rune(s)) / 1000
	if thousand < 1 {
		thousand = 1
	}
	if c := countAny(s, weakAdverbs); c > 3*thousand {
		issues = append(issues, DeslopIssue{Rule: "weak-adverb-tic", Matched: fmt.Sprintf("%d处", c),
			Message: "弱化副词（微微/淡淡/缓缓/轻轻）密度超过每千字3个，孤立自然使用可保留，成串时替换"})
	}
	return issues
}

func countAny(s string, words []string) int {
	c := 0
	for _, w := range words {
		c += strings.Count(s, w)
	}
	return c
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// contextAround 返回命中处所在的句子片段：向两侧尽量扩到句界
// （。！？换行/引号），最多向前 24 字、向后 30 字。用于拒绝消息中
// 精确定位命中句，引导 LLM 只改写命中处而非重写整章。
func contextAround(s string, start, end int) string {
	runes := []rune(s)
	lo, hi := runeIndexAt(s, start), runeIndexAt(s, end)
	if lo > hi || lo >= len(runes) {
		return ""
	}

	left := lo
	if minLeft := lo - 24; minLeft > 0 {
		left = minLeft
		for left < lo && !isSentenceBoundary(runes[left]) {
			left++
		}
	} else {
		left = 0
	}
	if left < lo && isSentenceBoundary(runes[left]) && left+1 <= lo {
		left++ // 句界标点不作为片段首字符
	}

	right := hi
	maxRight := hi + 30
	if maxRight > len(runes) {
		maxRight = len(runes)
	}
	for right < maxRight && !isSentenceBoundary(runes[right]) {
		right++
	}
	if right < len(runes) && isSentenceBoundary(runes[right]) {
		right++ // 包含句末标点
	}
	return strings.TrimSpace(string(runes[left:right]))
}

// runeIndexAt 把字节偏移换算为 rune 下标。
func runeIndexAt(s string, byteOff int) int {
	if byteOff >= len(s) {
		return len([]rune(s))
	}
	return len([]rune(s[:byteOff]))
}

func isSentenceBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '\n', '"', '\u201c', '\u201d', '…':
		return true
	}
	return false
}
