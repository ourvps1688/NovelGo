package character

import "time"

// RelationKind 是关系类型的软枚举（WBS §0.4 R1 冻结）：relation_describe 自由文本仍为主，
// relation_kind 可空、仅在能归类时填，供一致性判定（如「仇敌不得无故变恋人」）。
const (
	RelationKindMentor = "mentor"
	RelationKindRival  = "rival"
	RelationKindLover  = "lover"
	RelationKindKin    = "kin"
	RelationKindOther  = "other"
)

// Character 是角色元数据。
// personality 和 abilities 为 JSON 自由格式，由 LLM 填写和读取，不做结构化约束。
//
// 阶段 2（WBS §0.4 冻结清单）新增的四维判定字段（D1 结构化列）：
// identity（gender/birth_year/appearance_anchors）、status（alive/location_ref/faction/role）、
// capability（capability_ceiling/sealed，known_skills 复用 abilities）、voice_traits；
// 及 series bible 通用归属 volume_id 与 temporal 变革定位（changed_at/changed_in_*）。
type Character struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID     int64     `gorm:"column:novel_id;not null;index"    json:"novel_id"`
	Name        string    `gorm:"column:name;not null;index"        json:"name"`
	Description string    `gorm:"column:description"                json:"description"` // 自然语言描述，LLM 填写
	Personality string    `gorm:"column:personality"                json:"personality"` // JSON 自由格式，LLM 填写。如 {"traits":["勇敢","冲动"],"brief":"一个热血青年"}
	Abilities   string    `gorm:"column:abilities"                  json:"abilities"`   // JSON 数组，如 ["剑术","隐身"]（= capability.known_skills）
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"  json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"  json:"updated_at"`

	// ── identity（§0.2 角色维）────────────────────────────────────────
	Aliases           string `gorm:"column:aliases"             json:"aliases"`           // JSON 数组，如 ["小七","七爷"]
	Gender            string `gorm:"column:gender"              json:"gender"`            // 自由文本
	BirthYear         string `gorm:"column:birth_year"          json:"birth_year"`        // 出生年/年龄，虚构历法用字符串承载（如「新历312年」「24岁」）
	AppearanceAnchors string `gorm:"column:appearance_anchors" json:"appearance_anchors"` // JSON 数组，外貌锚点（如「左手刀疤」「左撇子」）

	// ── status（§0.2 角色维）──────────────────────────────────────────
	// Alive 生死态：指针三态——nil=未标注（迁移存量/新建缺省），true=在世，false=已死。
	// 「已死不复活」一致性规则靠它机器校验；不用 bool 是避免 GORM 零值跳写把 false 写成默认 true。
	Alive       *bool  `gorm:"column:alive;not null;default:true" json:"alive,omitempty"`
	LocationRef string `gorm:"column:location_ref"               json:"location_ref"` // 当前所在地点（引用 locations.name 或自由文本）
	Faction     string `gorm:"column:faction"                    json:"faction"`      // 阵营
	Role        string `gorm:"column:role"                       json:"role"`         // 剧情角色（如 主角/反派/导师）

	// ── capability（§0.2 角色维，防后期偷偷加挂）───────────────────────
	CapabilityCeiling string `gorm:"column:capability_ceiling" json:"capability_ceiling"` // 战力天花板描述
	Sealed            bool   `gorm:"column:sealed;not null;default:false" json:"sealed"`  // 封禁/封印态，sealed=true 时禁止新增能力

	// ── voice_traits（软约束，供一致性检查）────────────────────────────
	VoiceTraits string `gorm:"column:voice_traits" json:"voice_traits"` // JSON 数组，语言风格特征

	// ── series bible 通用（volume_id 归属 + temporal 变革定位）──────────
	// VolumeID 归属卷：NULL=未归属/全书级（存量迁移前为空，见 2.8 backfill）。
	VolumeID           *int64     `gorm:"column:volume_id;index"       json:"volume_id,omitempty"`
	ChangedAt          *time.Time `gorm:"column:changed_at"            json:"changed_at,omitempty"` // 语义最后一次实质变革时刻（temporal.changed_at）
	ChangedInVolumeID  *int64     `gorm:"column:changed_in_volume_id"  json:"changed_in_volume_id,omitempty"`
	ChangedInChapterID *int64     `gorm:"column:changed_in_chapter_id" json:"changed_in_chapter_id,omitempty"`
}

// TableName 指定 GORM 表名。
func (Character) TableName() string { return "characters" }

// CharacterRelation 是角色之间有向关系的当前快照。
//
// 设计原则（与 Python 版本的关键差异）：
//  1. 自由文本 relation_describe — LLM 用自然语言描述关系（如 "朋友、高中同学、暗中嫉妒"），
//     不再受枚举 18 种类型的限制。单行可表达多面关系。
//  2. 追加不可变 (append-only) — 关系变更时不修改旧行，而是 INSERT 新行并将旧行 is_current 置 false。
//     同一对 (source_id, target_id) 的全部记录按 created_at 排序即为完整演变历史。
//  3. 不需要 evolved_from_id 指针链 — 用 (source,target) 配对 + 时间戳隐式排序替代显式链表，
//     消除断链风险和并发分叉问题。Python 因为枚举 type 导致一对角色需要多行并行记录，
//     每行各自演化，才被迫引入自引用链；自由文本 relation_describe 从根上消除了这个需求。
//  4. 不需要 status 字段 — is_current 布尔替代 active/dormant/resolved/severed 四状态枚举，
//     关系状态只需区分"当前"和"历史"。
//
// 图查询支持：
//   - 全图：WHERE novel_id = ? AND is_current = true  → 所有节点+边，前端可渲染关系图
//   - 单角色邻域：WHERE (source_id = ? OR target_id = ?) AND is_current = true
//   - 两角色之间：WHERE (source_id = A AND target_id = B) OR (source_id = B AND target_id = A)
//
// MCP 工具实现参考：
//   - update_character_relationship: 变更时开启事务，旧行 SET is_current = false，INSERT 新行，COMMIT
//   - get_character_network: 查当前全图，返回 nodes(Character 列表) + edges(CharacterRelation 列表) 可以加入ids参数只查涉及到的人的关系网
//   - get_character_history: WHERE source_id = X AND target_id = Y ORDER BY created_at，
//     可选通过 chapter_number JOIN chapters 拉取对应章节摘要补充上下文
//
// TODO
// 后续可实现前端的可视化关系图绘制
type CharacterRelation struct {
	ID                int64  `gorm:"column:id;primaryKey;autoIncrement"           json:"id"`
	NovelID           int64  `gorm:"column:novel_id;not null;index"              json:"novel_id"`
	SourceCharacterID int64  `gorm:"column:source_character_id;not null;index"   json:"source_character_id"` // 关系发出方
	TargetCharacterID int64  `gorm:"column:target_character_id;not null;index"   json:"target_character_id"` // 关系接收方
	RelationDescribe  string `gorm:"column:relation_describe;not null"          json:"relation_describe"`    // 自由文本，LLM 自行描述，如 "亦师亦敌"、"朋友、高中同学"，工具描述的时候需要告诉llm详细描述而不是简单的type
	// RelationKind 软枚举（WBS §0.4 R1）：mentor|rival|lover|kin|other，可空；
	// relation_describe 仍为主，本字段仅在能归类时填，供一致性判定。
	RelationKind string    `gorm:"column:relation_kind"                        json:"relation_kind"`
	Description  string    `gorm:"column:description"                          json:"description"` // 当前关系阶段的详细描述
	ChapterID    *int64    `gorm:"column:chapter_id;index"                     json:"chapter_id"`  // 此关系在哪个章节确立/变更；nil 表示未关联具体章节
	IsCurrent    bool      `gorm:"column:is_current;not null;index"            json:"is_current"`  // true=当前有效关系，false=历史记录
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"            json:"created_at"`
	// VolumeID 归属卷（NULL=未归属/全书级）+ temporal 变革定位（§0.4 清单）。
	VolumeID  *int64     `gorm:"column:volume_id;index" json:"volume_id,omitempty"`
	ChangedAt *time.Time `gorm:"column:changed_at"      json:"changed_at,omitempty"`
}

// TableName 指定 GORM 表名。
func (CharacterRelation) TableName() string { return "character_relations" }

// CharacterFactKnowledge 是读者视角维的 knowledge_boundaries 结构化落点
// （WBS §0.4 R3 冻结：粒度 = per (char_id, fact_id) + as_of_chapter_id）：
// 「哪位 POV 角色自某章起知道/不知道某条 reader_fact」。fact_id 引用 reader_perspectives.id。
//
// 同一 (char_id, fact_id) 允许多行（不同 as_of_chapter 是认知变化的时间序列），
// 按 as_of_chapter_id 排序即为该角色对某事实的认知演变。
type CharacterFactKnowledge struct {
	ID      int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID int64 `gorm:"column:novel_id;not null;index"    json:"novel_id"`
	CharID  int64 `gorm:"column:char_id;not null;index"     json:"char_id"` // characters.id（POV 角色）
	FactID  int64 `gorm:"column:fact_id;not null;index"     json:"fact_id"` // reader_perspectives.id
	Knows   bool  `gorm:"column:knows;not null;default:false" json:"knows"` // true=自 as_of_chapter 起知道，false=不知道
	// AsOfChapterID 生效起点章节（chapters.id）；nil=未定位（存量迁移前/无从判定）。
	AsOfChapterID *int64    `gorm:"column:as_of_chapter_id;index" json:"as_of_chapter_id,omitempty"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	// temporal 变革定位（§0.3）：changed_at=语义最后一次实质变更；changed_in_* 定位到卷/章。
	ChangedAt          *time.Time `gorm:"column:changed_at"            json:"changed_at,omitempty"`
	ChangedInVolumeID  *int64     `gorm:"column:changed_in_volume_id"  json:"changed_in_volume_id,omitempty"`
	ChangedInChapterID *int64     `gorm:"column:changed_in_chapter_id" json:"changed_in_chapter_id,omitempty"`
}

// TableName 指定 GORM 表名。
func (CharacterFactKnowledge) TableName() string { return "character_fact_knowledge" }
