package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// #116 P1 技能效果可度量：把改稿轮的 finding_keys 与每章最新一轮
// finding_round 快照关联，回答「技能参与后目标发现项消掉没有」。
// 纪律：同 (skill, channel, novel, chapter, turn) 只算一轮；空 finding_keys
// 的装配事件不参与；无快照的章节绝不反推成「全部消掉」；窗口外不进汇总。

const createEffectQualityTable = `CREATE TABLE chapter_quality (
  novel_id INTEGER NOT NULL,
  chapter_id INTEGER NOT NULL,
  finding_round TEXT
)`

func seedSkillEffects(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.db.WithContext(ctx).Exec(createEffectQualityTable).Error)
	require.NoError(t, s.db.AutoMigrate(&SkillInvocation{}))

	// 第 1 章最新一轮：k1 消掉、k2 仍在。
	require.NoError(t, s.db.Exec(
		`INSERT INTO chapter_quality(novel_id, chapter_id, finding_round) VALUES(7, 1, ?)`,
		`{"round":3,"resolved":["l0|tail_hook|#0"],"persisted":["l0|opening_slope|#0"]}`,
	).Error)
	// 第 2 章：无快照（旧档案）——不得反推成全部消掉。
	require.NoError(t, s.db.Exec(
		`INSERT INTO chapter_quality(novel_id, chapter_id, finding_round) VALUES(7, 2, '')`,
	).Error)

	// 改稿轮：scene-beats 命中 k1/k2；同一 turn 的两个埋点只能算一轮。
	s.AddSkillInvocation(ctx, SkillInvocation{
		Skill: "scene-beats", Channel: "writing_bundle", NovelID: 7, ChapterID: 1,
		TurnID: 12, FindingKeys: `["l0|tail_hook|#0","l0|opening_slope|#0"]`,
	})
	s.AddSkillInvocation(ctx, SkillInvocation{
		Skill: "scene-beats", Channel: "writing_bundle", NovelID: 7, ChapterID: 1,
		TurnID: 12, FindingKeys: `["l0|tail_hook|#0","l0|opening_slope|#0"]`,
	})
	// 另一个 turn、另一通道：只带 k1（消掉）。
	s.AddSkillInvocation(ctx, SkillInvocation{
		Skill: "revision-pass", Channel: "chat_intent", NovelID: 7, ChapterID: 1,
		TurnID: 13, FindingKeys: `["l0|tail_hook|#0"]`,
	})
	// 无快照章节：只计参与轮，不产生 resolved/persisted。
	s.AddSkillInvocation(ctx, SkillInvocation{
		Skill: "deai-flavor", Channel: "chapter_ci_selfcheck", NovelID: 7, ChapterID: 2,
		TurnID: 14, FindingKeys: `["l0|ai_flavor|#0"]`,
	})
	// 空 finding_keys 的普通装配事件：不参与统计。
	s.AddSkillInvocation(ctx, SkillInvocation{
		Skill: "next", Channel: "chat_intent", NovelID: 7, ChapterID: 1, TurnID: 15,
	})
	// 空 skill 的通道级事件：不参与统计。
	s.AddSkillInvocation(ctx, SkillInvocation{
		Channel: "state_sync", NovelID: 7, ChapterID: 1, TurnID: 16,
	})
}

func TestSummarizeSkillEffectsLinksRoundsToResolution(t *testing.T) {
	s, _ := newTestStore(t)
	seedSkillEffects(t, s)

	stats, err := s.SummarizeSkillEffects(context.Background(), 30)
	require.NoError(t, err)
	require.Len(t, stats, 3, "按 skill+channel 聚合；空 skill/空 finding_keys 不参与")

	byKey := map[string]SkillEffectStat{}
	for _, st := range stats {
		byKey[st.Skill+"|"+st.Channel] = st
	}

	scene := byKey["scene-beats|writing_bundle"]
	require.EqualValues(t, 1, scene.Rounds, "同 turn 重复埋点去重后只算一轮")
	require.EqualValues(t, 1, scene.Resolved)
	require.EqualValues(t, 1, scene.Persisted)

	revision := byKey["revision-pass|chat_intent"]
	require.EqualValues(t, 1, revision.Rounds)
	require.EqualValues(t, 1, revision.Resolved)
	require.Zero(t, revision.Persisted)

	deai := byKey["deai-flavor|chapter_ci_selfcheck"]
	require.EqualValues(t, 1, deai.Rounds, "无快照章节仍计参与轮")
	require.Zero(t, deai.Resolved, "无快照绝不反推成全部消掉")
	require.Zero(t, deai.Persisted)

	_, hasNext := byKey["next|chat_intent"]
	require.False(t, hasNext, "空 finding_keys 的装配事件不参与")
}

func TestSummarizeSkillEffectsExcludesOutOfWindowAndBadJSON(t *testing.T) {
	s, db := newTestStore(t)
	seedSkillEffects(t, s)
	ctx := context.Background()

	// 窗口外（2026-01-01）：不得进入近 30 天汇总。
	require.NoError(t, db.Exec(
		`INSERT INTO skill_invocations(skill, channel, novel_id, chapter_id, turn_id, finding_keys, runes, created_at)
		 VALUES('old-skill','old',7,1,99,'["l0|tail_hook|#0"]',0,'2026-01-01 00:00:00')`,
	).Error)
	// 坏 JSON：跳过该轮，不 panic、不臆造收敛。
	require.NoError(t, db.Exec(
		`INSERT INTO skill_invocations(skill, channel, novel_id, chapter_id, turn_id, finding_keys, runes, created_at)
		 VALUES('bad-json','chat_intent',7,1,98,'not-json',0,'2027-01-01 00:00:00')`,
	).Error)

	stats, err := s.SummarizeSkillEffects(ctx, 30)
	require.NoError(t, err)
	require.Len(t, stats, 3, "窗口外与坏 JSON 均不产生新聚合行")
	for _, st := range stats {
		require.NotEqual(t, "old-skill", st.Skill)
		require.NotEqual(t, "bad-json", st.Skill)
	}
}

func TestSummarizeSkillEffectsEmptyLedger(t *testing.T) {
	s, _ := newTestStore(t)
	require.NoError(t, s.db.Exec(createEffectQualityTable).Error)
	require.NoError(t, s.db.AutoMigrate(&SkillInvocation{}))
	stats, err := s.SummarizeSkillEffects(context.Background(), 0)
	require.NoError(t, err, "days 非正按 30 天；空账本返回空表，前端静默降级")
	require.Empty(t, stats)
}
