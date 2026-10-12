package style

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// StringSlice 是 []string 的 DB 适配类型，存储为 JSON TEXT。
type StringSlice []string

// Scan 实现 sql.Scanner，从 DB 读取 JSON TEXT → []string。
func (s *StringSlice) Scan(value any) error {
	if value == nil {
		*s = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("StringSlice: cannot scan %T", value)
	}
	return json.Unmarshal(bytes, s)
}

// Value 实现 driver.Valuer，从 []string → JSON TEXT 写入 DB。
func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(s))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Sample 是一条风格素材的 GORM model。
type Sample struct {
	ID        int64       `gorm:"column:id;primaryKey;autoIncrement"    json:"id"`
	NovelID   int64       `gorm:"column:novel_id;not null"               json:"novel_id"`
	IsGlobal  bool        `gorm:"column:is_global;not null"              json:"is_global"`
	Name      string      `gorm:"column:name;not null"                   json:"name"`
	Content   string      `gorm:"column:content;not null"                json:"content"`
	Preview   string      `gorm:"column:preview;not null"                json:"preview"`
	Tags      StringSlice `gorm:"column:tags;type:text;not null"         json:"tags"`
	WordCount int         `gorm:"column:word_count;not null"             json:"word_count"`
	CreatedAt time.Time   `gorm:"column:created_at;autoCreateTime"       json:"created_at"`
	UpdatedAt time.Time   `gorm:"column:updated_at;autoUpdateTime"       json:"updated_at"`
}

func (Sample) TableName() string { return "style_samples" }

// Stats 是代码计算的确定性文本统计，不依赖 LLM。
type Stats struct {
	TotalChars      int     `json:"total_chars"`
	TotalWords      int     `json:"total_words"`
	SentenceCount   int     `json:"sentence_count"`
	ShortSentPct    float64 `json:"short_sent_pct"` // <15 字
	MidSentPct      float64 `json:"mid_sent_pct"`   // 15-30 字
	LongSentPct     float64 `json:"long_sent_pct"`  // >30 字
	AvgSentLen      float64 `json:"avg_sent_len"`
	SentLenStdDev   float64 `json:"sent_len_std_dev"` // 句长标准差
	CommaDensity    float64 `json:"comma_density"`    // 逗号占比
	PeriodDensity   float64 `json:"period_density"`   // 句号占比
	ExclaimDensity  float64 `json:"exclaim_density"`  // 感叹号占比
	QuestionDensity float64 `json:"question_density"` // 问号占比
	QuoteDensity    float64 `json:"quote_density"`    // 引号占比
	ParagraphCount  int     `json:"paragraph_count"`
	AvgParaLen      float64 `json:"avg_para_len"`
}

// ExtractResult 是风格提取的返回值。
// 改造后不再产出 skill 文件（旧的 frontmatter + ~/.novelgo/skills 路径已废弃），
// 而是返回一段可直接沉淀为「风格画像」的分析正文，由前端调用 CreateStyleProfile 入库。
type ExtractResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

// Profile 是一条「风格画像」的 GORM model。
// 与 style_samples（用户贴的原始素材）区分：Profile 是 LLM 从素材中提炼出的
// 结构化风格分析，作为本书「写作风格」偏好选择项的知识库（library）。
type Profile struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID     int64     `gorm:"column:novel_id;not null"            json:"novel_id"`
	IsGlobal    bool      `gorm:"column:is_global;not null"           json:"is_global"`
	Name        string    `gorm:"column:name;not null"                json:"name"`
	Description string    `gorm:"column:description;not null"         json:"description"`
	Content     string    `gorm:"column:content;not null"             json:"content"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"     json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"     json:"updated_at"`
}

func (Profile) TableName() string { return "style_profiles" }

// 写作风格偏好的固定分类名（与前端 STYLE_CONSTRAINT_CATEGORY 对齐）。
// 由 StyleConstraintPicker 选中后创建为本书偏好（content 首行带 marker）。
const StyleConstraintCategory = "写作风格"

// StyleMarkerPrefix 是「激活的写作风格偏好」content 首行 marker 的前缀，
// 完整形如 【写作风格·{profileID}】。用于识别与回链画像。
const StyleMarkerPrefix = "【写作风格·"

// StyleSkillOutput 是风格提取 toolcall 的结构化输出，由 LLM 调用 output_style_skill 工具填充。
type StyleSkillOutput struct {
	Name        string `json:"name" jsonschema:"required,description=风格名称（中文）"`
	Description string `json:"description" jsonschema:"required,description=一句话描述适合什么写作场景，什么时候调用"`
	Content     string `json:"content" jsonschema:"required,description=风格仿写技能正文 markdown，含 ## 风格概述/句式特征/用词习惯/修辞手法/节奏控制/叙事视角与距离/氛围与语调/仿写要点/原文锚点 等章节，不含 frontmatter"`
}
