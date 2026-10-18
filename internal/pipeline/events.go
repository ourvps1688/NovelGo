package pipeline

import (
	"context"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 流水线全局事件名（前端经 EventsOn 订阅）。
const (
	EvStage      = "pipeline:stage"       // 阶段变更：{run_id, novel_id, stage, status, note}
	EvGate       = "pipeline:gate"        // 闸门待确认：{run_id, novel_id, stage, gate_id, summary, options}
	EvControl    = "pipeline:control"     // 前端→后端控制指令：{run_id, cmd: start|pause|resume|stop}
	EvControlAck = "pipeline:control:ack" // 后端→前端控制确认：{run_id, cmd, status}
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
