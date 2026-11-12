package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// #114 P2：turn_id / finding_keys 由 ctx 一次性下沉，13 处埋点零改签名；
// 未绑定（pipeline / 子任务 / 旧链路）时恒零值；聚合口径 GROUP BY 不变。

func TestSkillInvocationCarriesTurnAndFindingKeysFromContext(t *testing.T) {
	t.Cleanup(func() { SetSkillInvocationSink(nil) })

	var got SkillInvocation
	SetSkillInvocationSink(func(_ context.Context, inv SkillInvocation) { got = inv })

	snapshot := `["l0|tail_hook|#0","l1|reader_state|#0"]`
	ctx := WithTurnFindingKeys(context.Background(), 42, snapshot)
	RecordSkillInvocation(ctx, SkillInvocation{
		Skill: "scene-beats", Channel: "writing_bundle", NovelID: 7,
	})
	require.Equal(t, 42, got.TurnID, "turn_id 复用 chat 链路 NextTurn 的既有 int")
	require.Equal(t, snapshot, got.FindingKeys)
	require.Equal(t, "scene-beats", got.Skill)

	// 未绑定：零值，不污染 pipeline / 子任务归属。
	RecordSkillInvocation(context.Background(), SkillInvocation{Skill: "chapter-ci", Channel: "chapter_ci"})
	require.Zero(t, got.TurnID)
	require.Empty(t, got.FindingKeys)
}

func TestWithTurnFindingKeysFreshContext(t *testing.T) {
	t.Cleanup(func() { SetSkillInvocationSink(nil) })
	var got SkillInvocation
	SetSkillInvocationSink(func(_ context.Context, inv SkillInvocation) { got = inv })
	RecordSkillInvocation(WithTurnFindingKeys(context.Background(), 9, "keys"), SkillInvocation{Channel: "slash"})
	require.Equal(t, 9, got.TurnID)
	require.Equal(t, "keys", got.FindingKeys)
}

func TestSummarizeSkillInvocationsLatestTurnAndFindingKeys(t *testing.T) {
	s, db := newTestStore(t)
	require.NoError(t, db.AutoMigrate(&SkillInvocation{}))
	ctx := context.Background()

	// 第一轮：无目标发现项。
	s.AddSkillInvocation(ctx, SkillInvocation{
		Skill: "next", Channel: "chat_intent", NovelID: 7, TurnID: 11,
	})
	// 第二轮：改稿轮，带 P1 单源 key 快照。
	s.AddSkillInvocation(ctx, SkillInvocation{
		Skill: "next", Channel: "chat_intent", NovelID: 7, TurnID: 12,
		FindingKeys: `["l0|tail_hook|#0"]`,
	})

	stats, err := s.SummarizeSkillInvocations(ctx, 30)
	require.NoError(t, err)
	require.Len(t, stats, 1, "GROUP BY 仍是 skill + channel，行数不变")
	require.EqualValues(t, 2, stats[0].Count, "计数语义不被新维度影响")
	require.EqualValues(t, 12, stats[0].LastTurnID)
	require.Equal(t, `["l0|tail_hook|#0"]`, stats[0].FindingKeys,
		"取最近一条非空目标发现项快照，而非窗口内任意一行")
}
