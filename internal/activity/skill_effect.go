package activity

// skill_effect.go —— #116 技能价值可度量（纯本地只读聚合，零写侧）。
//
// 旧账本只记「注入了什么、多少字符」，无法回答「这个技能有没有让发现项消掉」。
// 这里把 skill_invocations.finding_keys（带【本章目标发现项】的改稿轮）与
// chapter_quality.finding_round 的三态快照关联，产出每技能的
// 「参与改稿轮 / 消掉 / 仍在」统计，让「哪些技能真有用」第一次可观测。
//
// 口径限制（如实标注，绝不臆造）：finding_round 只保留每章最新一轮快照，
// 故为「最近一轮近似」而非逐轮历史；无快照的旧档案不参与计数，绝不反推成
// 「全部消掉」。不记正文、不记错误原文、不上传；不新增表、不新增执行体、
// 不派发任何修复。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SkillEffectStat 是某技能在改稿轮里与发现收敛的关联统计。
type SkillEffectStat struct {
	Skill     string `json:"skill"`
	Channel   string `json:"channel"`
	Rounds    int64  `json:"rounds"`
	Resolved  int64  `json:"resolved"`
	Persisted int64  `json:"persisted"`
}

type skillEffectRound struct {
	Skill       string `gorm:"column:skill"`
	Channel     string `gorm:"column:channel"`
	NovelID     int64  `gorm:"column:novel_id"`
	ChapterID   int64  `gorm:"column:chapter_id"`
	TurnID      int    `gorm:"column:turn_id"`
	FindingKeys string `gorm:"column:finding_keys"`
}

type skillEffectRoundRow struct {
	NovelID      int64  `gorm:"column:novel_id"`
	ChapterID    int64  `gorm:"column:chapter_id"`
	FindingRound string `gorm:"column:finding_round"`
}

type skillEffectSnapshot struct {
	Resolved  []string `json:"resolved"`
	Persisted []string `json:"persisted"`
}

// SummarizeSkillEffects 返回近 days 天（含今天）按 skill + channel 聚合的
// 改稿轮收敛统计，按参与轮数降序。days 非正时按 30 天；查询失败返回错误，
// 由调用方静默降级为空表。空 skill / 空 finding_keys 的装配事件不参与。
func (s *Store) SummarizeSkillEffects(ctx context.Context, days int) ([]SkillEffectStat, error) {
	if days <= 0 {
		days = DefaultStatDays
	}
	dayStart := s.now().AddDate(0, 0, -(days - 1))
	since := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 0, 0, 0, 0, dayStart.Location())

	var rounds []skillEffectRound
	if err := s.db.WithContext(ctx).Model(&SkillInvocation{}).
		Select("skill, channel, novel_id, chapter_id, turn_id, finding_keys").
		Where("created_at >= ? AND skill <> '' AND finding_keys <> ''", since).
		Scan(&rounds).Error; err != nil {
		return nil, err
	}
	var rows []skillEffectRoundRow
	if err := s.db.WithContext(ctx).Table("chapter_quality").
		Select("novel_id, chapter_id, finding_round").
		Where("finding_round IS NOT NULL AND finding_round <> ''").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	resolved, persisted := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, r := range rows {
		var snap skillEffectSnapshot
		if err := json.Unmarshal([]byte(r.FindingRound), &snap); err != nil {
			continue
		}
		scope := fmt.Sprintf("%d:%d", r.NovelID, r.ChapterID)
		resolved[scope], persisted[scope] = map[string]bool{}, map[string]bool{}
		for _, k := range snap.Resolved {
			resolved[scope][k] = true
		}
		for _, k := range snap.Persisted {
			persisted[scope][k] = true
		}
	}

	type agg struct {
		skill, channel       string
		rounds, res, persist int64
	}
	byKey := map[string]*agg{}
	seen := map[string]bool{}
	for _, r := range rounds {
		// 同一 (skill, channel, novel, chapter, turn) 只算一轮：一轮里多个
		// 埋点会重复写同一份 finding_keys，不能重复计数。
		dedup := fmt.Sprintf("%s|%s|%d|%d|%d", r.Skill, r.Channel, r.NovelID, r.ChapterID, r.TurnID)
		if seen[dedup] {
			continue
		}
		seen[dedup] = true
		var keys []string
		if err := json.Unmarshal([]byte(r.FindingKeys), &keys); err != nil {
			continue
		}
		ak := r.Skill + "|" + r.Channel
		a := byKey[ak]
		if a == nil {
			a = &agg{skill: r.Skill, channel: r.Channel}
			byKey[ak] = a
		}
		a.rounds++
		scope := fmt.Sprintf("%d:%d", r.NovelID, r.ChapterID)
		for _, k := range keys {
			if resolved[scope][k] {
				a.res++
			}
			if persisted[scope][k] {
				a.persist++
			}
		}
	}

	out := make([]SkillEffectStat, 0, len(byKey))
	for _, a := range byKey {
		out = append(out, SkillEffectStat{
			Skill: a.skill, Channel: a.channel,
			Rounds: a.rounds, Resolved: a.res, Persisted: a.persist,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rounds != out[j].Rounds {
			return out[i].Rounds > out[j].Rounds
		}
		if out[i].Skill != out[j].Skill {
			return strings.Compare(out[i].Skill, out[j].Skill) < 0
		}
		return strings.Compare(out[i].Channel, out[j].Channel) < 0
	})
	return out, nil
}
