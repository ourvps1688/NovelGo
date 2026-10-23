// Package voice 是 Voice 画像台账（voice_dimensions）的真理源：
// 十维枚举、落库模型与维度顺序单源。P1 WBS §0.1/§0.2 是实现唯一依据。
//
// 契约要点（勿改）：
//   - 维度枚举值一经定案禁改（diff/快照以枚举值为行键语义）；
//   - 缺失维度留空行（宁缺勿错），不造默认值；
//   - 不加 changed_at（风格修改不走 temporal/history 履历，快照 diff 覆盖变更可见性）；
//   - Voice 为书级，不挂 volume_id（风格非卷级事实）。
package voice

import "time"

// Dimension 是 Voice 画像的维度枚举值（WBS §0.1，十值）。
type Dimension string

// 通用六维 + 小说四维（§0.1 唯一事实源，顺序即台账渲染固定顺序）。
const (
	DimSentenceFingerprint Dimension = "sentence_fingerprint" // 句式指纹（长短句偏好、疑问/陈述比例）
	DimHighFreqWords       Dimension = "high_freq_words"      // 高频词
	DimTabooWords          Dimension = "taboo_words"          // 禁忌词
	DimRhythm              Dimension = "rhythm"               // 节奏（先结论还是先铺垫、转折方式）
	DimHumor               Dimension = "humor"                // 幽默
	DimCertaintyTone       Dimension = "certainty_tone"       // 确定性语气
	DimPOV                 Dimension = "pov"                  // POV 与视角处理
	DimTense               Dimension = "tense"                // 时态
	DimImageryDensity      Dimension = "imagery_density"      // 意象密度
	DimDialogueRatio       Dimension = "dialogue_ratio"       // 对话-叙述比
)

// dimensions 按 §0.1 固定顺序保存十维，是渲染/遍历的唯一顺序源。
var dimensions = []Dimension{
	DimSentenceFingerprint,
	DimHighFreqWords,
	DimTabooWords,
	DimRhythm,
	DimHumor,
	DimCertaintyTone,
	DimPOV,
	DimTense,
	DimImageryDensity,
	DimDialogueRatio,
}

// Dimensions 返回 §0.1 固定顺序的十维清单副本（块内渲染按此顺序，单源）。
func Dimensions() []Dimension {
	out := make([]Dimension, len(dimensions))
	copy(out, dimensions)
	return out
}

// Valid 判断维度枚举值是否合法。
func (d Dimension) Valid() bool {
	for _, dim := range dimensions {
		if d == dim {
			return true
		}
	}
	return false
}

// Source 是结构化维度的落库来源枚举（WBS §0.2）。
type Source string

const (
	SourceStyleSample   Source = "style_sample"   // 风格素材提取（style 链路）
	SourcePatternMental Source = "pattern_mental" // 心智提取（kind: mental）
	SourceManual        Source = "manual"         // 人工录入
)

// Valid 判断来源枚举值是否合法。
func (s Source) Valid() bool {
	switch s {
	case SourceStyleSample, SourcePatternMental, SourceManual:
		return true
	}
	return false
}

// Entry 是一条 Voice 维度台账（WBS §0.2 存储契约）。
// (novel_id, dimension) 唯一，写入 Upsert 幂等（T1.2 落地）。
type Entry struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"                   json:"id"`
	NovelID   int64     `gorm:"column:novel_id;not null;uniqueIndex:uk_voice_dimension;index" json:"novel_id"`
	Dimension string    `gorm:"column:dimension;not null;uniqueIndex:uk_voice_dimension;size:32" json:"dimension"`
	Content   string    `gorm:"column:content;not null"                              json:"content"`
	Metrics   string    `gorm:"column:metrics"                                       json:"metrics"`  // JSON：确定性数值，可空
	Evidence  string    `gorm:"column:evidence"                                      json:"evidence"` // JSON：证据锚点，可空
	Source    string    `gorm:"column:source;not null;size:32"                       json:"source"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"                     json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"                     json:"updated_at"`
}

// TableName 返回表名。与 characters.voice_traits（角色声纹特征列）语义无关。
func (Entry) TableName() string { return "voice_dimensions" }
