//go:build cgo

package mcp_tools

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/stretchr/testify/require"
)

// skillInvocations 捕获一次装配期间写进统一账本的记录；测试结束后还原为 no-op sink。
func skillInvocations(t *testing.T) *[]activity.SkillInvocation {
	t.Helper()
	var got []activity.SkillInvocation
	activity.SetSkillInvocationSink(func(_ context.Context, inv activity.SkillInvocation) {
		got = append(got, inv)
	})
	t.Cleanup(func() { activity.SetSkillInvocationSink(nil) })
	return &got
}

// #111 P2：store 缺失时前置核对块为空，P0 指令逐字不变（降级纪律）。
func TestStateSyncSkillBlock_NilStoreDegrades(t *testing.T) {
	block, mounted := stateSyncSkillBlock(nil, 1, 7)
	require.Empty(t, block)
	require.False(t, mounted)
}

func TestRunStateSyncForChapter_NilSkillStoreVerbatim(t *testing.T) {
	invs := skillInvocations(t)
	var instruction string
	stub := func(_ context.Context, req SubAgentRequest) (string, error) {
		instruction = req.Instruction
		return "", nil
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NoError(t, RunStateSyncForChapter(
		context.Background(), nil, nil, stub, logger, "test", 1, 7,
	))
	require.NotContains(t, instruction, "前置核对")
	require.Contains(t, instruction, "刚刚完成了章节（chapter_id=7）")
	require.Contains(t, instruction, "update_character(alive=false)")
	// 降级时仍记通道级事件，但 Skill 必须为空（不伪造技能用量）。
	require.Len(t, *invs, 1)
	require.Empty(t, (*invs)[0].Skill)
	require.Equal(t, "state_sync", (*invs)[0].Channel)
}

// #111 P2：collect + memory 正文真正进入 statesync 指令，各自记一条具体技能账
// （两个零使用技能由此获得确定性挂载，而不是靠用户手动 /memory）。
func TestRunStateSyncForChapter_SkillsMountedAndRecorded(t *testing.T) {
	invs := skillInvocations(t)
	store := newCISkillStore(t)
	var instruction string
	stub := func(_ context.Context, req SubAgentRequest) (string, error) {
		instruction = req.Instruction
		return "", nil
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NoError(t, RunStateSyncForChapter(
		context.Background(), nil, store, stub, logger, "test", 1, 7,
	))

	require.Contains(t, instruction, "前置核对")
	require.Contains(t, instruction, "【collect】")
	require.Contains(t, instruction, "收集清单（按用户指令裁剪）")
	require.Contains(t, instruction, "【memory】")
	require.Contains(t, instruction, "严禁派发子 Agent")
	require.Less(t, strings.Index(instruction, "前置核对"), strings.Index(instruction, "刚刚完成了章节"))
	// #103 的生死态派生职责不得被新块冲掉。
	require.Contains(t, instruction, "update_character(alive=false)")
	// db=nil 走「本地派生不可用」降级分支，仍须保留生死态同步职责文案。
	require.Contains(t, instruction, "本地派生不可用")
	require.NotContains(t, instruction, "\r", "注入的技能正文须统一为 LF")

	require.Len(t, *invs, len(stateSyncSkills), "技能注入时不得再记空 Skill 的通道事件")
	for i, e := range stateSyncSkills {
		inv := (*invs)[i]
		require.Equal(t, e.name, inv.Skill)
		require.Equal(t, "state_sync", inv.Channel)
		require.Equal(t, "state_sync", inv.Mount)
		require.Equal(t, int64(7), inv.ChapterID)
		require.Positive(t, inv.Runes)
		require.LessOrEqual(t, inv.Runes, e.cap)
	}
}
