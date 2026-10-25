package mcp_tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/timeline"
)

// ── upsert_chapter_task_card ─────────────────────────────

// UpsertChapterTaskCardArgs 是 upsert_chapter_task_card 的参数。
// 四件套全部必填：缺任何一件由 Store.SaveTaskCard → Validate 拒收（机器可验契约）。
type UpsertChapterTaskCardArgs struct {
	TargetReadingNumber int    `json:"target_reading_number" jsonschema:"required,description=本章的阅读序号（第几章，从 1 起）" validate:"required,min=1"`
	HookTask            string `json:"hook_task" jsonschema:"required,description=钩子任务：本章埋/收哪个坑 + 章尾断章钩设计" validate:"required"`
	BeatTask            string `json:"beat_task" jsonschema:"required,description=爽点任务：爽点类型与情绪落点" validate:"required"`
	MomentTask          string `json:"moment_task" jsonschema:"required,description=人设时刻：哪位角色的高光/反差时刻" validate:"required"`
	AdvanceTask         string `json:"advance_task" jsonschema:"required,description=推进任务：推进哪条主线/卷纲功能位" validate:"required"`
	Source              string `json:"source" jsonschema:"description=创建者,enum=ai,enum=user" validate:"omitempty,oneof=ai user"`
}

// UpsertChapterTaskCardTool 立/更新某章的章纲任务卡（四件套）。
type UpsertChapterTaskCardTool struct{}

func (t *UpsertChapterTaskCardTool) Name() string { return "upsert_chapter_task_card" }
func (t *UpsertChapterTaskCardTool) Description() string {
	return "立/更新某章的章纲任务卡（结构化四件套，写新章前必须先立卡）：\n" +
		"- hook_task 钩子任务：本章埋/收哪个坑 + 章尾断章钩设计\n" +
		"- beat_task 爽点任务：爽点类型与情绪落点\n" +
		"- moment_task 人设时刻：哪位角色的高光/反差时刻\n" +
		"- advance_task 推进任务：推进哪条主线/卷纲功能位\n" +
		"四件套齐全才可落库（缺一拒收）。同一章重复调用整卡覆盖；大纲的伏笔操作/章末钩子须与本卡一致，写完正文后按卡核验兑现。"
}
func (t *UpsertChapterTaskCardTool) Category() ToolCategory { return CategoryWritingAssistant }
func (t *UpsertChapterTaskCardTool) JSONSchema() json.RawMessage {
	return SchemaOf(UpsertChapterTaskCardArgs{})
}
func (t *UpsertChapterTaskCardTool) ExposeToLLM() bool { return true }
func (t *UpsertChapterTaskCardTool) NewArgs() any      { return &UpsertChapterTaskCardArgs{} }

func (t *UpsertChapterTaskCardTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*UpsertChapterTaskCardArgs)

	card := timeline.ChapterTaskCard{
		NovelID:             tc.NovelID,
		TargetReadingNumber: a.TargetReadingNumber,
		HookTask:            a.HookTask,
		BeatTask:            a.BeatTask,
		MomentTask:          a.MomentTask,
		AdvanceTask:         a.AdvanceTask,
		Source:              a.Source,
	}
	if err := timeline.NewStore(tc.DB, tc.LoggerOrDefault()).SaveTaskCard(ctx, &card); err != nil {
		// 契约拒收（四件套缺项/序号非法）是业务错误，LLM 补齐字段可成功——
		// 按 error_semantics 走业务拒绝（Success=false），不计入中断。
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}

	// 写前对账 L1.5（R2b-4）：立卡后置检查，只报告不拦截（告警随回执返回）。
	warnings := planCheckWarnings(ctx, tc, card.TargetReadingNumber, card.HookTask,
		[]structure.PlanField{
			{Name: "hook_task", Text: card.HookTask},
			{Name: "beat_task", Text: card.BeatTask},
			{Name: "moment_task", Text: card.MomentTask},
			{Name: "advance_task", Text: card.AdvanceTask},
		})

	return &ToolResult{
		Success: true,
		Data: planCheckData(map[string]any{
			"target_reading_number": card.TargetReadingNumber,
		}, warnings),
	}, nil
}

// ── get_chapter_task_card ────────────────────────────────

// GetChapterTaskCardArgs 是 get_chapter_task_card 的参数。
type GetChapterTaskCardArgs struct {
	TargetReadingNumber int `json:"target_reading_number" jsonschema:"description=章的阅读序号；不传=下一章（当前章数+1）" validate:"omitempty,min=0"`
}

// GetChapterTaskCardTool 读取某章的章纲任务卡。
type GetChapterTaskCardTool struct{}

func (t *GetChapterTaskCardTool) Name() string { return "get_chapter_task_card" }
func (t *GetChapterTaskCardTool) Description() string {
	return "读取某章的章纲任务卡（钩子/爽点/人设时刻/推进 四件套）。不传 target_reading_number 时返回下一章（当前章数+1）的卡；未立卡时明确提示。写前取卡对齐、写后按卡核验都用本工具。"
}
func (t *GetChapterTaskCardTool) Category() ToolCategory { return CategoryMemoryRetrieval }
func (t *GetChapterTaskCardTool) JSONSchema() json.RawMessage {
	return SchemaOf(GetChapterTaskCardArgs{})
}
func (t *GetChapterTaskCardTool) ExposeToLLM() bool { return true }
func (t *GetChapterTaskCardTool) NewArgs() any      { return &GetChapterTaskCardArgs{} }

func (t *GetChapterTaskCardTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*GetChapterTaskCardArgs)

	readingNumber := a.TargetReadingNumber
	if readingNumber == 0 {
		n, err := chapter.NewStore(tc.DB, tc.LoggerOrDefault()).CountByNovel(ctx, nil, tc.NovelID)
		if err != nil {
			return nil, fmt.Errorf("count chapters: %w", err)
		}
		readingNumber = n + 1
	}

	card, err := timeline.NewStore(tc.DB, tc.LoggerOrDefault()).GetTaskCard(ctx, tc.NovelID, readingNumber)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &ToolResult{
				Success: true,
				Data: map[string]any{
					"target_reading_number": readingNumber,
					"exists":                false,
					"content":               fmt.Sprintf("第 %d 章任务卡：尚未立卡（写前须调 upsert_chapter_task_card 立四件套任务卡）", readingNumber),
				},
			}, nil
		}
		return nil, fmt.Errorf("get task card: %w", err)
	}

	return &ToolResult{
		Success: true,
		Data: map[string]any{
			"target_reading_number": readingNumber,
			"exists":                true,
			"content":               FormatTaskCard(card),
		},
	}, nil
}

// FormatTaskCard 渲染任务卡为写作上下文文本（get_chapter_task_card 与 get_timeline 注入共用单源）。
func FormatTaskCard(card *timeline.ChapterTaskCard) string {
	return fmt.Sprintf("第 %d 章任务卡：\n- 钩子任务：%s\n- 爽点任务：%s\n- 人设时刻：%s\n- 推进任务：%s",
		card.TargetReadingNumber, card.HookTask, card.BeatTask, card.MomentTask, card.AdvanceTask)
}

// ── 大纲审批 × 章纲任务卡联动核验（只报告不拦截）────────────────
//
// 大纲是「本章为什么写」的第一次成文：审批那一刻把任务卡四件套带进审批载荷，
// 作者对照契约核验（伏笔操作/章末钩子/爽点设计须与卡一致）；机器只报告不拦截。

// outlineTaskCardLink 解析本章任务卡并产出大纲审批联动信息。
// 新建大纲（尚无 chapter_id）走「下一章」阅读序号；已有章节优先按绑定 chapter_id，
// 未绑定再按阅读序号兜底。返回：
//   - payload：审批载荷增强——有卡时 {"task_card": 四件套}，缺卡但本书启用任务卡时
//     {"task_card_missing": true}；整本零卡（纯即兴写作）返回 nil（零打扰，宁可漏拦）。
//   - note：写后注入给主 Agent 的核验提示（有卡=对卡核验；缺卡=先立卡）；无提示时为空。
//
// 任何读取失败静默降级为「无卡且不提示」（降级逐字一致：失败=零提示零报错）。
func outlineTaskCardLink(ctx context.Context, tc ToolContext, chapterID int64, isNew bool) (map[string]any, string) {
	var card *timeline.ChapterTaskCard
	if isNew {
		card = taskCardForNextChapter(ctx, tc)
	} else {
		card = taskCardForChapter(ctx, tc, chapterID)
	}
	if card != nil {
		return map[string]any{"task_card": taskCardApprovalPayload(card)},
			"大纲已写入。请对照本章章纲任务卡核验一致性" +
				"（钩子任务的埋/收坑与章尾断章钩、爽点任务、人设时刻、推进任务应与大纲吻合）；" +
				"方向确需调整时，先调用 upsert_chapter_task_card 更新任务卡再改大纲，禁止卡文两张皮。"
	}
	if bookUsesTaskCards(ctx, tc) {
		return map[string]any{"task_card_missing": true},
			"本书已启用章纲任务卡，但本章尚未立卡。请先调用 upsert_chapter_task_card 为本章立四件套任务卡" +
				"（钩子任务/爽点任务/人设时刻/推进任务），并保持大纲与卡一致。"
	}
	return nil, ""
}

// taskCardForChapter 解析某章的章纲任务卡：优先按已绑定 chapter_id，
// 未绑定/查不到再按该章当前阅读序号兜底（chapter_id 回填钩子可能尚未生效）。
// 任何失败静默降级为 nil（视作无卡）。
func taskCardForChapter(ctx context.Context, tc ToolContext, chapterID int64) *timeline.ChapterTaskCard {
	cardStore := timeline.NewStore(tc.DB, tc.LoggerOrDefault())
	if card, err := cardStore.GetTaskCardByChapterID(ctx, tc.NovelID, chapterID); err == nil {
		return card
	}
	readingNumber, err := chapter.NewStore(tc.DB, tc.LoggerOrDefault()).GetReadingNumberByID(ctx, nil, tc.NovelID, chapterID)
	if err != nil {
		return nil
	}
	card, err := cardStore.GetTaskCard(ctx, tc.NovelID, readingNumber)
	if err != nil {
		return nil
	}
	return card
}

// taskCardForNextChapter 解析「下一章」（当前章数+1）的任务卡——
// 新建大纲/章节尚无 chapter_id 时的联动入口。失败静默降级为 nil。
func taskCardForNextChapter(ctx context.Context, tc ToolContext) *timeline.ChapterTaskCard {
	n, err := chapter.NewStore(tc.DB, tc.LoggerOrDefault()).CountByNovel(ctx, nil, tc.NovelID)
	if err != nil {
		return nil
	}
	card, err := timeline.NewStore(tc.DB, tc.LoggerOrDefault()).GetTaskCard(ctx, tc.NovelID, n+1)
	if err != nil {
		return nil
	}
	return card
}

// bookUsesTaskCards 判断本书是否已启用任务卡工作流（存在任一卡）。
// 失败/坏数据一律静默 false（宁可漏拦：纯即兴写作零打扰）。
func bookUsesTaskCards(ctx context.Context, tc ToolContext) bool {
	ok, err := timeline.NewStore(tc.DB, tc.LoggerOrDefault()).HasAnyTaskCard(ctx, tc.NovelID)
	if err != nil {
		return false
	}
	return ok
}

// taskCardApprovalPayload 把任务卡四件套裁成审批联动载荷（随大纲审批展示给作者对照核验）。
func taskCardApprovalPayload(card *timeline.ChapterTaskCard) map[string]any {
	return map[string]any{
		"target_reading_number": card.TargetReadingNumber,
		"hook_task":             card.HookTask,
		"beat_task":             card.BeatTask,
		"moment_task":           card.MomentTask,
		"advance_task":          card.AdvanceTask,
	}
}

// RegisterTaskCardTools 注册章纲任务卡工具。
func RegisterTaskCardTools(r *Registry) {
	r.Register(&UpsertChapterTaskCardTool{})
	r.Register(&GetChapterTaskCardTool{})
}
