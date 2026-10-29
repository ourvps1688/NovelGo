// Package event 抽象 Wails 运行时事件发布，避免领域包（agent/pipeline/mcp_tools）
// 直接依赖 wails runtime，改为由 app 注入实现（符合 AGENTS.md 分层约束：
// 领域包不得 import wails，基础设施经接口/provider 注入）。
package event

import "context"

// Emitter 事件发布抽象。领域包仅依赖此接口，由 app 提供基于 wails runtime 的实现。
// 调用约定对齐 wails runtime.EventsEmit：name 为事件名，data 为可选载荷。
type Emitter interface {
	Emit(ctx context.Context, name string, data ...any)
}

// NopEmitter 丢弃所有事件，供测试与未注入时使用，不产生任何副作用。
type NopEmitter struct{}

// Emit 实现 Emitter，无任何操作。
func (NopEmitter) Emit(context.Context, string, ...any) {}
