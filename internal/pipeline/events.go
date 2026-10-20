package pipeline

import (
	"context"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 流水线全局事件名（前端经 EventsOn 订阅）。
const (
	EvStage          = "pipeline:stage"           // 阶段变更：{run_id, novel_id, stage, status, note}
	EvGate           = "pipeline:gate"            // 闸门待确认：{run_id, novel_id, stage, gate_id, summary, options}
	EvControl        = "pipeline:control"         // 前端→后端控制指令：{run_id, cmd: start|pause|resume|stop}
	EvControlAck     = "pipeline:control:ack"     // 后端→前端控制确认：{run_id, cmd, status}
	EvStockAudit     = "pipeline:stock_audit"     // 全量对齐体检完成：{novel_id, run_id}
	EvFixStart       = "pipeline:fix_start"       // S5 修复开始：{run_id, novel_id, issues}（前端展示修复清单）
	EvFixDone        = "pipeline:fix_done"        // S5 修复结束：{run_id, novel_id, ok, error}（前端展示结果）
	EvRewriteBacklog = "pipeline:rewrite_backlog" // 正文级问题回流 S3 重写回路：{run_id, novel_id, items}（前端展示重写清单）
)

// StagePayload 阶段变更事件载荷。
type StagePayload struct {
	RunID   string `json:"run_id"`
	NovelID int64  `json:"novel_id"`
	Stage   string `json:"stage"`
	Status  string `json:"status"`
	Note    string `json:"note,omitempty"`
}

// GatePayload 闸门事件载荷，options 为前端可展示的按钮。
type GatePayload struct {
	RunID   string   `json:"run_id"`
	NovelID int64    `json:"novel_id"`
	Stage   string   `json:"stage"`
	GateID  string   `json:"gate_id"`
	Summary string   `json:"summary"`
	Options []string `json:"options"`
}

// ControlAckPayload 控制指令回执载荷。
type ControlAckPayload struct {
	RunID  string `json:"run_id"`
	Cmd    string `json:"cmd"`
	Status string `json:"status"`
}

// EmitStage 广播阶段变更。
func EmitStage(ctx context.Context, p StagePayload) {
	wails.EventsEmit(ctx, EvStage, p)
}

// EmitGate 广播闸门待确认。
func EmitGate(ctx context.Context, p GatePayload) {
	wails.EventsEmit(ctx, EvGate, p)
}

// EmitControlAck 广播控制指令回执。
func EmitControlAck(ctx context.Context, p ControlAckPayload) {
	wails.EventsEmit(ctx, EvControlAck, p)
}

// EmitStockAudit 广播「全量对齐」体检完成，通知前端刷新并展示结果。
func EmitStockAudit(ctx context.Context, p map[string]any) {
	wails.EventsEmit(ctx, EvStockAudit, p)
}

// FixIssuePayload 是 S5 修复开始事件里的一条修复项（来自体检 issue 清单）。
// Status 是该问题的修复状态：pending（待修复）/ fixing（正在修复）/ fixed（已修复），
// 供前端在修复清单中逐项标注，让用户知道 LLM 做到哪了。
type FixIssuePayload struct {
	ID     string `json:"id"`
	Level  string `json:"level"`
	Area   string `json:"area"`
	Desc   string `json:"desc"`
	Status string `json:"status,omitempty"`
}

// EmitFixStart 广播 S5 修复开始，携带本轮要修复的 issue 清单，
// 让用户看到「在修复什么问题、涉及哪些元素（角色/地点/弧线/读者视角…）」。
func EmitFixStart(ctx context.Context, p map[string]any) {
	wails.EventsEmit(ctx, EvFixStart, p)
}

// EmitFixDone 广播 S5 修复结束（ok=false 时带 error），前端展示修复结果。
func EmitFixDone(ctx context.Context, p map[string]any) {
	wails.EventsEmit(ctx, EvFixDone, p)
}

// EmitRewriteBacklog 广播「正文级问题回流 S3 重写回路」，通知前端刷新并展示重写清单。
func EmitRewriteBacklog(ctx context.Context, p map[string]any) {
	wails.EventsEmit(ctx, EvRewriteBacklog, p)
}
