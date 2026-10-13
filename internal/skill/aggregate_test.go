package skill

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildMentalSkill 构造一个带五层结构与 bullet 条目的 mental skill。
func buildMentalSkill(name, author, content string) *Skill {
	return &Skill{
		Name:         name,
		Description:  "desc",
		Category:     "创作心智",
		Kind:         "mental",
		Mode:         ModeAuto,
		Author:       author,
		SourceAuthor: author,
		Version:      1,
		Content:      content,
	}
}

const (
	layerMental  = "## 叙事心智模型\n\n- 信息 withholding 与命运不可控\n- 冷峻的第三人称旁观\n\n"
	layerExpr    = "## 表达 DNA\n\n- 短句为主\n\n"
	layerValues  = "## 价值观与反模式\n\n- 拒绝廉价团圆\n\n"
	layerHonesty = "## 诚实边界\n\n- 推断自虚构文本\n\n"
)

func TestNormalizeAuthor_Basic(t *testing.T) {
	// trim + 大小写折叠
	assert.Equal(t, "liu cixin", normalizeAuthor("Liu Cixin ", nil))
	assert.Equal(t, "liu cixin", normalizeAuthor("LIU   CIXIN", nil))
	// 中文身份不变
	assert.Equal(t, "刘慈欣", normalizeAuthor("刘慈欣 ", nil))
	// 空名
	assert.Equal(t, "", normalizeAuthor("   ", nil))
}

func TestNormalizeAuthor_Alias(t *testing.T) {
	alias := map[string]string{"Liu Cixin": "刘慈欣"}
	// 英文经别名映射后与中文归为同一规范名
	assert.Equal(t, "刘慈欣", normalizeAuthor("Liu Cixin", alias))
	assert.Equal(t, "刘慈欣", normalizeAuthor("刘慈欣 ", alias))
	// 非命中别名保持原值（仅折叠）
	assert.Equal(t, "someone else", normalizeAuthor("Someone Else", alias))
}

func TestMergeMentalSkills_CrossBookStable(t *testing.T) {
	srcA := buildMentalSkill("a-san", "Liu Cixin",
		"# Liu Cixin 的创作心智\n\n"+layerMental+layerExpr+layerValues+layerHonesty)
	// srcB 与 srcA 共享「信息 withholding」「短句为主」「拒绝廉价团圆」「推断自虚构文本」四条（跨书复现→stable），
	// 另有各自独有条目（单书→single-book）。作者名大小写不同，应归同组。
	srcB := buildMentalSkill("b-qiu", "liu cixin",
		"# Liu Cixin 的创作心智\n\n## 叙事心智模型\n\n- 信息 withholding 与命运不可控\n- 另一种母题\n\n"+layerExpr+layerValues+layerHonesty)

	sources := []MergeSource{
		{Skill: srcA, BookTitle: "三体", BookID: 1},
		{Skill: srcB, BookTitle: "球状闪电", BookID: 2},
	}
	derived, err := MergeMentalSkills(context.Background(), sources, MergeOptions{})
	require.NoError(t, err)

	// 跨书复现条目 4 条 → stable=4；单书条目（冷峻旁观、另一种母题）→ single=2
	assert.Equal(t, 4, derived.StableItems)
	assert.Equal(t, 2, derived.SingleBookItems)

	// merged_books 去重且有序
	assert.Equal(t, []string{"三体", "球状闪电"}, derived.MergedBooks)
	// aggregated_from 为两个源名
	assert.ElementsMatch(t, []string{"a-san", "b-qiu"}, derived.AggregatedFrom)

	// frontmatter 权威字段
	assert.True(t, derived.Aggregated)
	assert.Equal(t, "liu cixin", derived.SourceAuthor)
	assert.Equal(t, "aggregate", derived.Author)
	assert.Equal(t, "mental", derived.Kind)
	assert.Equal(t, ModeAuto, derived.Mode)
	assert.Equal(t, "创作心智", derived.Category)
	assert.Equal(t, 1, derived.Version)
	assert.Equal(t, "liu cixin-mental", derived.Name)
	assert.NotEmpty(t, derived.AggregatedAt)

	// 稳定条目标注两个来源
	assert.Contains(t, derived.Content, "信息 withholding 与命运不可控")
	assert.Contains(t, derived.Content, "> 来源：《三体》")
	assert.Contains(t, derived.Content, "> 来源：《球状闪电》")
	// 五层结构齐全
	for _, l := range []string{"叙事心智模型", "表达 DNA", "价值观与反模式", "诚实边界"} {
		assert.Contains(t, derived.Content, "## "+l)
	}
}

func TestMergeMentalSkills_SingleBookOnly(t *testing.T) {
	src := buildMentalSkill("only-one", "Liu Cixin",
		"# Liu Cixin 的创作心智\n\n"+layerMental)
	sources := []MergeSource{{Skill: src, BookTitle: "三体", BookID: 1}}
	derived, err := MergeMentalSkills(context.Background(), sources, MergeOptions{})
	require.NoError(t, err)

	assert.Equal(t, 0, derived.StableItems)
	assert.Equal(t, 2, derived.SingleBookItems) // 信息 withholding + 冷峻旁观
	assert.Equal(t, []string{"三体"}, derived.MergedBooks)
	assert.Contains(t, derived.Content, "> 来源：《三体》")
}

func TestMergeMentalSkills_ChapterFootnote(t *testing.T) {
	content := "# X\n\n## 叙事心智模型\n\n- 关键母题在第 12 章首次显现\n\n"
	src := buildMentalSkill("ch", "X", content)
	derived, err := MergeMentalSkills(context.Background(), []MergeSource{{Skill: src, BookTitle: "书A"}}, MergeOptions{})
	require.NoError(t, err)
	assert.Contains(t, derived.Content, "> 来源：《书A》（第 12 章）")
}

func TestMergeMentalSkills_ExcludesAggregated(t *testing.T) {
	normal := buildMentalSkill("normal", "Liu Cixin", "# X\n\n"+layerMental)
	derived2 := buildMentalSkill("derived", "Liu Cixin", "# X\n\n"+layerMental)
	derived2.Aggregated = true // 派生聚合源，必须被排除

	sources := []MergeSource{
		{Skill: normal, BookTitle: "三体"},
		{Skill: derived2, BookTitle: "用户"},
	}
	derived, err := MergeMentalSkills(context.Background(), sources, MergeOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"三体"}, derived.MergedBooks)
	assert.ElementsMatch(t, []string{"normal"}, derived.AggregatedFrom)
}

func TestMergeMentalSkills_NoSources(t *testing.T) {
	_, err := MergeMentalSkills(context.Background(), nil, MergeOptions{})
	assert.Error(t, err)
}

func TestMergeMentalSkills_SerializeRoundTrip(t *testing.T) {
	srcA := buildMentalSkill("a", "Liu Cixin", "# X\n\n"+layerMental+layerExpr)
	srcB := buildMentalSkill("b", "Liu Cixin", "# X\n\n"+layerMental+strings.Replace(layerExpr, "短句为主", "长句铺陈", 1))
	derived, err := MergeMentalSkills(context.Background(), []MergeSource{
		{Skill: srcA, BookTitle: "三体"},
		{Skill: srcB, BookTitle: "球状闪电"},
	}, MergeOptions{})
	require.NoError(t, err)

	text, err := Serialize(derived)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(text, "---\n"))

	// 反解析后聚合字段应齐全（layerMental 中的「冷峻旁观」两条源均有 → stable=2）
	parsed, err := ParseBytes([]byte(text), "user")
	require.NoError(t, err)
	assert.True(t, parsed.Aggregated)
	assert.Equal(t, []string{"三体", "球状闪电"}, parsed.MergedBooks)
	assert.Equal(t, 2, parsed.StableItems)
	assert.ElementsMatch(t, []string{"a", "b"}, parsed.AggregatedFrom)
	assert.Equal(t, "liu cixin", parsed.SourceAuthor)
}

func TestGroupByAuthor(t *testing.T) {
	metas := []SkillMeta{
		{Name: "a", Kind: "mental", SourceAuthor: "Liu Cixin "},
		{Name: "b", Kind: "mental", SourceAuthor: "liu cixin"},
		{Name: "c", Kind: "struct", SourceAuthor: "Liu Cixin"}, // 非 mental，排除
	}
	groups := GroupByAuthor(metas, nil)
	assert.Len(t, groups, 1)
	assert.Len(t, groups["liu cixin"], 2)
}
