package pipeline

// feedback_run.go —— 编辑反馈工单容器（P1-1 反馈回流，quality/feedback.go 契约消费侧）。
//
// 编辑反馈拆解出的 F 系 issue 要走既有修复闭环（工单面板/去修复/复验），就必须挂到
// 一个 run 上（PipelineIssue 以 (run_id, issue_key) 唯一）。这里复用检测容器机制
// （ModeDetect 家族、出生即 StatusFinished 的隔离纪律）：每条反馈样本一个容器，
// ReplaceIssues 整批落法与检测容器同语义；工单面板按 novel 跨 run 列出，天然聚合。
//
// 与检测容器的差异只有一个：检测容器同 novel+kind+scope 复用，反馈容器**每样本新建**
// （多条反馈的工单是累积关系，不做整批替换合并）。

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/quality"
)

// FeedbackKind 是编辑反馈容器的 kind（mode='detect:feedback'，detectModeOf 口径）。
// 命名空间裁决：detect:<kind> 家族现有 volume/book/structure，feedback 是第四个——
// GetDetectRun 只按显式 kind 查询，检测面板/恢复链路不会误碰本容器。
const FeedbackKind = "feedback"

// CreateFeedbackRun 新建编辑反馈容器 run（每条反馈样本一个，出生即 StatusFinished）。
// 返回容器行（RunID 由反馈样本留痕，工单 keyed by 它；重落工单时复用同一 RunID 走
// ReplaceIssues 整批替换）。
func (s *Store) CreateFeedbackRun(ctx context.Context, novelID int64) (*PipelineRun, error) {
	run := &PipelineRun{
		NovelID:      novelID,
		RunID:        "fb-" + uuid.NewString(),
		Mode:         detectModeOf(FeedbackKind),
		CurrentStage: detectContainerStage,
		Status:       StatusFinished,
	}
	if err := s.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("编辑反馈：创建反馈容器失败: %w", err)
	}
	return run, nil
}

// FeedbackIssuesToAudit 把 F 系反馈 issue（quality/feedback.go 契约）投影为工单契约
// issue。命名空间裁决：I 归常规体检、D 归蓝图漂移、T 归巡检、V 归文风漂移、F 归编辑
// 反馈；F 系不属 series bible 四维（Dimension=none）、不涉卷作用域（TargetScope=none，
// writeProtected 不命中，可正常派发修复）。
func FeedbackIssuesToAudit(list []quality.FeedbackIssue) []AuditIssue {
	out := make([]AuditIssue, 0, len(list))
	for _, f := range list {
		out = append(out, AuditIssue{
			ID:          f.ID,
			Level:       f.Level,
			Area:        f.Area,
			Desc:        f.Desc,
			FixType:     f.FixType,
			TargetScope: TargetScopeNone,
			Dimension:   DimensionNone,
			Direction:   DirectionForwardFix,
			ChapterIDs:  f.ChapterIDs,
		})
	}
	return out
}

// LandFeedbackTickets 把 F 系反馈 issue 落成 pipeline 工单（feedback 容器 +
// ReplaceIssues 整批落法），返回（runID, 落库条数）。novelID<=0（平台级样本）或
// 零 issue 直接跳过（只沉淀口径不落工单）。
//
// 调用方是 mcp_tools 编辑反馈工具：mcp_tools 不能反向 import 本包（pipeline→agent→
// mcp_tools 会成环），由 app 启动时把本函数注入 mcp_tools.FeedbackTicketLander
// （SetEmitter 同款装配纪律）。
func LandFeedbackTickets(ctx context.Context, db *gorm.DB, novelID int64, issues []quality.FeedbackIssue) (string, int, error) {
	if novelID <= 0 || len(issues) == 0 {
		return "", 0, nil
	}
	s := NewStore(db)
	run, err := s.CreateFeedbackRun(ctx, novelID)
	if err != nil {
		return "", 0, err
	}
	if err := s.ReplaceIssues(ctx, novelID, run.RunID, 0, FeedbackIssuesToAudit(issues)); err != nil {
		return "", 0, err
	}
	return run.RunID, len(issues), nil
}
