package v170

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// 四维结构化字段「尽力填充」的确定性规则（WBS 2.8 / G6）。
//
// 原则（勿回退）：宁缺勿滥——只在**单一无歧义命中**时写、只写空字段、永不覆盖已填值、
// 永不改写自由文本源字段。多组同时命中（如「亦师亦敌」）一律不写，留空交由
// 2.6 写路径 / 作者 / LLM 维护。无可靠机械规则的字段（appearance_anchors、
// voice_traits、capability_ceiling、location_ref、faction、role、locations.controller、
// story_arcs.motif 等）不在迁移中猜测，保持空值。
//
// 全部规则均为纯函数，便于单测锁定行为。

// keywordRule 是「值 + 词级正则」规则项。
type keywordRule struct {
	value string
	pat   *regexp.Regexp
}

// 关系软枚举词级规则（WBS §0.4 R1：relation_describe 仍为主，relation_kind 可空）。
// 词级而非单字匹配，避免「师父」误判为 kin、「师」单独出现误判等。
var relationKindPats = []keywordRule{
	{value: "mentor", pat: regexp.MustCompile(`师父|师傅|老师|导师|师徒|徒弟|弟子|恩师|拜师学艺|传授`)},
	{value: "rival", pat: regexp.MustCompile(`仇人|仇敌|宿敌|死敌|敌人|对手|对峙|翻脸|闹翻|决裂|反目|报复|仇恨|敌对`)},
	{value: "lover", pat: regexp.MustCompile(`夫妻|妻子|丈夫|爱人|恋人|情侣|相爱|结婚|成亲|婚约|媳妇|老公|老婆|表白`)},
	{value: "kin", pat: regexp.MustCompile(`之父|之母|之子|之女|之兄|之弟|之姐|之妹|表侄|表叔|表舅|表兄|表弟|表姐|表妹|堂兄|堂弟|堂姐|堂妹|侄子|侄女|外甥|岳父|公公|婆婆|姐夫|妹夫|嫂子|弟媳|父子|父女|母子|母女|结拜`)},
}

// matchRelationKind 从关系自由文本推断软枚举；多组命中或零命中返回空串（不写）。
func matchRelationKind(describe, description string) string {
	return singleMatch(relationKindPats, describe+"\n"+description)
}

// 地点存续状态强模式（WBS §0.4 冻结：intact|destroyed|blocked）。
// intact 无强模式可依（缺证据≠完好），保持空串。
var locationStatePats = []keywordRule{
	{value: "destroyed", pat: regexp.MustCompile(`已毁|被毁|已塌|坍塌|塌毁|烧毁|炸毁|摧毁|夷为平地|沦为废墟|化为废墟|成为废墟`)},
	{value: "blocked", pat: regexp.MustCompile(`封住|封着|封死|堵死|被封|封锁|拦死|无法进入|进不去|不许进入|戒严`)},
}

// matchLocationState 从地点自由文本推断存续状态；多组命中或零命中返回空串（不写）。
func matchLocationState(texts ...string) string {
	return singleMatch(locationStatePats, strings.Join(texts, "\n"))
}

// 角色性别断言模式：描述中的「X之父/之母」亲属断言（描述为「陈三斗之父」时该角色
// 即其父，属自我断言；「他娘让他……」这类第三方指称已刻意排除，防误判）。
var genderPats = []keywordRule{
	{value: "男", pat: regexp.MustCompile(`之父|之子|之兄|之弟`)},
	{value: "女", pat: regexp.MustCompile(`之母|之女|之姐|之妹`)},
}

// matchCharacterGender 从姓名与描述推断性别；多组命中或零命中返回空串（不写）。
// 姓名裸性别字（如「姓何的女人」「姓李的夹克男」）只认姓名不认描述（描述里
// 「他的妻子」等第三方指称太常见）。
func matchCharacterGender(name, description string) string {
	seen := map[string]bool{}
	mark := func(v string) {
		if v != "" {
			seen[v] = true
		}
	}
	if name = strings.TrimSpace(name); name != "" {
		if strings.Contains(name, "男") {
			mark("男")
		}
		if strings.Contains(name, "女") {
			mark("女")
		}
	}
	for _, g := range genderPats {
		if g.pat.MatchString(description) {
			mark(g.value)
		}
	}
	if len(seen) == 1 {
		for v := range seen {
			return v
		}
	}
	return ""
}

// 年龄/出生年模式：仅取描述开头的自我陈述（如「28岁，退役水下工兵」「四十出头，……」），
// 且要求其后紧跟标点或结束（拦掉「七八岁的孩子」这类第三方指称）。
var agePat = regexp.MustCompile(`(?:[0-9]{1,3}|[零一二三四五六七八九十百]{1,6})(?:岁(?:出头|上下|来岁|左右|多|几)?|来岁|出头|上下)`)
var ageFollowPunct = "，。、；：,.;:!?！？\"」』）)]} \t\n"

// matchCharacterBirthYear 从描述开头提取年龄表述（虚构历法/约数用原字符串承载）；
// 无合规命中返回空串（不写）。
func matchCharacterBirthYear(description string) string {
	const window = 24
	trimmed := strings.TrimLeft(description, " \t\r\n\"「『（(")
	runes := []rune(trimmed)
	if len(runes) > window {
		runes = runes[:window]
	}
	loc := agePat.FindStringIndex(string(runes))
	if loc == nil {
		return ""
	}
	// 命中后必须是标点或结尾（窗口是 trimmed 的前缀，字节下标与 trimmed 对齐）。
	if rest := trimmed[loc[1]:]; rest != "" {
		if r, _ := utf8.DecodeRuneInString(rest); !strings.ContainsRune(ageFollowPunct, r) {
			return ""
		}
	}
	return trimmed[loc[0]:loc[1]]
}

// 别名模式：外号/别名/绰号/人称/又名 + 短词（如「外号水鬼苏」→ ["水鬼苏"]）。
var aliasPat = regexp.MustCompile(`(?:外号|别名|绰号|人称|又名)\s*[「『"]?([^，。；、！？\s"」』]{2,12})`)

// matchCharacterAliases 从描述提取别名，返回 JSON 数组字符串；无命中返回空串（不写）。
func matchCharacterAliases(description string) string {
	matches := aliasPat.FindAllStringSubmatch(description, 5)
	if len(matches) == 0 {
		return ""
	}
	seen := map[string]bool{}
	aliases := make([]string, 0, len(matches))
	for _, m := range matches {
		a := strings.TrimSpace(m[1])
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		aliases = append(aliases, a)
	}
	if len(aliases) == 0 {
		return ""
	}
	b, err := json.Marshal(aliases)
	if err != nil {
		return ""
	}
	return string(b)
}

// arcChar 是角色弧定位所需的最小角色引用。
type arcChar struct {
	ID   int64
	Name string
}

// matchStoryArcCharID 为角色弧（arc_type=character）定位角色：
//  1. 弧名「X：……」前缀与角色名精确一致 → 直接命中；
//  2. 否则在弧名里做角色名子串匹配，恰好一个命中才写，多命中/零命中不写。
//
// （别名不参与匹配：别名本身也是本次迁移才尽力提取的，不做规则间依赖。）
func matchStoryArcCharID(arcName string, chars []arcChar) int64 {
	arcName = strings.TrimSpace(arcName)
	if arcName == "" {
		return 0
	}
	if i := strings.IndexAny(arcName, "：:"); i > 0 {
		prefix := strings.TrimSpace(arcName[:i])
		for _, c := range chars {
			if c.Name != "" && c.Name == prefix {
				return c.ID
			}
		}
	}
	var hit int64
	hits := 0
	for _, c := range chars {
		if c.Name == "" || !strings.Contains(arcName, c.Name) {
			continue
		}
		hits++
		hit = c.ID
	}
	if hits == 1 {
		return hit
	}
	return 0
}

// singleMatch 统一「多组命中或零命中都不写」语义：返回唯一命中的值，否则空串。
func singleMatch(pats []keywordRule, text string) string {
	var hits []string
	for _, p := range pats {
		if p.pat.MatchString(text) {
			hits = append(hits, p.value)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}
