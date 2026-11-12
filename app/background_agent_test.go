package app

// #115 P0 静态接线 + 失败分类契约：后台子 Agent 不得再用空 RunOptions，
// 章节检测失败必须带稳定分类码，且失败不得清空上一轮 L1 档案。

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/quality"
)

func readAppSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	require.NoError(t, err, "读取接线源文件 %s", rel)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestBackgroundSubAgentWiring(t *testing.T) {
	for _, file := range []string{"handler.go", "editor_feedback.go"} {
		src := readAppSource(t, file)
		assert.NotContains(t, src, "agent.RunOptions{}",
			"%s 不得再用空 RunOptions 派发后台子 Agent（Model=nil 会在 agent.Run 入口直接失败）", file)
	}
	handler := readAppSource(t, "handler.go")
	for _, want := range []string{
		"a.runBackgroundSubAgent", "classifyChapterDetectError(",
	} {
		assert.Contains(t, handler, want, "handler.go 缺少接线 %q", want)
	}
	assert.Contains(t, readAppSource(t, "editor_feedback.go"), "a.runBackgroundSubAgent")
	bg := readAppSource(t, "background_agent.go")
	for _, want := range []string{
		"settingsSnapshot()", "parseModelKey(", "ProviderModel(",
		"a.agent.RunSubAgent(ctx, opts, req)", "DetectReason() string",
		"errBackgroundModelUnavailable", "quality.ErrL1ReceiptNotFound",
	} {
		assert.Contains(t, bg, want, "background_agent.go 缺少 %q", want)
	}
	ci := readAppSource(t, "../internal/mcp_tools/chapter_ci.go")
	assert.NotContains(t, ci, "record.L1Status = quality.L1StatusSkipped",
		"章节 CI 失败不得把上一轮 checked 结果覆盖成 skipped")
	assert.Contains(t, ci, "保留上一轮 L1")
}

func TestClassifyChapterDetectError(t *testing.T) {
	type reasoner interface{ DetectReason() string }
	extract := func(err error) string {
		t.Helper()
		var r reasoner
		require.True(t, errors.As(err, &r), "失败必须实现 DetectReason 接口")
		return r.DetectReason()
	}

	assert.Nil(t, classifyChapterDetectError(nil), "成功不产生分类错误")

	modelErr := fmt.Errorf("%w: mimo/mimo-v2.6-flash", errBackgroundModelUnavailable)
	got := classifyChapterDetectError(modelErr)
	assert.Equal(t, "model", extract(got))
	assert.Equal(t, modelErr.Error(), got.Error(), "Error() 保持原友好文案")
	assert.True(t, errors.Is(got, errBackgroundModelUnavailable), "Unwrap 必须保留可判定哨兵")

	assert.Equal(t, "receipt", extract(classifyChapterDetectError(quality.ErrL1ReceiptNotFound)))
	assert.Equal(t, "llm", extract(classifyChapterDetectError(errors.New("对话出错，请重试"))))
}
