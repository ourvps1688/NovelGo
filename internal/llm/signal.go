package llm

import "sync"

// #114 P2 供应商可观测：把此前只有 logger.Warn 的四类失败信号以「发信号」
// 的方式交给 app 层落本地指标表。观测自身绝不影响业务行为：
//   - 未注入 sink 完全 no-op（测试与旧链路零行为变更）；
//   - sink panic 吞掉；
//   - 不带 ctx（信号落库是 best-effort 短写，天然不受请求取消影响，
//     与 activity.RecordSkillInvocation 的 context.WithoutCancel 同纪律）；
//   - 保留原有全部 logger.Warn，不改 Retryable / Hard / 重试矩阵 / 熔断状态机。
//
// 依赖方向：本包只定义纯数据结构与可替换 sink，由 app.OnStartup 注入
// (*activity.Store).AddLLMSignal 的适配器；llm 不反向依赖 activity/gorm。
const (
	SignalTruncatedByLength = "truncated_by_length" // finish_reason=length，输出预算截断
	SignalToolArgsInvalid   = "tool_args_invalid"   // 工具参数 JSON 校验失败
	SignalEmptyStream       = "empty_stream"        // 流结束但零有效产出
	SignalBreakerOpen       = "breaker_open"        // 熔断拦截快速失败

	// TaskKindUnknown 是未显式标注任务类型时的兜底（GenerateText/CallTool 等
	// 不经 agent.RunOptions 的入口）。chat 主链路与 pipeline 子 agent 由
	// CallOptions.TaskKind 填 agent.RunOptions.AgentType，是准确值。
	TaskKindUnknown  = "unknown"
	TaskKindGenerate = "generate"
)

// LLMSignalSample 是一条失败信号的标识样本：只含标识，不含正文/错误原文。
type LLMSignalSample struct {
	Provider string
	Model    string
	TaskKind string
	Signal   string
}

var (
	llmSignalMu   sync.RWMutex
	llmSignalSink func(LLMSignalSample)
)

// SetLLMSignalSink 注入/替换信号落库口；传 nil 关闭观测。
func SetLLMSignalSink(fn func(LLMSignalSample)) {
	llmSignalMu.Lock()
	llmSignalSink = fn
	llmSignalMu.Unlock()
}

// RecordLLMSignal 发一条失败信号。观测失败绝不向调用方返回错误。
func RecordLLMSignal(s LLMSignalSample) {
	llmSignalMu.RLock()
	sink := llmSignalSink
	llmSignalMu.RUnlock()
	if sink == nil {
		return
	}
	defer func() { _ = recover() }()
	sink(s)
}

// TaskKindFromOptions 从调用选项中取任务类型，缺省回落 unknown。
func TaskKindFromOptions(opts *CallOptions) string {
	if opts != nil && opts.TaskKind != "" {
		return opts.TaskKind
	}
	return TaskKindUnknown
}

// NormalizeTaskKind 归一任务类型字符串（空=兜底值）。
func NormalizeTaskKind(kind string) string {
	if kind == "" {
		return TaskKindUnknown
	}
	return kind
}
