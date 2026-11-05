package quality

// feedback.go —— 编辑反馈结构化回流（P1-1，升级计划 §P1）。
//
// 真实平台编辑反馈（签约驳回原话）是比内部判官更硬的现实信号，但它是非结构化文本。
// 本文件补上「非结构化文本 → 结构化规则」这一环：编辑原话经 editor-feedback 技能
// 拆解为回执（八维归类 + F 系原子 issue + 生成约束修订），按本契约落库；
// issue 由调用方投影为 pipeline 工单（走既有修复闭环），约束修订经
// apply_editor_feedback_constraints 人工显式应用（CheckerConfig / preference）。
//
// 契约要点（与 contract.go 的 L1 回执同纪律）：
//   - 回执 JSON 形、八维枚举、约束键枚举、格式串、解析在本文件唯一定义；
//     skill md（../skill/builtin/editor-feedback.md）输出格式节原样引用
//     FeedbackReceiptFormatLine()，契约测试强制同步；
//   - issue ID 用 F 系命名空间（F1/F2/…）：I 归常规体检、D 归蓝图漂移、T 归巡检、
//     V 归文风漂移、F 归编辑反馈回流（pipeline/contract.go 命名空间裁决），绝不混排；
//   - 宁缺勿错：文本没说的不编造——拿不准的维度/issue/约束宁可不报；
//   - 约束修订只「生成」不「应用」：落库待人工显式应用（零自动改配置）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// FeedbackSchemaVersion 是编辑反馈回执契约的版本号。每次给回执增删字段或变更语义时 +1。
const FeedbackSchemaVersion = 1

// 编辑反馈八维归类枚举（回执 dims / issue dim 取值单源；与 signability-audit 八维同域）。
const (
	FeedbackDimOpening    = "opening_hook"        // 开篇过签力（黄金三章/代入感）
	FeedbackDimGenre      = "genre_fit"           // 题材契合与差异化
	FeedbackDimCharacter  = "character_selling"   // 人设卖点
	FeedbackDimPacing     = "pacing_beats"        // 节奏与爽点密度
	FeedbackDimSetting    = "setting_coherence"   // 世界/设定自洽与卖点
	FeedbackDimLongline   = "longline_pull"       // 长线牵引（追读钩子）
	FeedbackDimCompliance = "platform_compliance" // 平台调性合规
	FeedbackDimReader     = "reader_view"         // 读者视角（已知/悬念/误解）
)

// FeedbackDimValid 判断八维归类枚举值是否合法。
func FeedbackDimValid(s string) bool {
	switch s {
	case FeedbackDimOpening, FeedbackDimGenre, FeedbackDimCharacter, FeedbackDimPacing,
		FeedbackDimSetting, FeedbackDimLongline, FeedbackDimCompliance, FeedbackDimReader:
		return true
	}
	return false
}

// 约束修订的目标域（回执 constraint target 取值单源）。
const (
	FeedbackTargetChecker    = "checker_config" // 落 novel 级检查器配置（CheckerConfig）
	FeedbackTargetPreference = "preference"     // 落创作偏好（preference，自由文本口径）
)

// checkerConfigConstraintKeys 是 checker_config 域可修订的参数键（与 CheckerConfig
// 工具参数同名，apply 时按键解析 Value）。命名空间裁决：这里只收机器可检参数。
const (
	ConstraintKeyBeatDensityMin   = "beat_density_min"
	ConstraintKeyTailHookRequired = "tail_hook_required"
	ConstraintKeyWindowN          = "window_n"
	ConstraintKeyTabooList        = "taboo_list"
	ConstraintKeyPremiseKeywords  = "premise_keywords"
	ConstraintKeyCheatKeywords    = "cheat_keywords"
	ConstraintKeyProtagonistNames = "protagonist_names"
)

// FeedbackConstraintValid 判断一条约束修订的 target/key 组合是否合法
// （preference 域 key 为自由分类标签，非空即可）。
func FeedbackConstraintValid(c FeedbackConstraint) bool {
	switch c.Target {
	case FeedbackTargetChecker:
		switch c.Key {
		case ConstraintKeyBeatDensityMin, ConstraintKeyTailHookRequired, ConstraintKeyWindowN,
			ConstraintKeyTabooList, ConstraintKeyPremiseKeywords, ConstraintKeyCheatKeywords,
			ConstraintKeyProtagonistNames:
			return strings.TrimSpace(c.Value) != ""
		}
		return false
	case FeedbackTargetPreference:
		return strings.TrimSpace(c.Key) != "" && strings.TrimSpace(c.Value) != ""
	}
	return false
}

// FeedbackIssue 是编辑反馈拆解出的一条原子问题（F 系，只报告不改写）。
type FeedbackIssue struct {
	// ID 是 F 系稳定键（F1/F2/…）；空缺由投影层补号。
	ID string `json:"id"`
	// Dim 是八维归类（FeedbackDim* 枚举）。
	Dim string `json:"dim"`
	// Level 是严重级：P0（阻断签约）/ P1（提升竞争力）。
	Level string `json:"level"`
	// Area 是展示标签（与体检 issue area 同风格，如「开篇过签力」）。
	Area string `json:"area"`
	// Desc 是问题描述（编辑原话的结构化转写，不编造原文没有的判断）。
	Desc string `json:"desc"`
	// FixType 是修复出口：body（正文级）/ ledger（台账/约束级）；空=ledger。
	FixType string `json:"fix_type,omitempty"`
	// ChapterIDs 是涉及章节全局阅读序号（编辑说「开篇」→ [1,2,3]；全书级 → []）。
	ChapterIDs []int64 `json:"chapter_ids,omitempty"`
}

// FeedbackConstraint 是一条生成约束修订（不自动应用，待人工显式 apply）。
type FeedbackConstraint struct {
	// Target 是目标域（FeedbackTarget* 枚举）。
	Target string `json:"target"`
	// Key 是目标参数键（checker_config 域=ConstraintKey* 枚举；preference 域=分类标签）。
	Key string `json:"key"`
	// Value 是建议值（checker_config 域按参数语义序列化：数字/布尔/JSON 数组）。
	Value string `json:"value"`
	// Reason 是修订依据（引编辑原话，供人工裁决）。
	Reason string `json:"reason"`
}

// FeedbackReceipt 是 editor-feedback 技能的回执契约形。
type FeedbackReceipt struct {
	// Dims 是本次反馈命中的八维归类（FeedbackDim* 枚举，去重）。
	Dims []string `json:"dims"`
	// Issues 是 F 系原子问题清单（宁缺勿错：文本没说的不编造）。
	Issues []FeedbackIssue `json:"issues"`
	// Constraints 是生成约束修订（只生成不应用）。
	Constraints []FeedbackConstraint `json:"constraints"`
	// Summary 是一句话小结。
	Summary string `json:"summary"`
}

// FeedbackReceiptFormatLine 生成回执 JSON 格式串（skill md 输出格式节原样引用，契约测试锁定）。
func FeedbackReceiptFormatLine() string {
	return `{"dims":["opening_hook"],"issues":[{"id":"F1","dim":"opening_hook","level":"P0|P1","area":"维度","desc":"...","fix_type":"body|ledger","chapter_ids":[1,2,3]}],"constraints":[{"target":"checker_config|preference","key":"...","value":"...","reason":"..."}],"summary":"一句话小结"}`
}

// feedbackJSONBlockRe 匹配 ```json 或裸 ``` 围栏内的 JSON 块（回执在最终回复末尾）。
var feedbackJSONBlockRe = regexp.MustCompile("(?s)```(?:json)?[ \t]*\n?(\\{.*?\\})\n?```")

// ParseFeedbackReceipt 从 LLM 输出提取末尾 ```json 回执块并解析（多个块时取最后一个
// 可解析的）。降级纪律：找不到回执块 / 解析失败 → 返回 error，由调用方降级。
func ParseFeedbackReceipt(out string) (*FeedbackReceipt, error) {
	matches := feedbackJSONBlockRe.FindAllStringSubmatch(out, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		var r FeedbackReceipt
		if err := json.Unmarshal([]byte(matches[i][1]), &r); err != nil {
			continue
		}
		r.normalize()
		return &r, nil
	}
	return nil, errors.New("quality feedback: 未找到可解析的编辑反馈回执 JSON 块")
}

// normalize 归一化回执：ID 空缺按序补 F{n}；dim/target/key 非法的 issue/约束直接剔除
// （宁缺勿错：坏分类不落库误导后续）；fix_type 空归 ledger。
func (r *FeedbackReceipt) normalize() {
	dims := make([]string, 0, len(r.Dims))
	seen := map[string]bool{}
	for _, d := range r.Dims {
		if FeedbackDimValid(d) && !seen[d] {
			seen[d] = true
			dims = append(dims, d)
		}
	}
	r.Dims = dims
	issues := make([]FeedbackIssue, 0, len(r.Issues))
	for i, f := range r.Issues {
		if !FeedbackDimValid(f.Dim) || strings.TrimSpace(f.Desc) == "" {
			continue
		}
		if strings.TrimSpace(f.ID) == "" {
			f.ID = fmt.Sprintf("F%d", i+1)
		}
		if f.FixType == "" {
			f.FixType = "ledger"
		}
		issues = append(issues, f)
	}
	r.Issues = issues
	cons := make([]FeedbackConstraint, 0, len(r.Constraints))
	for _, c := range r.Constraints {
		if FeedbackConstraintValid(c) {
			cons = append(cons, c)
		}
	}
	r.Constraints = cons
}

// ParseStatus 是反馈样本的拆解状态。
const (
	FeedbackParseRaw    = "raw"    // 仅存原话（拆解失败/未拆解）
	FeedbackParseParsed = "parsed" // 拆解成功（dims/issues/constraints 为准）
)

// EditorFeedback 是一条编辑反馈样本（P1-1 反馈回流的持久化事实源）。
// novel_id=0 是平台级样本（作品未入库，如已发布的旧作），只沉淀口径不落工单；
// novel_id>0 时 F 系 issue 投影为 pipeline 工单（run_id=反馈容器）走既有修复闭环。
type EditorFeedback struct {
	ID        int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID   int64  `gorm:"column:novel_id;not null;index" json:"novel_id"`
	RunID     string `gorm:"column:run_id" json:"run_id,omitempty"`
	Platform  string `gorm:"column:platform" json:"platform"`
	WorkTitle string `gorm:"column:work_title" json:"work_title"`
	Channel   string `gorm:"column:channel" json:"channel"`
	// Verdict 是编辑原话（非结构化输入的原文留痕，任何拆解产物不得歪曲它）。
	Verdict string `gorm:"column:verdict;type:text" json:"verdict"`
	// ParseStatus 是拆解状态（FeedbackParse*）。
	ParseStatus string `gorm:"column:parse_status;not null" json:"parse_status"`
	// Dims 是 JSON：命中八维（[]string，FeedbackDim*）。
	Dims string `gorm:"column:dims;type:text" json:"dims"`
	// Issues 是 JSON：F 系原子问题（[]FeedbackIssue）。
	Issues string `gorm:"column:issues;type:text" json:"issues"`
	// Constraints 是 JSON：生成约束修订（[]FeedbackConstraint，未应用前仅是建议）。
	Constraints string `gorm:"column:constraints;type:text" json:"constraints"`
	// ConstraintsAppliedAt 是约束修订的应用时间（nil=未应用；apply 工具显式落）。
	ConstraintsAppliedAt *time.Time `gorm:"column:constraints_applied_at" json:"constraints_applied_at,omitempty"`
	// Summary 是一句话小结。
	Summary   string    `gorm:"column:summary;type:text" json:"summary"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (EditorFeedback) TableName() string { return "editor_feedbacks" }

// FeedbackDims 解析命中八维（坏数据按空，宁缺勿错）。
func (f *EditorFeedback) FeedbackDims() []string { return parseStringList(f.Dims) }

// FeedbackIssues 解析 F 系问题清单（坏数据按空，宁缺勿错）。
func (f *EditorFeedback) FeedbackIssues() []FeedbackIssue {
	if strings.TrimSpace(f.Issues) == "" {
		return nil
	}
	var list []FeedbackIssue
	if err := json.Unmarshal([]byte(f.Issues), &list); err != nil {
		return nil
	}
	return list
}

// FeedbackConstraints 解析约束修订清单（坏数据按空，宁缺勿错）。
func (f *EditorFeedback) FeedbackConstraints() []FeedbackConstraint {
	if strings.TrimSpace(f.Constraints) == "" {
		return nil
	}
	var list []FeedbackConstraint
	if err := json.Unmarshal([]byte(f.Constraints), &list); err != nil {
		return nil
	}
	return list
}

// CreateEditorFeedback 落一条反馈样本（先存原话再拆解的两段式第一步）。
func (s *Store) CreateEditorFeedback(ctx context.Context, row *EditorFeedback) error {
	return s.DB.WithContext(ctx).Create(row).Error
}

// UpdateEditorFeedbackReceipt 拆解成功后回填结构化产物（两段式第二步）。
func (s *Store) UpdateEditorFeedbackReceipt(ctx context.Context, id int64, r *FeedbackReceipt, runID string) error {
	dims, _ := json.Marshal(r.Dims)
	issues, _ := json.Marshal(r.Issues)
	cons, _ := json.Marshal(r.Constraints)
	updates := map[string]any{
		"parse_status": FeedbackParseParsed,
		"dims":         string(dims),
		"issues":       string(issues),
		"constraints":  string(cons),
		"summary":      r.Summary,
		"updated_at":   time.Now(),
	}
	if runID != "" {
		updates["run_id"] = runID
	}
	return s.DB.WithContext(ctx).Model(&EditorFeedback{}).
		Where("id = ?", id).Updates(updates).Error
}

// GetEditorFeedback 按 ID 读一条反馈样本。
func (s *Store) GetEditorFeedback(ctx context.Context, id int64) (*EditorFeedback, error) {
	var row EditorFeedback
	if err := s.DB.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListEditorFeedbacks 列反馈样本：novelID>0 该书 + 平台级（novel_id=0）样本一并返回；
// novelID=0 只返回平台级样本。新样本在前。
func (s *Store) ListEditorFeedbacks(ctx context.Context, novelID int64) ([]EditorFeedback, error) {
	q := s.DB.WithContext(ctx).Model(&EditorFeedback{})
	if novelID > 0 {
		q = q.Where("novel_id IN (?, 0)", novelID)
	} else {
		q = q.Where("novel_id = 0")
	}
	var rows []EditorFeedback
	if err := q.Order("id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// MarkFeedbackConstraintsApplied 标记约束修订已应用（幂等：重复标记覆盖时间戳）。
func (s *Store) MarkFeedbackConstraintsApplied(ctx context.Context, id int64) error {
	now := time.Now()
	return s.DB.WithContext(ctx).Model(&EditorFeedback{}).
		Where("id = ?", id).Updates(map[string]any{"constraints_applied_at": now, "updated_at": now}).Error
}
