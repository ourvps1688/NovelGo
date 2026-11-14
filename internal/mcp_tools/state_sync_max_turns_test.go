//go:build cgo

package mcp_tools

// #125 方案 1 契约：写后台账同步子 Agent 必须带轮数帽与收敛提示。
// turn40 实测它无 MaxTurns（沿用 RunSubAgent 默认 100 轮）跑了 11 轮、撞 10min
// 独立预算非阻断收口，且与 chapterci 并发抢同一供应商、反复读同一批材料。

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateSyncMaxTurnsContract(t *testing.T) {
	assert.Equal(t, 8, stateSyncMaxTurns)
	assert.Less(t, stateSyncMaxTurns, 100, "不得沿用 RunSubAgent 默认 100 轮")

	var req SubAgentRequest
	stub := func(_ context.Context, r SubAgentRequest) (string, error) {
		req = r
		return "", nil
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NoError(t, RunStateSyncForChapter(
		context.Background(), nil, nil, stub, logger, "test", 1, 7,
	))
	assert.Equal(t, "statesync", req.AgentType)
	assert.Equal(t, stateSyncMaxTurns, req.MaxTurns)
	assert.Contains(t, req.Instruction, "一次读齐所需材料")
	assert.Contains(t, req.Instruction, "勿反复检索同一来源")
	// 收敛提示是恒定后缀，不得冲掉 P0 主指令，也不得在技能降级时伪造前置核对块。
	assert.Contains(t, req.Instruction, "刚刚完成了章节（chapter_id=7）")
	assert.NotContains(t, req.Instruction, "前置核对")
}
