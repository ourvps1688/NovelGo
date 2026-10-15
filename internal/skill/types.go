package skill

// Mode 常量。
const (
	ModeAuto   = "auto"   // 出现在 skill catalog，AI 可自主 invoke，用户也可 / 触发
	ModeManual = "manual" // 不出现在 catalog，仅用户 / 手动触发
	ModeAlways = "always" // 不出现在 catalog，会话开头注入 system 消息，常驻生效
)

// UseWhenEntry 是「何时使用」意图线索的一条。
// Intent 是人类可读的意图描述（用于 catalog 路由表、用户侧提示）；
// Cues 是命中关键词/短语（小写，对用户消息做子串匹配，确定性、零开销）。
type UseWhenEntry struct {
	Intent string   `json:"intent" yaml:"intent"`
	Cues   []string `json:"cues" yaml:"cues"`
}

// normalizeMode 兼容旧值并校验，非法值返回 "auto"。
func normalizeMode(raw string) string {
	switch raw {
	case "on_demand", "auto", "":
		return ModeAuto
	case "manual":
		return ModeManual
	case "always":
		return ModeAlways
	default:
		return ModeAuto
	}
}

// Skill 是一个完整的 skill 定义，包含元数据和正文内容。
type Skill struct {
	Name         string         `json:"name" yaml:"name"`
	Description  string         `json:"description" yaml:"description"`
	Category     string         `json:"category" yaml:"category"` // 分类，未来可基于不同分类组合实现 skill 矩阵
	Kind         string         `json:"kind" yaml:"kind"`         // 提取类型：留空/struct=套路模板，mental=创作心智
	Mode         string         `json:"mode" yaml:"mode"`
	UseWhen      []UseWhenEntry `json:"use_when,omitempty" yaml:"useWhen,omitempty"` // 结构化「何时使用」意图线索，供 LLM 预路由与用户侧提示
	Author       string         `json:"author" yaml:"author"`
	SourceAuthor string         `json:"source_author" yaml:"source_author"` // 仅 mental 有意义：被蒸馏心智所属的作者（V3 跨书聚合归并键）
	Version      int            `json:"version" yaml:"version"`
	Content      string         `json:"content" yaml:"-"`
	RawContent   string         `json:"raw_content,omitempty" yaml:"-"` // 原始 markdown 全文（含 YAML frontmatter）

	// —— 以下为 V3 跨书聚合（cross-book mental-skill aggregation）派生字段 ——
	// 仅当 Aggregated=true 的派生 skill 使用；普通 skill 这些字段为零值，序列化时被 yaml 忽略。
	Aggregated      bool     `json:"aggregated" yaml:"aggregated,omitempty"`               // 是否为跨书聚合派生 skill
	AggregatedFrom  []string `json:"aggregated_from" yaml:"aggregated_from,omitempty"`     // 参与合并的源 skill 名清单
	MergedBooks     []string `json:"merged_books" yaml:"merged_books,omitempty"`           // 去重后的来源书名清单
	StableItems     int      `json:"stable_items" yaml:"stable_items,omitempty"`           // 跨书复现（stable）条目数
	SingleBookItems int      `json:"single_book_items" yaml:"single_book_items,omitempty"` // 单书（single-book）条目数
	AggregatedAt    string   `json:"aggregated_at" yaml:"aggregated_at,omitempty"`         // 聚合时间，RFC3339
	// AggregatedSourceHash 是当前参与合并的源 skill（名称+内容）的 sha256 摘要，
	// 供 T-AGG-5 过期检测：源 skill 增删改后摘要变化 → 触发自动重建。普通 skill 无此值。
	AggregatedSourceHash string `json:"aggregated_source_hash" yaml:"aggregated_source_hash,omitempty"`
}

// SkillMeta 是去除了正文的 skill 元数据，用于列表展示。
type SkillMeta struct {
	Name         string         `json:"name"`        //name和description给llm其余的保留作为元数据
	Description  string         `json:"description"` //先写技能的简要介绍，再写何时invoke
	Category     string         `json:"category"`    // 分类，未来可基于不同分类组合实现 skill 矩阵
	Kind         string         `json:"kind"`
	Mode         string         `json:"mode"`
	UseWhen      []UseWhenEntry `json:"use_when,omitempty"` // 结构化「何时使用」意图线索（来自技能 frontmatter）
	Author       string         `json:"author"`
	SourceAuthor string         `json:"source_author"`
	Version      int            `json:"version"`
	Source       string         `json:"source"` // "builtin" | "user" | "novel"

	// —— 以下为 V3 跨书聚合派生字段（仅 Aggregated=true 时有意义）——
	Aggregated      bool     `json:"aggregated"`        // 是否为跨书聚合派生 skill
	AggregatedFrom  []string `json:"aggregated_from"`   // 参与合并的源 skill 名清单
	MergedBooks     []string `json:"merged_books"`      // 去重后的来源书名清单
	StableItems     int      `json:"stable_items"`      // 跨书复现（stable）条目数
	SingleBookItems int      `json:"single_book_items"` // 单书（single-book）条目数
}

// Meta 返回去除正文的元数据视图。
func (s *Skill) Meta(source string) SkillMeta {
	return SkillMeta{
		Name:         s.Name,
		Description:  s.Description,
		Category:     s.Category,
		Kind:         s.Kind,
		Mode:         s.Mode,
		UseWhen:      s.UseWhen,
		Author:       s.Author,
		SourceAuthor: s.SourceAuthor,
		Version:      s.Version,
		Source:       source,

		Aggregated:      s.Aggregated,
		AggregatedFrom:  s.AggregatedFrom,
		MergedBooks:     s.MergedBooks,
		StableItems:     s.StableItems,
		SingleBookItems: s.SingleBookItems,
	}
}
