package app

// writing_skill_conditions.go —— #111 P1：写章技法按章况条件化扩容。
// 设计单源见 docs/skill-wiring-contract-2026-11-09.md §3.3 / §6.2。
//
// 纪律：章况探测任何一步失败都降级为「该维度未知」，对应条件一律不命中，
// 退回 writingBundleSkills 的固定 3 个技法；不新增 DB 强依赖、不改 chat.go 调用签名。
// 所有追加技能仍受 slash_command.go 的单技能/整块双封顶约束（非空才注入、空则逐字不变）。

import (
	"context"
	"strings"

	"github.com/novelgo/novelgo/internal/quality"
)

// writingSkillCondition 是条件化追加技法的单源条目。
// id 供命中判定使用；skills 是命中后按序追加的技能名。
type writingSkillCondition struct {
	id     string
	skills []string
}

// writingSkillConditions 是写章技法条件表（健康检查静态闸会逐个校验这些技能
// 存在、声明 writing_bundle mount 且非 internalOnly）。
var writingSkillConditions = []writingSkillCondition{
	{"opening", []string{"opening-chapters"}},
	{"new_world", []string{"worldbuilding-design"}},
	{"dialogue", []string{"dialogue-subtext"}},
	{"rhythm", []string{"pacing-control"}},
	{"stuck", []string{"brainstorm-composer"}},
}

// writingChapterContext 是本章章况快照。零值 = 全未知（除消息类条件外一律不命中）。
type writingChapterContext struct {
	nextReadingNumber int    // 下一章阅读序号；0 = 取数失败/未知
	taskCardText      string // 本章任务卡四件套拼接；空 = 无卡或读取失败
	rhythmRequired    bool   // 平台 profile 指名节奏模板
	message           string // 本轮用户消息
}

// detectWritingChapterContext 从 App 已有的确定性取数路径拼出章况快照。
// 每一步都允许失败：失败只让对应条件不命中，绝不阻断写章注入。
func (a *App) detectWritingChapterContext(ctx context.Context, novelID int64, message string) writingChapterContext {
	c := writingChapterContext{message: message}
	if a == nil || a.db == nil {
		return c
	}
	if a.chapter != nil {
		if n, err := a.chapter.CountByNovel(ctx, a.db, novelID); err == nil {
			c.nextReadingNumber = n + 1
		}
	}
	if a.timeline != nil && c.nextReadingNumber > 0 {
		if card, err := a.timeline.GetTaskCard(ctx, novelID, c.nextReadingNumber); err == nil && card != nil {
			c.taskCardText = strings.Join([]string{
				card.HookTask, card.BeatTask, card.MomentTask, card.AdvanceTask,
			}, "\n")
		}
	}
	if cfg, err := quality.NewStore(a.db, a.logger).GetCheckerConfig(ctx, novelID); err == nil && cfg != nil {
		if p := cfg.Profile(); p != nil && p.RhythmTemplate != "" {
			c.rhythmRequired = true
		}
	}
	return c
}

// hit 判定某个条件是否命中；未知维度一律返回 false。
func (c writingChapterContext) hit(id string) bool {
	hay := strings.ToLower(c.taskCardText + "\n" + c.message)
	switch id {
	case "opening":
		return c.nextReadingNumber > 0 && c.nextReadingNumber <= 3
	case "new_world":
		return containsAny(hay, "新卷", "新地图", "新势力", "首次登场", "新设定")
	case "dialogue":
		return containsAny(hay, "对白戏", "对话戏", "潜台词", "大量对白", "对白推进")
	case "rhythm":
		return c.rhythmRequired
	case "stuck":
		return containsAny(hay, "卡文", "没思路", "写不出", "没灵感", "不知道怎么写")
	}
	return false
}

// conditionWritingSkillNames 按条件表顺序返回命中的技能名（去重，确定性）。
func conditionWritingSkillNames(c writingChapterContext) []string {
	seen := map[string]bool{}
	var out []string
	for _, cond := range writingSkillConditions {
		if !c.hit(cond.id) {
			continue
		}
		for _, name := range cond.skills {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

func containsAny(hay string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(hay, n) {
			return true
		}
	}
	return false
}
