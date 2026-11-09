package activity

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// #111 P0 统一账本：观测自身绝不影响业务（未注入 / panic 均静默），
// 只记标识与字符数、空 skill 的通道级事件不参与技能用量统计。

func TestRecordSkillInvocationNoopAndPanicSafe(t *testing.T) {
	t.Cleanup(func() { SetSkillInvocationSink(nil) })

	SetSkillInvocationSink(nil)
	require.NotPanics(t, func() {
		RecordSkillInvocation(context.Background(), SkillInvocation{Skill: "next", Channel: "chat_intent"})
	}, "未注入 sink 时必须完全 no-op（零行为变更）")

	SetSkillInvocationSink(func(context.Context, SkillInvocation) { panic("sink boom") })
	require.NotPanics(t, func() {
		RecordSkillInvocation(context.Background(), SkillInvocation{Skill: "next", Channel: "chat_intent"})
	}, "sink panic 必须被吞掉")

	var got SkillInvocation
	SetSkillInvocationSink(func(_ context.Context, inv SkillInvocation) { got = inv })
	RecordSkillInvocation(context.Background(), SkillInvocation{Skill: "scene-beats", Channel: "writing_bundle", Runes: 812})
	require.Equal(t, "scene-beats", got.Skill)
	require.Equal(t, "writing_bundle", got.Channel)
	require.Equal(t, 812, got.Runes)
}

func TestSkillInvocationSinkWiresToStore(t *testing.T) {
	s, db := newTestStore(t)
	require.NoError(t, db.AutoMigrate(&SkillInvocation{}))
	t.Cleanup(func() { SetSkillInvocationSink(nil) })
	SetSkillInvocationSink(s.AddSkillInvocation)

	RecordSkillInvocation(context.Background(), SkillInvocation{
		Skill: "next", Channel: "chat_intent", Mount: "chat_intent", NovelID: 7, Runes: 320,
	})
	var count int64
	require.NoError(t, db.Model(&SkillInvocation{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestAddSkillInvocationRejectsBlankChannel(t *testing.T) {
	s, db := newTestStore(t)
	require.NoError(t, db.AutoMigrate(&SkillInvocation{}))
	ctx := context.Background()
	s.AddSkillInvocation(ctx, SkillInvocation{Skill: "next"})
	s.AddSkillInvocation(ctx, SkillInvocation{Skill: "next", Channel: "  "})
	var count int64
	require.NoError(t, db.Model(&SkillInvocation{}).Count(&count).Error)
	require.Zero(t, count, "无通道标识的样本必须拒收")
}

func TestSummarizeSkillInvocationsGroupsBySkillAndChannel(t *testing.T) {
	s, db := newTestStore(t)
	require.NoError(t, db.AutoMigrate(&SkillInvocation{}))
	ctx := context.Background()

	s.AddSkillInvocation(ctx, SkillInvocation{Skill: "next", Channel: "chat_intent", Mount: "chat_intent", NovelID: 7, Runes: 320})
	s.AddSkillInvocation(ctx, SkillInvocation{Skill: "next", Channel: "chat_intent", Mount: "chat_intent", NovelID: 7, Runes: 300})
	s.AddSkillInvocation(ctx, SkillInvocation{Skill: "next", Channel: "slash", Mount: "slash_only", NovelID: 7, Runes: 310})
	s.AddSkillInvocation(ctx, SkillInvocation{Skill: "chapter-ci", Channel: "chapter_ci", Mount: "chapter_ci", NovelID: 7, ChapterID: 12, Runes: 900})
	// 通道级事件：P0 的 state_sync 仍无技能正文挂载，不得计入技能用量。
	s.AddSkillInvocation(ctx, SkillInvocation{Channel: "state_sync", Mount: "state_sync", NovelID: 7, ChapterID: 12})
	// 窗口外样本：不进近 30 天汇总。
	s.AddSkillInvocation(ctx, SkillInvocation{Skill: "legacy", Channel: "slash", CreatedAt: s.now().AddDate(0, 0, -40)})

	var total int64
	require.NoError(t, db.Model(&SkillInvocation{}).Count(&total).Error)
	require.EqualValues(t, 6, total, "含通道事件与窗口外样本共 6 行")

	stats, err := s.SummarizeSkillInvocations(ctx, 30)
	require.NoError(t, err)
	require.Len(t, stats, 3, "空 skill 与窗口外样本都被过滤")
	require.Equal(t, "next", stats[0].Skill)
	require.Equal(t, "chat_intent", stats[0].Channel)
	require.EqualValues(t, 2, stats[0].Count)
	require.NotEmpty(t, stats[0].LastSeen, "最近一次时间应可读")
	require.True(t, strings.HasPrefix(stats[0].LastSeen, "2027"), "时间戳应来自 store 时钟")

	byKey := map[string]int64{}
	for _, st := range stats {
		byKey[st.Skill+"/"+st.Channel] = st.Count
	}
	require.EqualValues(t, 2, byKey["next/chat_intent"])
	require.EqualValues(t, 1, byKey["next/slash"])
	require.EqualValues(t, 1, byKey["chapter-ci/chapter_ci"])
}

func TestSkillInvocationTableName(t *testing.T) {
	require.Equal(t, "skill_invocations", (SkillInvocation{}).TableName())
}
