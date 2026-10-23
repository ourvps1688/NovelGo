// Package voice 是 Voice 画像台账（voice_dimensions）的真理源：
// 十维枚举、落库模型与维度顺序单源。P1 WBS §0.1/§0.2 是实现唯一依据。
//
// 契约要点（勿改）：
//   - 维度枚举值一经定案禁改（diff/快照以枚举值为行键语义）；
//   - 缺失维度留空行（宁缺勿错），不造默认值；
//   - 不加 changed_at（风格修改不走 temporal/history 履历，快照 diff 覆盖变更可见性）；
//   - Voice 为书级，不挂 volume_id（风格非卷级事实）。
package voice

import (
	"encoding/json"
	"time"
)

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

// metrics JSON 键单源（WBS §0.2/T2.2 风险条）：确定性统计映射与后续渲染
// 都从此取键，禁止两处各写一份字符串。
const (
	MetricKeyAvgSentLen      = "avg_sent_len"
	MetricKeySentLenStdDev   = "sent_len_std_dev"
	MetricKeyShortSentPct    = "short_sent_pct"
	MetricKeyQuoteDensity    = "quote_density"
	MetricKeyCommaDensity    = "comma_density"
	MetricKeyPeriodDensity   = "period_density"
	MetricKeyExclaimDensity  = "exclaim_density"
	MetricKeyQuestionDensity = "question_density"
)

// ProfileItem 是提取 toolcall 输出契约中的结构化维度条目（WBS §0.3），
// pattern mental / style 双写共用此形（jsonschema 标签即工具定义来源）。
// 落库时 Metrics/Evidence 经 MetricsJSON/EvidenceJSON 序列化为 JSON 文本。
type ProfileItem struct {
	Dimension string             `json:"dimension" jsonschema:"required,description=Voice 画像维度枚举值，仅限：sentence_fingerprint/high_freq_words/taboo_words/rhythm/humor/certainty_tone/pov/tense/imagery_density/dialogue_ratio"`
	Content   string             `json:"content" jsonschema:"required,description=该维度的风格结论（描述性文字，中文）"`
	Metrics   map[string]float64 `json:"metrics,omitempty" jsonschema:"description=可选确定性数值指标（键如 avg_sent_len/quote_density）；无则省略"`
	Evidence  *ProfileEvidence   `json:"evidence,omitempty" jsonschema:"description=可选证据锚点；无则省略"`
}

// ProfileEvidence 是 ProfileItem 的证据锚点（§0.2 evidence JSON 的结构化来源）。
type ProfileEvidence struct {
	ChapterIDs []int64 `json:"chapter_ids,omitempty" jsonschema:"description=佐证本维度的章节 chapter_id 列表"`
	SampleIDs  []int64 `json:"sample_ids,omitempty" jsonschema:"description=佐证本维度的风格素材 sample_id 列表"`
	Note       string  `json:"note,omitempty" jsonschema:"description=补充说明"`
}

// MetricsJSON 序列化确定性指标为 JSON 文本；无指标返回空串（可空列）。
func (p ProfileItem) MetricsJSON() string {
	if len(p.Metrics) == 0 {
		return ""
	}
	b, err := json.Marshal(p.Metrics)
	if err != nil {
		return "" // 坏数据宁缺勿错
	}
	return string(b)
}

// EvidenceJSON 按「独立证据 <2 降级」纪律（WBS §0.3，B1 平移）序列化证据锚点：
//   - 锚点 ≥2：原样序列化；
//   - 锚点恰 1 个：锚点置空、仅留 note 标注（维度照常落库，不阻断）；
//   - 无锚点：返回空串。
func (p ProfileItem) EvidenceJSON() string {
	ev := p.Evidence
	if ev == nil {
		return ""
	}
	anchors := distinctInt64(ev.ChapterIDs) + distinctInt64(ev.SampleIDs)
	out := ProfileEvidence{Note: ev.Note}
	switch {
	case anchors >= 2:
		out.ChapterIDs = ev.ChapterIDs
		out.SampleIDs = ev.SampleIDs
	case anchors == 1:
		marker := "独立证据 <2，锚点已置空"
		if out.Note == "" {
			out.Note = marker
		} else {
			out.Note += "；" + marker
		}
	default:
		if out.Note == "" {
			return ""
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(b)
}

// distinctInt64 返回列表中互异值个数。
func distinctInt64(list []int64) int {
	seen := make(map[int64]bool, len(list))
	for _, v := range list {
		seen[v] = true
	}
	return len(seen)
}
