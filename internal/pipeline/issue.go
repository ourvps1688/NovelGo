package pipeline

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ── issue 一等公民落表（R1 §4.3「修复工单化」）──────────────────────────────
// 体检 issue 不再只活在 GateContext.Audit 的 JSON 串里：落独立表 pipeline_issues，
// 带稳定键、结构化章节作用域（chapter_ids，替代 parseChapterRefs 正则猜测，F5 根治）、
// 修复路由字段（fix_type/target_scope/dimension/direction）、状态与轮次。
// JSON 副本仍是提示词/前端渲染载荷；本表是结构化事实源（作用域分组、状态流转、统计）。

// issue 工单状态（与 GateContext.FixStates 取值对齐；pending/fixing 是落表后的进行态）。
const (
	IssueStatusPending = "pending"
	IssueStatusFixing  = "fixing"
	IssueStatusFixed   = "fixed"
	IssueStatusSkipped = "skipped"
	IssueStatusRewrite = "rewrite"
	IssueStatusRetcon  = "retcon"
)

// PipelineIssue 是一条体检 issue 的持久化工单。
type PipelineIssue struct {
	ID      int64  `gorm:"column:id;primaryKey;autoIncrement"`
	NovelID int64  `gorm:"column:novel_id;not null;index"`
	RunID   string `gorm:"column:run_id;not null;index;uniqueIndex:idx_pipeline_issues_run_key,priority:1"`
	// IssueKey 是 run 内稳定键（issue 契约 ID，如 I1/V1/D1）。每轮体检整批替换
	// （新体检=新问题清单，与 executeAudit 重置 FixStates 同语义），跨轮同号
	// 不共用断点。
	IssueKey    string `gorm:"column:issue_key;not null;uniqueIndex:idx_pipeline_issues_run_key,priority:2"`
	Level       string `gorm:"column:level;not null"`
	Area        string `gorm:"column:area"`
	Desc        string `gorm:"column:desc;type:text"`
	FixType     string `gorm:"column:fix_type"`
	TargetScope string `gorm:"column:target_scope"`
	Dimension   string `gorm:"column:dimension"`
	Direction   string `gorm:"column:direction"`
	// ChapterIDsJSON 是涉及章节全局阅读序号（JSON 数组序列化，如 [1,2,3]）。
	// 存的是 issueChapterIDs 的有效值（结构化字段优先，正则兜底），供作用域分组直接消费。
	ChapterIDsJSON string `gorm:"column:chapter_ids;type:text"`
	Status         string `gorm:"column:status;not null;index"`
	SkipReason     string `gorm:"column:skip_reason;type:text"`
	// Round 是记录/流转时的修复轮数（GateContext.FixRounds），供轮次统计与增量复验分轮。
	Round     int       `gorm:"column:round;not null;default:0"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 指定物理表名。
func (PipelineIssue) TableName() string { return "pipeline_issues" }

// issueChapterIDs 返回 issue 的涉及章节全局阅读序号：契约结构化字段 chapter_ids 优先；
// 空时回退 parseChapterRefs 从 area/desc 文本正则兜底（兼容读：旧数据/LLM 未输出）。
// F5 的正则猜测从「唯一来源」降级为「兜底」，新数据一律走结构化字段。
func issueChapterIDs(it AuditIssue) []int64 {
	if len(it.ChapterIDs) > 0 {
		return it.ChapterIDs
	}
	return parseChapterRefs(it.Area, it.Desc)
}

// pipelineIssueRow 把一条契约 issue 投影为工单行（状态 pending，作用域取有效章节号）。
func pipelineIssueRow(novelID int64, runID string, round int, it AuditIssue) PipelineIssue {
	chapters, _ := json.Marshal(issueChapterIDs(it))
	return PipelineIssue{
		NovelID:        novelID,
		RunID:          runID,
		IssueKey:       it.ID,
		Level:          it.Level,
		Area:           it.Area,
		Desc:           it.Desc,
		FixType:        it.FixType,
		TargetScope:    it.TargetScope,
		Dimension:      it.Dimension,
		Direction:      it.Direction,
		ChapterIDsJSON: string(chapters),
		Status:         IssueStatusPending,
		Round:          round,
	}
}

// ReplaceIssues 以新一轮体检结果整批替换该 run 的 issue 工单（新体检=新问题清单）。
// round 记录本轮修复轮数，供增量复验分轮与统计。
func (s *Store) ReplaceIssues(ctx context.Context, novelID int64, runID string, round int, issues []AuditIssue) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("novel_id = ? AND run_id = ?", novelID, runID).
			Delete(&PipelineIssue{}).Error; err != nil {
			return err
		}
		for _, it := range issues {
			row := pipelineIssueRow(novelID, runID, round, it)
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateIssueStatus 流转一条工单状态（fixed/skipped/rewrite/retcon/...）；skipReason
// 非空时一并记录（skipped 即转人工工单的留痕依据）。
func (s *Store) UpdateIssueStatus(ctx context.Context, novelID int64, runID, issueKey, status, skipReason string) error {
	updates := map[string]any{"status": status, "updated_at": time.Now()}
	if skipReason != "" {
		updates["skip_reason"] = skipReason
	}
	return s.db.WithContext(ctx).Model(&PipelineIssue{}).
		Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
		Updates(updates).Error
}

// ListIssues 列出该 run 的全部 issue 工单（落表顺序即体检清单顺序）。
func (s *Store) ListIssues(ctx context.Context, novelID int64, runID string) ([]PipelineIssue, error) {
	var rows []PipelineIssue
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND run_id = ?", novelID, runID).
		Order("id").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
