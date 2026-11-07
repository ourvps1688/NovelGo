package character

// 生死态派生（#103 第三层）：characters.alive 原本是「人工维护的独立列」，与正文里的
// 死亡事实天然脱节，导致巡检按列判「台账记错」、修复/复验反复空转。这里让 statesync
// 链路在每章写完后**从正文本地派生**生死态，不再依赖 LLM 记得调 update_character。
//
// 口径（勿回退）：
//   - 只读「角色名/别名 + 紧邻的完成态死亡谓词」，宁漏勿误：假设、否定、比喻、回忆、
//     疑问、他人所有格、单字名都不派生。漏掉的交给 statesync 子 Agent 读正文补齐。
//   - **单向**：只做 在世→已死，绝不复活；复活/误标纠正一律走作者人工入口（retcon）。
//   - 幂等：已标死角色重复跑不产生写入、不重复记履历。
//   - **人工裁决优先**：作者经人工入口改过 alive（履历 source=author/retcon）后，
//     本地派生不再覆盖该角色，避免「刚纠正误标，重跑同一章同步又被同一句正文标死」。
//   - 写入走 GORM Save，复用 ledger_temporal 回调自动记 ledger_change_history，
//     不另造审计通道；调用方经 storage.WithLedgerSource 标注来源。

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/storage"
)

// DeathMention 是一条「正文明写某角色死亡」的本地证据。
type DeathMention struct {
	CharacterID int64  `json:"character_id"`
	Name        string `json:"name"`
	Excerpt     string `json:"excerpt"`
}

// deathPredicates 完成态死亡谓词。刻意不收录单字「死」（会命中「死里逃生」「死心」
// 等假阳），也不收录「倒下」「闭眼」等可能只是受伤/昏厥的描写。
var deathPredicates = []string{
	"再也没能醒来", "再也没能起来", "永远闭上了眼", "停止了呼吸", "没有了呼吸",
	"倒在了血泊中", "倒在血泊中", "一命呜呼", "咽了气", "断了气", "被处死",
	"被刺死", "命丧", "气绝", "咽气", "断气", "毙命", "丧命", "殒命", "身亡",
	"身死", "遇害", "被斩", "被杀", "战死", "死去", "死亡", "已死", "已故",
	"死了", "陨落", "殒落",
}

// deathGapRe 允许名字与谓词之间存在极短连接（标点/完成态副词）。
// 刻意不允许「的」：角色名 + 「的父亲/的师父/的手下」属于他人死亡，不得派生。
// 也刻意不吞「已/已经」：它们是「已死」「已故」等谓词本身的前缀，吞掉会漏报。
var deathGapRe = regexp.MustCompile(`^[，,、\s：:「」“”"']{0,3}(?:便|就|终于|当场|就此|彻底|再也不|永远|也随之|竟|果然|最终)?`)

// deathPrefixExclusions 同句内、角色名之前的否定/假设/转述/假死信号。
var deathPrefixExclusions = []string{
	"没死", "没有死", "未死", "差点", "险些", "几乎", "倘若", "如果", "假如",
	"要是", "若是", "像是", "仿佛", "似乎", "梦见", "回忆中", "传闻", "听说",
	"假死", "装死", "诈死", "死里逃生", "别死", "不要死", "不想死", "以为",
	"误以为", "并没有", "并非", "怎么可能", "难道",
}

// deathSuffixExclusions 谓词之后的比喻/疑问/否定尾巴。
var deathSuffixExclusions = []string{"死了心", "死了这条心", "死了吗", "死了没有", "死去活来"}

// ScanDeathMentions 从章节正文召回「角色名 + 紧邻死亡谓词」的候选。
// chars 为本书角色台账（应已按 novel_id 过滤）；返回按角色去重后的证据列表。
func ScanDeathMentions(content string, chars []Character) []DeathMention {
	if strings.TrimSpace(content) == "" || len(chars) == 0 {
		return nil
	}
	type nameRef struct {
		id   int64
		name string
	}
	var refs []nameRef
	seen := map[string]bool{}
	add := func(id int64, name string) {
		name = strings.TrimSpace(name)
		// 单字名/纯符号名误匹配率过高（「林中死了人」），保守跳过；交给 LLM 复核。
		if utf8.RuneCountInString(name) < 2 || seen[name] {
			return
		}
		seen[name] = true
		refs = append(refs, nameRef{id: id, name: name})
	}
	for _, ch := range chars {
		add(ch.ID, ch.Name)
		for _, alias := range parseAliases(ch.Aliases) {
			add(ch.ID, alias)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	// 长名优先，避免「赵彦」抢先匹配掉「赵彦之父」这类台账里的独立角色。
	sort.SliceStable(refs, func(i, j int) bool {
		return utf8.RuneCountInString(refs[i].name) > utf8.RuneCountInString(refs[j].name)
	})

	preds := append([]string(nil), deathPredicates...)
	sort.SliceStable(preds, func(i, j int) bool { return len(preds[i]) > len(preds[j]) })
	parts := make([]string, 0, len(refs)+len(preds))
	for _, r := range refs {
		parts = append(parts, regexp.QuoteMeta(r.name))
	}
	for _, p := range preds {
		parts = append(parts, regexp.QuoteMeta(p))
	}
	// 一次扫描找出所有「名字或谓词」出现点，再逐个做上下文校验，避免为每个角色
	// 单独跑一遍全文正则（长篇 + 上千角色时开销明显）。
	tokenRe := regexp.MustCompile(strings.Join(parts, "|"))
	byName := map[string]int64{}
	for _, r := range refs {
		byName[r.name] = r.id
	}

	out := make([]DeathMention, 0)
	got := map[int64]bool{}
	for _, loc := range tokenRe.FindAllStringIndex(content, -1) {
		token := content[loc[0]:loc[1]]
		id, isName := byName[token]
		if !isName || got[id] {
			continue
		}
		rest := content[loc[1]:]
		gap := deathGapRe.FindString(rest)
		after := rest[len(gap):]
		pred := longestMatchingPredicate(after, preds)
		if pred == "" || !deathSentenceOK(content, loc[0], pred+after[len(pred):], after[len(pred):]) {
			continue
		}
		got[id] = true
		out = append(out, DeathMention{
			CharacterID: id,
			Name:        token,
			Excerpt:     sentenceExcerpt(content, loc[0], loc[1]+len(gap)+len(pred)),
		})
	}
	return out
}

// longestMatchingPredicate 返回 after 开头能命中的最长谓词。
func longestMatchingPredicate(after string, preds []string) string {
	for _, p := range preds { // preds 已按长度降序
		if strings.HasPrefix(after, p) {
			return p
		}
	}
	return ""
}

// deathSentenceOK 做同句否定/假设窗口、比喻后缀与疑问句排雷。
// full 是「谓词 + 后缀」（用于比对死了心这类 idiom），tail 是谓词之后的文本。
func deathSentenceOK(content string, nameStart int, full, tail string) bool {
	for _, ex := range deathSuffixExclusions {
		if strings.HasPrefix(full, ex) {
			return false
		}
	}
	// 疑问句（「赵彦死了？」）一律不派生。
	trimmed := strings.TrimLeft(tail, " \t")
	if strings.HasPrefix(trimmed, "？") || strings.HasPrefix(trimmed, "?") ||
		strings.HasPrefix(trimmed, "吗") || strings.HasPrefix(trimmed, "么") {
		return false
	}
	prefix := runePrefix(content, nameStart, 24)
	if i := strings.LastIndexAny(prefix, "。！？!?\n；;"); i >= 0 {
		prefix = prefix[i+1:]
	}
	for _, ex := range deathPrefixExclusions {
		if strings.Contains(prefix, ex) {
			return false
		}
	}
	return true
}

func runePrefix(s string, end int, n int) string {
	start := end
	for i := 0; i < n && start > 0; i++ {
		_, size := utf8.DecodeLastRuneInString(s[:start])
		start -= size
	}
	return s[start:end]
}

func sentenceExcerpt(content string, start, end int) string {
	lo := strings.LastIndexAny(content[:start], "。！？!?\n")
	if lo < 0 {
		lo = 0
	} else {
		lo++
	}
	hi := strings.IndexAny(content[end:], "。！？!?\n")
	if hi < 0 {
		hi = len(content)
	} else {
		hi = end + hi + 1
	}
	ex := strings.TrimSpace(strings.ReplaceAll(content[lo:hi], "\n", " "))
	if utf8.RuneCountInString(ex) > 120 {
		ex = string([]rune(ex)[:120]) + "…"
	}
	return ex
}

func parseAliases(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		// 台账允许自由文本，解析失败时退化为按常见分隔符切分。
		return strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '，' || r == '、' || r == ';' || r == '；'
		})
	}
	return list
}

// authorAliveOverride 判断作者是否已对该角色生死态做过人工裁决（最后一次 alive 履历
// 非 auto）。派生写入标 auto，作者/retcon 写入标 author/retcon。履历表缺失或无记录时
// 返回 false（保护不生效，派生照常），保证旧库/精简 DB 环境不被阻断。
func authorAliveOverride(db *gorm.DB, novelID, characterID int64) bool {
	var row storage.LedgerChangeHistory
	err := db.Where("novel_id = ? AND entry_table = ? AND entry_id = ? AND field = ?",
		novelID, "characters", characterID, "alive").
		Order("id DESC").First(&row).Error
	if err != nil {
		return false
	}
	return row.Source != storage.HistorySourceAuto
}

// DeriveAliveFromChapter 把本章正文中明写的角色死亡单向派生到 characters.alive。
// 返回本次**新标死**的角色（已标死的不会重复返回），供 statesync 指令复核使用。
func DeriveAliveFromChapter(ctx context.Context, db *gorm.DB, novelID int64, content string) ([]DeathMention, error) {
	if db == nil || strings.TrimSpace(content) == "" {
		return nil, nil
	}
	var chars []Character
	if err := db.WithContext(ctx).Where("novel_id = ?", novelID).Find(&chars).Error; err != nil {
		return nil, fmt.Errorf("list characters for alive derivation: %w", err)
	}
	mentions := ScanDeathMentions(content, chars)
	derived := make([]DeathMention, 0, len(mentions))
	for _, m := range mentions {
		var ch Character
		if err := db.WithContext(ctx).
			Where("id = ? AND novel_id = ?", m.CharacterID, novelID).
			First(&ch).Error; err != nil {
			return derived, fmt.Errorf("load character %d: %w", m.CharacterID, err)
		}
		// 单向 + 幂等：已标死角色跳过，绝不复活、不重复写履历。
		if ch.Alive != nil && !*ch.Alive {
			continue
		}
		// 人工裁决优先：作者刚改回在世（或手动设过状态）后不再自动覆盖。
		if authorAliveOverride(db, novelID, ch.ID) {
			continue
		}
		dead := false
		ch.Alive = &dead
		if err := db.WithContext(ctx).Save(&ch).Error; err != nil {
			return derived, fmt.Errorf("derive alive=false for character %d: %w", ch.ID, err)
		}
		derived = append(derived, m)
	}
	return derived, nil
}
