package structure

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 用户书写不变量（pinned invariants，提案 §8.2/§9.2 吃书）的 kind 封闭枚举。
// 前两类规则可判（对台账比对出违例告警），custom 自由文本（供 LLM 消费，机器不判定）。
const (
	// KindAliveBefore 角色在某阅读序号前必须存活（"主角第 X 章前不死"；before<=0=全书不死）。
	KindAliveBefore = "character_alive_before"
	// KindCapabilityCeiling 角色战力天花板钉住（"力量体系上限 Y"，数值可比时判超限）。
	KindCapabilityCeiling = "capability_ceiling"
	// KindCustom 自由文本硬约束（机器不判，statement 供 LLM 消费）。
	KindCustom = "custom"
)

// ValidKind 判断不变量 kind 是否合法。
func ValidKind(kind string) bool {
	switch kind {
	case KindAliveBefore, KindCapabilityCeiling, KindCustom:
		return true
	}
	return false
}

// PinnedInvariant 是作者钉住的书写不变量（硬约束）：作者声明的"本书不许破"规则，
// 确定性检查器对台账比对产出违例告警（§8.2 组件 A 的用户书写不变量行）。
//
// 约定：
//   - (novel_id, kind, statement) 唯一——同声明重复 upsert 覆盖参数与确认标记；
//   - statement 是作者声明原文（人读 + LLM 消费），kind + params 是机器可判的结构化投影；
//   - Confirmed 是人工确认标记（true=作者显式确认，自动修改需更谨慎；机器不因
//     未确认而不检查，仅作留痕）；
//   - 属作者声明约束而非系列圣经事实，不进 snapshot 冻结范围。
type PinnedInvariant struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID    int64     `gorm:"column:novel_id;not null;uniqueIndex:uk_pinned_invariant,priority:1;index" json:"novel_id"`
	Kind       string    `gorm:"column:kind;not null;uniqueIndex:uk_pinned_invariant,priority:2" json:"kind"`
	Statement  string    `gorm:"column:statement;not null;uniqueIndex:uk_pinned_invariant,priority:3" json:"statement"`
	ParamsJSON string    `gorm:"column:params;type:text" json:"params"` // JSON 对象，kind 相关结构化参数
	Confirmed  bool      `gorm:"column:confirmed;not null;default:false" json:"confirmed"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (PinnedInvariant) TableName() string { return "pinned_invariants" }

// AliveBeforeParams 是 KindAliveBefore 的结构化参数。
type AliveBeforeParams struct {
	// Character 是角色名（与 characters.name 或其别名精确匹配）。
	Character string `json:"character"`
	// BeforeReadingNumber 是承诺存活到的阅读序号（第 X 章前不死）；<=0=全书不死。
	BeforeReadingNumber int `json:"before_reading_number"`
}

// CapabilityCeilingParams 是 KindCapabilityCeiling 的结构化参数。
type CapabilityCeilingParams struct {
	// Character 是角色名（与 characters.name 或其别名精确匹配）。
	Character string `json:"character"`
	// Ceiling 是钉住的战力天花板（数值文本可比时判超限；自由文本跳过不判）。
	Ceiling string `json:"ceiling"`
}

// ParseAliveBeforeParams 解析 KindAliveBefore 参数；坏 JSON/缺角色名 → ok=false（宁缺勿错）。
func ParseAliveBeforeParams(params string) (AliveBeforeParams, bool) {
	var p AliveBeforeParams
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return AliveBeforeParams{}, false
	}
	if strings.TrimSpace(p.Character) == "" {
		return AliveBeforeParams{}, false
	}
	p.Character = strings.TrimSpace(p.Character)
	return p, true
}

// ParseCapabilityCeilingParams 解析 KindCapabilityCeiling 参数；坏 JSON/缺角色名/缺上限 → ok=false。
func ParseCapabilityCeilingParams(params string) (CapabilityCeilingParams, bool) {
	var p CapabilityCeilingParams
	if err := json.Unmarshal([]byte(params), &p); err != nil {
		return CapabilityCeilingParams{}, false
	}
	if strings.TrimSpace(p.Character) == "" || strings.TrimSpace(p.Ceiling) == "" {
		return CapabilityCeilingParams{}, false
	}
	p.Character = strings.TrimSpace(p.Character)
	p.Ceiling = strings.TrimSpace(p.Ceiling)
	return p, true
}

// Validate 校验不变量契约：kind 合法、statement 非空、规则 kind 的 params 可判且必填项齐全。
// 缺失/坏参数逐项拒收——机器可验契约的拒收口径（upsert 前必过）。
func (p *PinnedInvariant) Validate() error {
	p.Statement = strings.TrimSpace(p.Statement)
	if p.Statement == "" {
		return fmt.Errorf("书写不变量缺声明原文（statement 必填）")
	}
	if !ValidKind(p.Kind) {
		return fmt.Errorf("书写不变量 kind 非法：%q（可选 %s/%s/%s）", p.Kind, KindAliveBefore, KindCapabilityCeiling, KindCustom)
	}
	params := strings.TrimSpace(p.ParamsJSON)
	if params == "" {
		params = "{}"
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(params), &obj); err != nil {
		return fmt.Errorf("书写不变量 params 不是合法 JSON 对象：%w", err)
	}
	p.ParamsJSON = params
	switch p.Kind {
	case KindAliveBefore:
		if _, ok := ParseAliveBeforeParams(params); !ok {
			return fmt.Errorf("书写不变量 %s 的 params 须含 character（角色名）与 before_reading_number（第几章前不死，<=0=全书）", KindAliveBefore)
		}
	case KindCapabilityCeiling:
		if _, ok := ParseCapabilityCeilingParams(params); !ok {
			return fmt.Errorf("书写不变量 %s 的 params 须含 character（角色名）与 ceiling（钉住上限）", KindCapabilityCeiling)
		}
	}
	return nil
}

// CharacterState 是不变量检查的角色台账投影（由 characters 表投影而来）。
type CharacterState struct {
	// ID 是角色记录 ID。
	ID int64
	// Name 是角色名。
	Name string
	// Aliases 是别名列表（JSON 数组解析而来；坏数据按无别名）。
	Aliases []string
	// Alive 是生死态三态：nil=未标注，true=在世，false=已死。
	Alive *bool
	// CapabilityCeiling 是账面战力天花板描述。
	CapabilityCeiling string
}
