package mcp_tools

// provenance_tools.go —— 台账条目 provenance（R2b-7，提案 §8.3 组件 B）：
// 事实条目带来源章节 + 提取置信/人工确认标记；核心事实可钉住（钉住后自动修改
// 被写路径护栏拒收，需走 retcon 级审批）。
//
// 消费侧单源：双向对账巡检（structure.CheckLedgerReconcile 来源支撑核验）；
// statesync/写手提取时标来源与置信，作者核对后标 confirmed，核心事实标 pinned。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/novelgo/novelgo/internal/storage"
)

// ── upsert_ledger_provenance ────────────────────────────

// UpsertLedgerProvenanceArgs 是 upsert_ledger_provenance 的参数（局部覆盖语义：
// 未给的字段保持现值）。定位 (entry_table, entry_id) 必填。
type UpsertLedgerProvenanceArgs struct {
	EntryTable      string  `json:"entry_table" jsonschema:"description=台账表名（characters/locations/story_arcs/time_entries/reader_perspectives/chronology_anchors 等）" validate:"required"`
	EntryID         int64   `json:"entry_id" jsonschema:"description=台账条目 ID（对应表的 id）" validate:"required,min=1"`
	SourceChapterID *int64  `json:"source_chapter_id" jsonschema:"description=来源章节（chapters.id）：该事实从哪章正文提取/确立。未给=保持现值"`
	Confidence      *string `json:"confidence" jsonschema:"description=提取置信：high|medium|low（封闭枚举）。未给=保持现值"`
	Confirmed       *bool   `json:"confirmed" jsonschema:"description=人工确认标记：true=已核对该条目为真。未给=保持现值"`
	Pinned          *bool   `json:"pinned" jsonschema:"description=钉住（核心事实）：true 后任何自动语义修改将被拒收（需走 retcon 级审批，作者手动修改不受限）。未给=保持现值"`
}

// UpsertLedgerProvenanceTool 标注/更新一条台账条目的 provenance。
type UpsertLedgerProvenanceTool struct{}

func (t *UpsertLedgerProvenanceTool) Name() string { return "upsert_ledger_provenance" }
func (t *UpsertLedgerProvenanceTool) Description() string {
	return "标注/更新一条台账条目的来源与可信度（provenance）：\n" +
		"- entry_table + entry_id：定位台账条目（如 characters + 3）\n" +
		"- source_chapter_id：来源章节（该事实从哪章正文提取/确立）\n" +
		"- confidence：提取置信 high|medium|low\n" +
		"- confirmed：人工确认标记（已核对为真）\n" +
		"- pinned：钉住核心事实（钉住后自动修改需走 retcon 级审批）\n" +
		"局部覆盖：未给的字段保持现值。提取事实时请尽量标注来源章节与置信。"
}
func (t *UpsertLedgerProvenanceTool) Category() ToolCategory { return CategoryWritingAssistant }
func (t *UpsertLedgerProvenanceTool) JSONSchema() json.RawMessage {
	return SchemaOf(UpsertLedgerProvenanceArgs{})
}
func (t *UpsertLedgerProvenanceTool) ExposeToLLM() bool { return true }
func (t *UpsertLedgerProvenanceTool) NewArgs() any      { return &UpsertLedgerProvenanceArgs{} }

func (t *UpsertLedgerProvenanceTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*UpsertLedgerProvenanceArgs)
	store := storage.NewProvenanceStore(tc.DB)

	cur, err := store.Get(ctx, tc.NovelID, a.EntryTable, a.EntryID)
	if err != nil {
		return nil, fmt.Errorf("get ledger provenance: %w", err)
	}
	if cur == nil {
		cur = &storage.LedgerProvenance{NovelID: tc.NovelID, EntryTable: a.EntryTable, EntryID: a.EntryID}
	}
	// 局部覆盖：nil=保持现值。
	if a.SourceChapterID != nil {
		v := *a.SourceChapterID
		cur.SourceChapterID = &v
	}
	if a.Confidence != nil {
		cur.Confidence = *a.Confidence
	}
	if a.Confirmed != nil {
		cur.Confirmed = *a.Confirmed
	}
	if a.Pinned != nil {
		cur.Pinned = *a.Pinned
	}
	// 契约拒收（定位缺失/置信非法）是业务错误，LLM 补齐可成功——
	// 按 error_semantics 走业务拒绝（Success=false），不计入中断。
	if err := store.Upsert(ctx, cur); err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &ToolResult{Success: true, Data: provenanceData(cur)}, nil
}

// ── list_ledger_provenance ──────────────────────────────

// ListLedgerProvenanceArgs 是 list_ledger_provenance 的参数（无参数）。
type ListLedgerProvenanceArgs struct{}

// ListLedgerProvenanceTool 查看本书台账条目的 provenance 标注清单。
type ListLedgerProvenanceTool struct{}

func (t *ListLedgerProvenanceTool) Name() string { return "list_ledger_provenance" }
func (t *ListLedgerProvenanceTool) Description() string {
	return "查看本书台账条目的来源/置信/确认/钉住标注清单（provenance）：定位（表名:id）、" +
		"来源章节、提取置信、是否人工确认、是否钉住。未标注的条目不出现在清单中。"
}
func (t *ListLedgerProvenanceTool) Category() ToolCategory { return CategoryMemoryRetrieval }
func (t *ListLedgerProvenanceTool) JSONSchema() json.RawMessage {
	return SchemaOf(ListLedgerProvenanceArgs{})
}
func (t *ListLedgerProvenanceTool) ExposeToLLM() bool { return true }
func (t *ListLedgerProvenanceTool) NewArgs() any      { return &ListLedgerProvenanceArgs{} }

func (t *ListLedgerProvenanceTool) Execute(ctx context.Context, _ any, tc ToolContext) (*ToolResult, error) {
	rows, err := storage.NewProvenanceStore(tc.DB).ListByNovel(ctx, tc.NovelID)
	if err != nil {
		return nil, fmt.Errorf("list ledger provenance: %w", err)
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		items = append(items, provenanceData(&rows[i]))
	}
	return &ToolResult{Success: true, Data: map[string]any{"provenance": items}}, nil
}

// provenanceData 投影 provenance 标注为工具返回（消费侧口径）。
func provenanceData(p *storage.LedgerProvenance) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"entry_table": p.EntryTable,
		"entry_id":    p.EntryID,
		"source_chapter_id": func() any {
			if p.SourceChapterID == nil {
				return nil
			}
			return *p.SourceChapterID
		}(),
		"confidence": p.Confidence,
		"confirmed":  p.Confirmed,
		"pinned":     p.Pinned,
	}
}

// RegisterProvenanceTools 注册台账 provenance 标注工具。
func RegisterProvenanceTools(r *Registry) {
	r.Register(&UpsertLedgerProvenanceTool{})
	r.Register(&ListLedgerProvenanceTool{})
}
