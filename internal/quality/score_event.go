package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 章节评分历史（ChapterScoreEvent，P0-3 评分打通）：每次成章写评分门禁落库时
// 追加一行，保留 6 维判官分全量（review.ScoreResult.Dimensions），供「修复前后对比」
// 与逐章趋势回答「这次重写到底提分了没有」。
//
// 契约要点（勿改）：
//   - 只追加不更新（append-only）：每次成章写一行，历史不覆盖；
//     当前快照仍以 chapters.quality_score / quality_report 为准，本表是历史轴；
//   - 分维得分是 0-100 判官口径（六键=review.scoringDimensions），换算成 8 维体检
//     口径（1-5 星）用 review.JudgeToAuditScores（映射契约单源），本表只存原始分；
//   - 降级宁缺勿错：追加失败仅告警，不影响写作主流程。

// 评分事件的来源（写入通道）。前两者由成章写评分门禁触发，rescore 由用户在
// 「修复前后对比」里显式点「重新判分」触发（#115 P0）：局部改稿不自动评分，
// 也不会自动跑 LLM；显式入口让每次改稿都能留下一条可对比的分数记录。
const (
	ScoreSourceNew         = "new"          // chapters/new.md 新建章节
	ScoreSourceFullReplace = "full_replace" // 已有正文全文替换（含修复后重写）
	ScoreSourceRescore     = "rescore"      // 用户显式重判分（当前落盘正文）
)

// ChapterScoreEvent 是一次成章写评分的明细历史行（append-only）。
type ChapterScoreEvent struct {
	ID        int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID   int64 `gorm:"column:novel_id;not null;index:idx_score_event_chapter,priority:1" json:"novel_id"`
	ChapterID int64 `gorm:"column:chapter_id;not null;index:idx_score_event_chapter,priority:2" json:"chapter_id"`
	// ReadingNumber 是评分时的全局阅读序号快照（1-based，与 ChapterQuality 同口径）。
	ReadingNumber int `gorm:"column:reading_number;not null;index" json:"reading_number"`
	// Overall 是判官总分（0-100）。
	Overall int `gorm:"column:overall;not null" json:"overall"`
	// Dimensions 是 JSON：6 维判官分全量（map[string]int，键=review.scoringDimensions）。
	Dimensions string `gorm:"column:dimensions;type:text" json:"dimensions"`
	// Verdict 是判官自评（"pass"|"fail"，仅供参考，门禁以总分+阈值为准）。
	Verdict string `gorm:"column:verdict" json:"verdict"`
	// Critique 是本次判官的可执行修改意见。
	Critique string `gorm:"column:critique;type:text" json:"critique"`
	// FindingKeys 是 #114 P1 本轮评分时注入判官的未消除发现稳定 key 快照
	// （JSON：[]string，口径同 quality.FindingKeys）。append-only 历史轴使前端
	// 能对相邻两次评分做 resolved/persisted/new 对比；旧行空串=无快照，禁止
	// 反推成「全部消掉」。零新增查询：装配处方时已读过同一份档案。
	FindingKeys string `gorm:"column:finding_keys;type:text" json:"finding_keys"`
	// Source 是写入通道（ScoreSource*）。
	Source    string    `gorm:"column:source;not null" json:"source"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 返回物理表名。
func (ChapterScoreEvent) TableName() string { return "chapter_score_events" }

// MarshalScoreDims 序列化 6 维判官分为 JSON 文本；失败返回空串（宁缺勿错）。
func MarshalScoreDims(dims map[string]int) string {
	if len(dims) == 0 {
		return ""
	}
	b, err := json.Marshal(dims)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseScoreDims 解析 6 维判官分 JSON；空串/坏数据返回空映射（宁缺勿错，不报错）。
func ParseScoreDims(s string) map[string]int {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// MarshalFindingKeys 序列化一轮发现稳定 key 快照；空清单/失败返回空串（宁缺勿错）。
func MarshalFindingKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	b, err := json.Marshal(keys)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseFindingKeys 解析发现 key 快照；空串/坏数据返回 nil（旧数据天然兼容）。
func ParseFindingKeys(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var keys []string
	if err := json.Unmarshal([]byte(s), &keys); err != nil {
		return nil
	}
	return keys
}

// normalizeScoreEvent 校验评分历史行（宁缺勿错）：novel_id/chapter_id 缺失拒收。
func normalizeScoreEvent(e *ChapterScoreEvent) error {
	if e == nil {
		return errors.New("quality store: 评分事件为空")
	}
	if e.NovelID <= 0 || e.ChapterID <= 0 {
		return errors.New("quality store: novel_id/chapter_id 缺失")
	}
	if e.Source == "" {
		e.Source = ScoreSourceFullReplace
	}
	return nil
}

// AppendScoreEvent 追加一行评分历史（append-only，从不更新既有行）。
func (s *Store) AppendScoreEvent(ctx context.Context, e *ChapterScoreEvent) error {
	if err := normalizeScoreEvent(e); err != nil {
		return fmt.Errorf("quality store: append score event: %w", err)
	}
	if err := s.DB.WithContext(ctx).Create(e).Error; err != nil {
		return fmt.Errorf("quality store: append score event: %w", err)
	}
	return nil
}

// ListScoreEvents 返回某章的评分历史（按落库顺序升序，即时间正序）。
func (s *Store) ListScoreEvents(ctx context.Context, novelID, chapterID int64) ([]ChapterScoreEvent, error) {
	var list []ChapterScoreEvent
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND chapter_id = ?", novelID, chapterID).
		Order("id").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("quality store: list score events: %w", err)
	}
	return list, nil
}

// ListScoreEventsByNovel 返回全书评分历史（按落库顺序升序），供逐章趋势曲线消费。
func (s *Store) ListScoreEventsByNovel(ctx context.Context, novelID int64) ([]ChapterScoreEvent, error) {
	var list []ChapterScoreEvent
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("id").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("quality store: list score events by novel: %w", err)
	}
	return list, nil
}
