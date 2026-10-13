package skill

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// folder 是 Unicode 大小写折叠器（用于作者名归一，使 "Liu Cixin" 与 "liu cixin" 等同）。
var folder = cases.Fold()

// mentalLayerOrder 是 mental skill 五层 canonical 结构的标准顺序。
// 聚合产物严格按此顺序输出，保证与既有注入链路对五层结构的预期一致。
var mentalLayerOrder = []string{
	"叙事心智模型",
	"创作决策启发式",
	"价值观与反模式",
	"表达 DNA",
	"诚实边界",
}

// chapterRe 从条目文本中提取"第 N 章"形式的章节引用，用于尾注。
var chapterRe = regexp.MustCompile(`第\s*(\d+)\s*章`)

// MergeSource 携带一个来源 skill 及其书名溯源。
// BookTitle 用于尾注与 merged_books；novel 级来源 BookID 有值，user 级可为 0。
type MergeSource struct {
	Skill     *Skill
	BookTitle string // 来源书名；user 级聚合档案本身可参与二次聚合，其 merged_books 已含书名
	BookID    int64  // 仅 novel 级有值
}

// MergeOptions 是 MergeMentalSkills 的配置。
type MergeOptions struct {
	CanonicalAuthor string            // 归一后的规范作者名（派生文件 source_author / 文件名前缀）
	AliasMap        map[string]string // 别名→规范名（可选）
	OutputDir       string            // 派生文件落盘目录（通常 config.UserSkillsDir()）
	AggregatedBy    string            // 写 author 字段，默认 "aggregate"
	BookTitles      map[string]string // skill.Name → 书名（尾注与 merged_books 用，Backward 兼容；优先用 MergeSource.BookTitle）
}

// MergeMentalSkills 将同一规范作者的多本 mental skill 聚合为一个派生 skill。
//
// 设计要点：
//   - 作者名先经 normalizeAuthor（trim + NFKC + 大小写折叠 + 别名映射）归一并分组。
//   - 以"五层结构里的习惯条目"为对齐单元（按条目名 + 所属层对齐），非整文件拼接。
//   - 跨书复现（≥2 个不同书名出现同条目）→ stable:true，合并 appears_in、证据章节去重；
//     仅单书出现 → single-book（stable:false），保留但不计入稳定集。
//   - 正文每条目尾注：> 来源：《X》（第 N 章）。
//   - 不在此落盘；落盘由调用方 WriteSkill 完成。
//
// 本函数为 internal/skill 包内纯函数，不依赖 app/ 或 Wails。
func MergeMentalSkills(ctx context.Context, sources []MergeSource, opts MergeOptions) (*Skill, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("skill: 聚合失败，源 skill 为空")
	}

	// 1. 过滤掉派生聚合 skill（避免聚合的聚合，见设计文档 R5）。
	valid := make([]MergeSource, 0, len(sources))
	for _, s := range sources {
		if s.Skill == nil {
			continue
		}
		if s.Skill.Aggregated {
			continue
		}
		valid = append(valid, s)
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("skill: 聚合失败，没有可用的普通 mental skill 源")
	}

	// 2. 作者名归一；为空时从首个源推断。
	canonicalAuthor := normalizeAuthor(opts.CanonicalAuthor, opts.AliasMap)
	if canonicalAuthor == "" {
		canonicalAuthor = normalizeAuthor(valid[0].Skill.SourceAuthor, opts.AliasMap)
	}
	if canonicalAuthor == "" {
		canonicalAuthor = "unknown-author"
	}

	// 书名去重（有序，供 merged_books）。
	mergedBooks := make([]string, 0)
	bookSeen := make(map[string]bool)
	addBook := func(b string) {
		if b == "" || bookSeen[b] {
			return
		}
		bookSeen[b] = true
		mergedBooks = append(mergedBooks, b)
	}
	for _, s := range valid {
		addBook(s.BookTitle)
	}

	// 3. 条目级对齐：key = (归一层名, 归一条目名)。
	type mergedItem struct {
		layer      string   // 显示用层名（canonical 或首见原名）
		rawText    string   // 输出用原始条目文本（首源）
		bookTitles []string // 贡献该条目的去重书名（有序）
		stable     bool
	}
	itemsByKey := make(map[string]*mergedItem)
	itemOrder := make([]string, 0) // key 顺序（首次出现）

	for _, s := range valid {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		layers := parseMentalLayers(s.Skill.Content)
		for _, layer := range layers {
			layerNorm := normalizeLayer(layer.name)
			if !isCanonicalLayer(layerNorm) {
				continue // 仅聚合五层 canonical 结构，其余层（如"验证与降级说明"）丢弃
			}
			display := canonicalLayerName(layerNorm)
			for _, it := range layer.items {
				key := layerNorm + "\x00" + normalizeItemKey(it)
				mi, ok := itemsByKey[key]
				if !ok {
					mi = &mergedItem{layer: display, rawText: it}
					itemsByKey[key] = mi
					itemOrder = append(itemOrder, key)
				}
				if !contains(mi.bookTitles, s.BookTitle) {
					mi.bookTitles = append(mi.bookTitles, s.BookTitle)
				}
			}
		}
	}

	// 4. 判定 stable：跨 ≥2 个不同书名出现。
	for _, key := range itemOrder {
		mi := itemsByKey[key]
		mi.stable = len(uniqueStrings(mi.bookTitles)) >= 2
	}

	// 5. 组装派生正文：按五层 canonical 顺序，stable 优先。
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s 的创作心智\n\n", canonicalAuthor))

	stableCount := 0
	singleCount := 0

	for _, layerName := range mentalLayerOrder {
		var stableItems, singleItems []*mergedItem
		for _, key := range itemOrder {
			mi := itemsByKey[key]
			if normalizeLayer(mi.layer) != normalizeLayer(layerName) {
				continue
			}
			if mi.stable {
				stableItems = append(stableItems, mi)
			} else {
				singleItems = append(singleItems, mi)
			}
		}
		if len(stableItems) == 0 && len(singleItems) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("## %s\n\n", layerName))
		for _, mi := range stableItems {
			stableCount++
			sb.WriteString("- " + mi.rawText + "\n")
			sb.WriteString(buildFootnotes(mi.bookTitles, mi.rawText))
		}
		for _, mi := range singleItems {
			singleCount++
			sb.WriteString("- " + mi.rawText + "\n")
			sb.WriteString(buildFootnotes(mi.bookTitles, mi.rawText))
		}
		sb.WriteString("\n")
	}

	// 6. aggregated_from：去重源 skill 名（输入顺序）。
	aggFrom := make([]string, 0)
	fromSeen := make(map[string]bool)
	for _, s := range valid {
		n := s.Skill.Name
		if n == "" || fromSeen[n] {
			continue
		}
		fromSeen[n] = true
		aggFrom = append(aggFrom, n)
	}

	aggregatedBy := opts.AggregatedBy
	if aggregatedBy == "" {
		aggregatedBy = "aggregate"
	}

	derived := &Skill{
		Name:            SanitizeFileName(canonicalAuthor) + "-mental",
		Description:     fmt.Sprintf("创作心智·%s 跨书聚合档案", canonicalAuthor),
		Category:        "创作心智",
		Kind:            "mental",
		Mode:            ModeAuto,
		Author:          aggregatedBy,
		SourceAuthor:    canonicalAuthor,
		Version:         1,
		Content:         strings.TrimRight(sb.String(), "\n"),
		Aggregated:      true,
		AggregatedFrom:  aggFrom,
		MergedBooks:     mergedBooks,
		StableItems:     stableCount,
		SingleBookItems: singleCount,
		AggregatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	return derived, nil
}

// normalizeAuthor 对作者名做归一：trim + NFKC + Unicode 大小写折叠 + 别名映射。
// 空名返回空串；别名映射命中后会对映射结果再归一（支持别名本身也含大小写差异）。
func normalizeAuthor(raw string, aliasMap map[string]string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	if aliasMap != nil {
		if mapped, ok := aliasMap[name]; ok && mapped != "" {
			name = mapped
		}
	}
	name = norm.NFKC.String(name)
	name = folder.String(name)
	return strings.Join(strings.Fields(name), " ")
}

// NormalizeAuthor 导出 normalizeAuthor，供 app 层在过滤源前预归一作者名。
func NormalizeAuthor(raw string, aliasMap map[string]string) string {
	return normalizeAuthor(raw, aliasMap)
}

// normalizeLayer 归一化层名（用于对齐/匹配 canonical 层）。
func normalizeLayer(name string) string {
	n := strings.TrimSpace(name)
	n = norm.NFKC.String(n)
	n = folder.String(n)
	return strings.Join(strings.Fields(n), " ")
}

// normalizeItemKey 归一化条目文本，用于跨书对齐。
func normalizeItemKey(text string) string {
	t := norm.NFKC.String(text)
	t = folder.String(t)
	return strings.Join(strings.Fields(t), " ")
}

// isCanonicalLayer 判断归一后的层名是否属于五层 canonical 结构。
func isCanonicalLayer(normName string) bool {
	for _, l := range mentalLayerOrder {
		if normalizeLayer(l) == normName {
			return true
		}
	}
	return false
}

// canonicalLayerName 返回归一层名对应的 canonical 显示名。
func canonicalLayerName(normName string) string {
	for _, l := range mentalLayerOrder {
		if normalizeLayer(l) == normName {
			return l
		}
	}
	return normName
}

// parseMentalLayers 将 mental skill 正文解析为有序的"层 → 条目"结构。
// 仅识别 `## ` 二级标题为层；bullet（-/*/+）与纯文本行都视为条目；
// `>` 尾注行与 `# ` 顶层标题被跳过（聚合时尾注重新生成）。
func parseMentalLayers(content string) []parsedLayer {
	layers := make([]parsedLayer, 0)
	current := -1
	lines := strings.Split(content, "\n")
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			continue // 顶层标题
		}
		if strings.HasPrefix(trimmed, "## ") {
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
			layers = append(layers, parsedLayer{name: name})
			current = len(layers) - 1
			continue
		}
		if strings.HasPrefix(trimmed, ">") {
			continue // 尾注，跳过
		}
		if current < 0 {
			continue // 层标题之前的内容，忽略
		}
		item := trimmed
		if strings.HasPrefix(item, "- ") || strings.HasPrefix(item, "* ") || strings.HasPrefix(item, "+ ") {
			item = strings.TrimSpace(item[2:])
		}
		if item == "" {
			continue
		}
		layers[current].items = append(layers[current].items, item)
	}
	return layers
}

// parsedLayer 是解析出的单层及其条目。
type parsedLayer struct {
	name  string
	items []string
}

// buildFootnotes 根据条目贡献的书名与（可选的）章节引用生成尾注。
// 格式：> 来源：《X》（第 N 章）或 > 来源：《X》。多书则多行。
func buildFootnotes(books []string, rawText string) string {
	chapter := extractChapter(rawText)
	var sb strings.Builder
	for _, b := range books {
		if b == "" {
			continue
		}
		if chapter != "" {
			sb.WriteString(fmt.Sprintf("> 来源：《%s》（%s）\n", b, chapter))
		} else {
			sb.WriteString(fmt.Sprintf("> 来源：《%s》\n", b))
		}
	}
	return sb.String()
}

// extractChapter 从条目文本中提取首个"第 N 章"引用。
func extractChapter(text string) string {
	m := chapterRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return "第 " + m[1] + " 章"
}

// contains 判断字符串切片是否包含某元素。
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// uniqueStrings 返回去重后的字符串切片（保持首次出现顺序）。
func uniqueStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// GroupByAuthor 将 skill 元数据按（归一后的）source_author 分组。
// 仅纳入 kind=mental 的条目；用于 App 层枚举"同作者心智档案"候选。
// aliasMap 为可选别名映射（nil 表示不应用别名）。
func GroupByAuthor(metas []SkillMeta, aliasMap map[string]string) map[string][]SkillMeta {
	groups := make(map[string][]SkillMeta)
	for _, m := range metas {
		if m.Kind != "mental" {
			continue
		}
		author := normalizeAuthor(m.SourceAuthor, aliasMap)
		if author == "" {
			continue
		}
		groups[author] = append(groups[author], m)
	}
	return groups
}
