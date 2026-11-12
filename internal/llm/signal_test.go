package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// #114 P2：信号通道只发标识，观测自身绝不影响业务行为。

func TestRecordLLMSignalNoopAndPanicSafe(t *testing.T) {
	t.Cleanup(func() { SetLLMSignalSink(nil) })

	SetLLMSignalSink(nil)
	require.NotPanics(t, func() {
		RecordLLMSignal(LLMSignalSample{Signal: SignalEmptyStream})
	}, "未注入 sink 时必须完全 no-op")

	SetLLMSignalSink(func(LLMSignalSample) { panic("sink boom") })
	require.NotPanics(t, func() {
		RecordLLMSignal(LLMSignalSample{Signal: SignalEmptyStream})
	}, "sink panic 必须被吞掉")

	var got LLMSignalSample
	SetLLMSignalSink(func(s LLMSignalSample) { got = s })
	RecordLLMSignal(LLMSignalSample{
		Provider: "deepseek", Model: "m1", TaskKind: "main", Signal: SignalBreakerOpen,
	})
	require.Equal(t, "deepseek", got.Provider)
	require.Equal(t, SignalBreakerOpen, got.Signal)
	require.Equal(t, "main", got.TaskKind)
}

func TestTaskKindFromOptions(t *testing.T) {
	require.Equal(t, TaskKindUnknown, TaskKindFromOptions(nil))
	require.Equal(t, TaskKindUnknown, TaskKindFromOptions(&CallOptions{}))
	require.Equal(t, "chapterci", TaskKindFromOptions(&CallOptions{TaskKind: "chapterci"}))
	require.Equal(t, TaskKindUnknown, NormalizeTaskKind(""))
	require.Equal(t, "review", NormalizeTaskKind("review"))
}

func TestLLMSignalConstantsStable(t *testing.T) {
	// 落库指标 key 是稳定字面量，改动会让历史数据与新数据无法对齐。
	require.Equal(t, "truncated_by_length", SignalTruncatedByLength)
	require.Equal(t, "tool_args_invalid", SignalToolArgsInvalid)
	require.Equal(t, "empty_stream", SignalEmptyStream)
	require.Equal(t, "breaker_open", SignalBreakerOpen)
	require.Equal(t, "unknown", TaskKindUnknown)
	require.Equal(t, "generate", TaskKindGenerate)
}
