package mcp_tools

// post_write_policy.go —— #119 P0：写后维护子 Agent 的生命周期与可见性单源。
//
// 2026-11-14 02:01 实测：正文已在 01:59:54 落盘并通过评分，主 chat 在 02:01:41
// 撞上 30 分钟硬上限；但 statesync / chapterci 由 debounce 派发时用了
// context.WithoutCancel(ctx) 且没有自己的超时，于是在主 ctx 死后继续跑了 6 分钟以上，
// 不断往会话写「正在思考」与工具结果、继续消耗 LLM 额度，而结果已无人收口。
//
// 这里只做两件事（不改派发链、不新增执行体、不引入自动修复）：
//   - 给 detached 链一个独立且有界的预算，主 ctx 结束后仍可收口但不会无限续命；
//   - 给请求打 Detached 标记，由 agent 层换独立事件前缀 + 关闭前端可见性。
//
// 纪律：纯本地、零上传、零自动修复；前台 run_subagent 与手动重检不走这里。

import (
	"context"
	"time"
)

// postWriteSubAgentTimeout 是写后维护子 Agent 的独立预算。
// 8 分钟覆盖一次正常的思考模型调用（现有 idle watchdog 为 9 分钟），
// 同时保证主轮结束后后台不会无限挂住。
const postWriteSubAgentTimeout = 8 * time.Minute

// detachedPostWriteContext 派生「脱离主 ctx 取消、但自带超时」的写后维护上下文。
// 调用方必须 defer cancel()。
func detachedPostWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), postWriteSubAgentTimeout)
}

// detachedPostWriteSubAgent 把写后维护的派发请求标记为后台，零改被调函数签名。
// 其他调用点（app/handler.go 的手动重检、pipeline、测试）不经过这里，行为逐字不变。
func detachedPostWriteSubAgent(run RunSubAgentFunc) RunSubAgentFunc {
	if run == nil {
		return nil
	}
	return func(ctx context.Context, req SubAgentRequest) (string, error) {
		req.Detached = true
		return run(ctx, req)
	}
}
