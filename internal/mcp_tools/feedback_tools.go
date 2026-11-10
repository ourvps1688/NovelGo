package mcp_tools

// feedback_tools.go —— 编辑反馈结构化回流工具（P1-1，quality/feedback.go 契约消费侧）。
//
// 三个工具的分工（零自动改配置纪律）：
//   - record_editor_feedback：录入编辑原话（两段式：先存原话再拆解），拆解产物沉淀为
//     反馈样本；novel_id>0 时 F 系 issue 投影为 pipeline 工单（feedback 容器）走既有
//     修复闭环（工单面板/去修复/复验）；
//   - get_editor_feedbacks：列反馈样本（该书 + 平台级样本）；
//   - apply_editor_feedback_constraints：**人工显式**应用约束修订（CheckerConfig /
//     preference），已应用幂等拒绝——约束只建议，绝不自动应用。
//
// 拆解派发给 editorfeedback 子 Agent（零工具纯推理），方法论由 editor-feedback 技能
// 注入（单源，buildFeedbackInstruction 同 chapter-ci 纪律）；派发失败/回执解析失败
// 保留 raw 原话行（两段式降级，宁缺勿错——绝不拿未解析的半成品冒充结构化产物）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/skill"
)

// FeedbackTicketLanderFunc 把 F 系反馈 issue 落成 pipeline 工单，返回（runID, 落库条数）。
// 由 app 启动时注入（pipeline.LandFeedbackTickets）——mcp_tools 不能反向 import pipeline
// （pipeline→agent→mcp_tools 会成环），与 RunSubAgentFunc 同一避免循环依赖的纪律。
type FeedbackTicketLanderFunc func(ctx context.Context, db *gorm.DB, novelID int64, issues []quality.FeedbackIssue) (string, int, error)

// feedbackTicketLander 是落单回调（nil=未装配，降级只沉淀样本不落工单）。
var feedbackTicketLander FeedbackTicketLanderFunc

// SetFeedbackTicketLander 装配落单回调（app 启动时注入 pipeline.LandFeedbackTickets）。
func SetFeedbackTicketLander(fn FeedbackTicketLanderFunc) { feedbackTicketLander = fn }

// ── record_editor_feedback ─────────────────────────────

// RecordEditorFeedbackArgs 是 record_editor_feedback 的参数。
type RecordEditorFeedbackArgs struct {
	// NovelID 是关联小说 ID；不传=当前小说；显式传 0=平台级样本（作品未入库，
	// 只沉淀口径不落工单，如已发布的旧作）。
	NovelID   *int64 `json:"novel_id,omitempty" jsonschema:"description=关联小说ID；不传=当前小说；传0=平台级样本（作品未入库，只沉淀口径不落工单）"`
	WorkTitle string `json:"work_title" jsonschema:"required,description=作品书名" validate:"required"`
	Platform  string `json:"platform" jsonschema:"required,description=反馈平台（如 七猫）" validate:"required"`
	Channel   string `json:"channel,omitempty" jsonschema:"description=频道（如 男频悬疑）"`
	Verdict   string `json:"verdict" jsonschema:"required,description=编辑原话（完整粘贴，拆解唯一依据，不得改写）" validate:"required"`
}

// RecordEditorFeedbackTool 录入并拆解一条平台编辑反馈（P1-1 反馈回流入口）。
type RecordEditorFeedbackTool struct{}

func (t *RecordEditorFeedbackTool) Name() string { return "record_editor_feedback" }
func (t *RecordEditorFeedbackTool) Description() string {
	return "录入平台编辑的审稿/驳回原话并自动结构化拆解（八维归类+F系原子问题+生成约束修订建议）。" +
		"两段式落库：原话先存（任何拆解产物不得歪曲它），拆解成功再回填结构化产物。" +
		"拆解出的 F 系问题在关联小说（novel_id>0）时落成可修复工单，走既有去修复/复验闭环；" +
		"约束修订只建议不应用（用 apply_editor_feedback_constraints 人工显式应用）。" +
		"作品未入库传 novel_id=0 记平台级样本（只沉淀口径不落工单）。"
}
func (t *RecordEditorFeedbackTool) Category() ToolCategory { return CategoryConsistencyCheck }

func (t *RecordEditorFeedbackTool) JSONSchema() json.RawMessage {
	return SchemaOf(RecordEditorFeedbackArgs{})
}
func (t *RecordEditorFeedbackTool) ExposeToLLM() bool { return true }
func (t *RecordEditorFeedbackTool) NewArgs() any      { return &RecordEditorFeedbackArgs{} }

func (t *RecordEditorFeedbackTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*RecordEditorFeedbackArgs)
	novelID := tc.NovelID
	if a.NovelID != nil {
		novelID = *a.NovelID
	}
	store := quality.NewStore(tc.DB, tc.LoggerOrDefault())

	// 两段式第一步：先存原话（parse_status=raw）。后续任何失败都不删行——原话留痕优先。
	row := &quality.EditorFeedback{
		NovelID:     novelID,
		Platform:    strings.TrimSpace(a.Platform),
		WorkTitle:   strings.TrimSpace(a.WorkTitle),
		Channel:     strings.TrimSpace(a.Channel),
		Verdict:     strings.TrimSpace(a.Verdict),
		ParseStatus: quality.FeedbackParseRaw,
	}
	if err := store.CreateEditorFeedback(ctx, row); err != nil {
		return nil, fmt.Errorf("record editor feedback: 落样本失败: %w", err)
	}
	degraded := func(reason string) *ToolResult {
		return &ToolResult{
			Success: true,
			Data: map[string]any{
				"feedback_id":  row.ID,
				"parse_status": quality.FeedbackParseRaw,
				"degraded":     reason,
			},
		}
	}

	// 拆解派发：editorfeedback 子 Agent（零工具纯推理，方法论由技能注入）。
	if tc.RunSubAgent == nil {
		return degraded("子 Agent 能力不可用，仅存原话（拆解未执行）"), nil
	}
	instruction := buildFeedbackInstruction(tc.SkillStore, row)
	report, err := tc.RunSubAgent(ctx, SubAgentRequest{
		AgentType:   "editorfeedback",
		NovelID:     novelID,
		Instruction: instruction,
		ToolID:      tc.ToolID,
	})
	if err != nil {
		tc.LoggerOrDefault().Warn("编辑反馈拆解派发失败（保留原话降级）", "err", err, "feedbackID", row.ID)
		return degraded("拆解派发失败，仅存原话"), nil
	}
	receipt, perr := quality.ParseFeedbackReceipt(report)
	if perr != nil {
		tc.LoggerOrDefault().Warn("编辑反馈回执解析失败（保留原话降级）", "err", perr, "feedbackID", row.ID)
		return degraded("回执解析失败，仅存原话"), nil
	}

	// F 系 issue 投影为 pipeline 工单（novel_id>0 才落；平台级样本只沉淀口径）。
	// 落单回调未装配（测试/app 未注入）同样降级只沉淀样本。
	runID := ""
	ticketsLanded := 0
	if feedbackTicketLander != nil && novelID > 0 && len(receipt.Issues) > 0 {
		rid, landed, lerr := feedbackTicketLander(ctx, tc.DB, novelID, receipt.Issues)
		if lerr != nil {
			tc.LoggerOrDefault().Warn("编辑反馈工单落库失败（样本照常沉淀）", "err", lerr, "feedbackID", row.ID)
		} else {
			runID, ticketsLanded = rid, landed
		}
	}

	// 两段式第二步：回填结构化产物。
	if uerr := store.UpdateEditorFeedbackReceipt(ctx, row.ID, receipt, runID); uerr != nil {
		return nil, fmt.Errorf("record editor feedback: 回填拆解产物失败: %w", uerr)
	}
	return &ToolResult{
		Success: true,
		Data: map[string]any{
			"feedback_id":      row.ID,
			"parse_status":     quality.FeedbackParseParsed,
			"dims":             receipt.Dims,
			"issue_count":      len(receipt.Issues),
			"constraint_count": len(receipt.Constraints),
			"run_id":           runID,
			"tickets_landed":   ticketsLanded,
			"summary":          receipt.Summary,
		},
	}, nil
}

// buildFeedbackInstruction 构造编辑反馈拆解指令：原话内联 + editor-feedback 技能方法论
// （单源）+ 回执契约格式串（与 buildChapterCIInstruction 同纪律）。
func buildFeedbackInstruction(skillStore *skill.Store, row *quality.EditorFeedback) string {
	var b strings.Builder
	fmt.Fprintf(&b, "请对以下平台编辑反馈做结构化拆解（feedback_id=%d）。\n", row.ID)
	b.WriteString("作品：" + row.WorkTitle + "；平台：" + row.Platform)
	if row.Channel != "" {
		b.WriteString("；频道：" + row.Channel)
	}
	b.WriteString("\n编辑原话（唯一依据，绝不改写/补充编辑没说的判断）：\n")
	b.WriteString(row.Verdict)
	b.WriteString("\n")
	if skillStore != nil {
		if sk, ok := skillStore.Get(0, "editor-feedback"); ok && strings.TrimSpace(sk.Content) != "" {
			b.WriteString("\n# 本次任务方法论\n")
			b.WriteString(sk.Content)
			b.WriteString("\n")
			// #111 P2 统一账本：editor-feedback 走 RunSubAgent，不进 exec_run_stats；
			// 与 chapter-ci 同纪律，在技能正文注入点直接记账。
			activity.RecordSkillInvocation(context.Background(), activity.SkillInvocation{
				Skill: "editor-feedback", Channel: "feedback", Mount: "feedback",
				NovelID: row.NovelID, Runes: len([]rune(sk.Content)),
			})
		}
	}
	b.WriteString("\n最终回复末尾必须附 ```json 代码块上报回执，格式：\n")
	b.WriteString(quality.FeedbackReceiptFormatLine())
	b.WriteString("\n")
	return b.String()
}

// ── get_editor_feedbacks ───────────────────────────────

// GetEditorFeedbacksArgs 是 get_editor_feedbacks 的参数。
type GetEditorFeedbacksArgs struct {
	// NovelID 不传=当前小说（该书 + 平台级样本）；传 0=只看平台级样本。
	NovelID *int64 `json:"novel_id,omitempty" jsonschema:"description=小说ID；不传=当前小说（返回该书+平台级样本）；传0=只看平台级样本"`
}

// GetEditorFeedbacksTool 列编辑反馈样本（P1-1 反馈回流查看入口）。
type GetEditorFeedbacksTool struct{}

func (t *GetEditorFeedbacksTool) Name() string { return "get_editor_feedbacks" }
func (t *GetEditorFeedbacksTool) Description() string {
	return "列出编辑反馈样本（新样本在前）：关联小说的样本 + 平台级样本（novel_id=0，作品未入库的旧作口径）。" +
		"返回每条的原话、拆解状态、八维归类、F 系问题、约束修订建议与应用状态。"
}
func (t *GetEditorFeedbacksTool) Category() ToolCategory { return CategoryConsistencyCheck }
func (t *GetEditorFeedbacksTool) JSONSchema() json.RawMessage {
	return SchemaOf(GetEditorFeedbacksArgs{})
}
func (t *GetEditorFeedbacksTool) ExposeToLLM() bool { return true }
func (t *GetEditorFeedbacksTool) NewArgs() any      { return &GetEditorFeedbacksArgs{} }

func (t *GetEditorFeedbacksTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*GetEditorFeedbacksArgs)
	novelID := tc.NovelID
	if a.NovelID != nil {
		novelID = *a.NovelID
	}
	rows, err := quality.NewStore(tc.DB, tc.LoggerOrDefault()).ListEditorFeedbacks(ctx, novelID)
	if err != nil {
		return nil, fmt.Errorf("get editor feedbacks: %w", err)
	}
	return &ToolResult{
		Success: true,
		Data:    map[string]any{"feedbacks": rows, "count": len(rows)},
	}, nil
}

// ── apply_editor_feedback_constraints ──────────────────

// ApplyFeedbackConstraintsArgs 是 apply_editor_feedback_constraints 的参数。
type ApplyFeedbackConstraintsArgs struct {
	FeedbackID int64 `json:"feedback_id" jsonschema:"required,description=反馈样本ID（record_editor_feedback 返回）" validate:"required"`
	// NovelID 是约束应用目标小说 ID：平台级样本（novel_id=0）应用 checker_config
	// 约束时必传；不传=用样本自身的 novel_id。preference 约束在目标为 0 时落全局偏好。
	NovelID *int64 `json:"novel_id,omitempty" jsonschema:"description=约束应用目标小说ID；平台级样本（novel_id=0）应用 checker_config 约束时必传；不传=用样本自身的novel_id"`
}

// ApplyFeedbackConstraintsTool 人工显式应用一条反馈样本的约束修订（P1-1 收口：
// 零自动改配置——约束只建议，应用必须作者显式触发；已应用幂等拒绝）。
type ApplyFeedbackConstraintsTool struct{}

func (t *ApplyFeedbackConstraintsTool) Name() string { return "apply_editor_feedback_constraints" }
func (t *ApplyFeedbackConstraintsTool) Description() string {
	return "人工显式应用一条编辑反馈样本的约束修订建议（checker_config 七参数 / preference 偏好）。" +
		"已应用过的样本幂等拒绝不重复应用。这是唯一能把约束建议落成生成配置的入口（零自动改配置）。" +
		"平台级样本（novel_id=0）应用 checker_config 约束时必须传 novel_id 指定目标小说。"
}
func (t *ApplyFeedbackConstraintsTool) Category() ToolCategory { return CategoryConsistencyCheck }
func (t *ApplyFeedbackConstraintsTool) JSONSchema() json.RawMessage {
	return SchemaOf(ApplyFeedbackConstraintsArgs{})
}
func (t *ApplyFeedbackConstraintsTool) ExposeToLLM() bool { return true }
func (t *ApplyFeedbackConstraintsTool) NewArgs() any      { return &ApplyFeedbackConstraintsArgs{} }

func (t *ApplyFeedbackConstraintsTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*ApplyFeedbackConstraintsArgs)
	store := quality.NewStore(tc.DB, tc.LoggerOrDefault())
	row, err := store.GetEditorFeedback(ctx, a.FeedbackID)
	if err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("反馈样本 %d 不存在", a.FeedbackID)}, nil
	}
	if row.ParseStatus != quality.FeedbackParseParsed {
		return &ToolResult{Success: false, Error: "该样本未完成拆解（parse_status=raw），无结构化约束可应用"}, nil
	}
	if row.ConstraintsAppliedAt != nil {
		return &ToolResult{
			Success: false,
			Error:   fmt.Sprintf("该样本的约束修订已于 %s 应用过（幂等拒绝，不重复应用）", row.ConstraintsAppliedAt.Format("2006-01-02 15:04")),
		}, nil
	}
	cons := row.FeedbackConstraints()
	if len(cons) == 0 {
		return &ToolResult{Success: false, Error: "该样本无约束修订建议可应用"}, nil
	}
	targetNovelID := row.NovelID
	if a.NovelID != nil {
		targetNovelID = *a.NovelID
	}

	// 第一遍：全部约束值解析/校验通过才动手（宁缺勿错：一半应用一半失败最坑）。
	var checkerCons, prefCons []quality.FeedbackConstraint
	var cfg *quality.CheckerConfig
	for _, c := range cons {
		switch c.Target {
		case quality.FeedbackTargetChecker:
			if targetNovelID <= 0 {
				return &ToolResult{Success: false, Error: "平台级样本应用 checker_config 约束需传 novel_id 指定目标小说"}, nil
			}
			if cfg == nil {
				cfg, err = store.GetCheckerConfig(ctx, targetNovelID)
				if err != nil {
					return nil, fmt.Errorf("apply feedback constraints: 读检查器配置: %w", err)
				}
			}
			if err := applyCheckerConstraintField(cfg, c); err != nil {
				return &ToolResult{
					Success: false,
					Error:   fmt.Sprintf("约束 [%s=%s] 校验失败: %s", c.Key, c.Value, err),
				}, nil
			}
			checkerCons = append(checkerCons, c)
		case quality.FeedbackTargetPreference:
			prefCons = append(prefCons, c)
		}
	}

	// 第二遍：应用。checker_config 先落（按 novel_id upsert，重试幂等）；preference 后落
	// （事务批量；失败重试时 checker 重放无副作用、preference 未成行不重复）。
	if len(checkerCons) > 0 {
		if err := store.SaveCheckerConfig(ctx, cfg); err != nil {
			return nil, fmt.Errorf("apply feedback constraints: 写检查器配置: %w", err)
		}
	}
	appliedPref := make([]string, 0, len(prefCons))
	if len(prefCons) > 0 {
		isGlobal := targetNovelID == 0
		if err := tc.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, c := range prefCons {
				item := UpsertPreferenceItem{Category: c.Key, Content: c.Value}
				if isGlobal {
					item.IsGlobal = &isGlobal
				}
				if _, err := upsertOnePreference(tx, targetNovelID, item); err != nil {
					return err
				}
				appliedPref = append(appliedPref, c.Key)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("apply feedback constraints: 写偏好: %w", err)
		}
	}
	if err := store.MarkFeedbackConstraintsApplied(ctx, row.ID); err != nil {
		return nil, fmt.Errorf("apply feedback constraints: 标记已应用: %w", err)
	}

	appliedChecker := make([]string, 0, len(checkerCons))
	for _, c := range checkerCons {
		appliedChecker = append(appliedChecker, c.Key)
	}
	return &ToolResult{
		Success: true,
		Data: map[string]any{
			"feedback_id":        row.ID,
			"novel_id":           targetNovelID,
			"applied_checker":    appliedChecker,
			"applied_preference": appliedPref,
		},
	}, nil
}

// applyCheckerConstraintField 把一条 checker_config 约束按键解析写入配置（值语义校验；
// 清单类值原样写入，由 SaveCheckerConfig 的 Validate 归一化/拒收）。
func applyCheckerConstraintField(cfg *quality.CheckerConfig, c quality.FeedbackConstraint) error {
	v := strings.TrimSpace(c.Value)
	switch c.Key {
	case quality.ConstraintKeyBeatDensityMin:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			return fmt.Errorf("beat_density_min 不是非负数字：%q", c.Value)
		}
		cfg.BeatDensityMin = f
	case quality.ConstraintKeyTailHookRequired:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("tail_hook_required 不是布尔值：%q", c.Value)
		}
		cfg.TailHookRequired = &b
	case quality.ConstraintKeyWindowN:
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("window_n 不是非负整数：%q", c.Value)
		}
		cfg.WindowN = n
	case quality.ConstraintKeyTabooList:
		cfg.TabooList = v
	case quality.ConstraintKeyPremiseKeywords:
		cfg.PremiseList = v
	case quality.ConstraintKeyCheatKeywords:
		cfg.CheatList = v
	case quality.ConstraintKeyProtagonistNames:
		cfg.ProtagonistList = v
	default:
		return fmt.Errorf("未知 checker_config 约束键 %q", c.Key)
	}
	return nil
}

// ── 注册 ──────────────────────────────────────────────

// RegisterFeedbackTools 注册编辑反馈回流工具。
func RegisterFeedbackTools(r *Registry) {
	r.Register(&RecordEditorFeedbackTool{})
	r.Register(&GetEditorFeedbacksTool{})
	r.Register(&ApplyFeedbackConstraintsTool{})
}
