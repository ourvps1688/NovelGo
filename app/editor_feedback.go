package app

import (
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/quality"
)

// EditorFeedbackInput 是前端显式录入一条平台编辑反馈的入参（#111 P2）。
// 与 MCP 工具 record_editor_feedback 的字段一一对应；novel_id=0 时按平台级样本处理。
type EditorFeedbackInput struct {
	NovelID   int64  `json:"novel_id"`
	WorkTitle string `json:"work_title"`
	Platform  string `json:"platform"`
	Channel   string `json:"channel"`
	Verdict   string `json:"verdict"`
}

// EditorFeedbackResult 是录入后的两段式回执。parse_status=raw 表示只留了原话
// （子 Agent 不可用 / 派发失败 / 回执解析失败），此时 degraded 给出原因。
type EditorFeedbackResult struct {
	FeedbackID      int64    `json:"feedback_id"`
	ParseStatus     string   `json:"parse_status"`
	Degraded        string   `json:"degraded,omitempty"`
	Dims            []string `json:"dims,omitempty"`
	IssueCount      int      `json:"issue_count"`
	ConstraintCount int      `json:"constraint_count"`
	RunID           string   `json:"run_id,omitempty"`
	TicketsLanded   int      `json:"tickets_landed"`
	Summary         string   `json:"summary,omitempty"`
}

// EditorFeedbackRow 是列表用的只读投影（不暴露 JSON 原文与时间指针，
// 避免 Wails 绑定生成带 *time.Time 的噪声 DTO）。
type EditorFeedbackRow struct {
	ID          int64  `json:"id"`
	NovelID     int64  `json:"novel_id"`
	WorkTitle   string `json:"work_title"`
	Platform    string `json:"platform"`
	Channel     string `json:"channel"`
	Verdict     string `json:"verdict"`
	ParseStatus string `json:"parse_status"`
	Summary     string `json:"summary"`
	IssueCount  int    `json:"issue_count"`
	TicketRunID string `json:"ticket_run_id"`
	CreatedAt   string `json:"created_at"`
}

// RecordEditorFeedback 是编辑反馈的前端显式入口：复用 record_editor_feedback 工具的
// 两段式落库与结构化拆解（含 F 系工单投影），不复制任何解析/回流逻辑。
// 子 Agent 不可用时工具会降级只存原话，绝不伪造结构化产物。
func (a *App) RecordEditorFeedback(input EditorFeedbackInput) (*EditorFeedbackResult, error) {
	if a.db == nil {
		return nil, fmt.Errorf("数据库未就绪，无法录入编辑反馈")
	}
	if strings.TrimSpace(input.Verdict) == "" {
		return nil, fmt.Errorf("编辑原话不能为空")
	}
	if strings.TrimSpace(input.WorkTitle) == "" || strings.TrimSpace(input.Platform) == "" {
		return nil, fmt.Errorf("作品书名与反馈平台不能为空")
	}
	novelID := input.NovelID
	args := &mcp_tools.RecordEditorFeedbackArgs{
		NovelID:   &novelID,
		WorkTitle: input.WorkTitle,
		Platform:  input.Platform,
		Channel:   input.Channel,
		Verdict:   input.Verdict,
	}
	tc := mcp_tools.ToolContext{
		DB:         a.db,
		NovelID:    novelID,
		SkillStore: a.skill,
		Logger:     a.logger,
	}
	if a.agent != nil {
		// #115 P0：与 chapterci / statesync 共用后台子 Agent 模型单源，
		// 空 RunOptions 会让 agent.Run 在入口直接失败（Model=nil）。
		tc.RunSubAgent = a.runBackgroundSubAgent
	}
	res, err := (&mcp_tools.RecordEditorFeedbackTool{}).Execute(a.ctx, args, tc)
	if err != nil {
		return nil, err
	}
	data := res.Data
	return &EditorFeedbackResult{
		FeedbackID:      mapInt64(data["feedback_id"]),
		ParseStatus:     mapString(data["parse_status"]),
		Degraded:        mapString(data["degraded"]),
		Dims:            mapStringList(data["dims"]),
		IssueCount:      mapInt(data["issue_count"]),
		ConstraintCount: mapInt(data["constraint_count"]),
		RunID:           mapString(data["run_id"]),
		TicketsLanded:   mapInt(data["tickets_landed"]),
		Summary:         mapString(data["summary"]),
	}, nil
}

// ListEditorFeedbacks 返回该书的反馈样本（含 novel_id=0 的平台级样本），按 id 倒序。
func (a *App) ListEditorFeedbacks(novelID int64) ([]EditorFeedbackRow, error) {
	out := []EditorFeedbackRow{}
	if a.db == nil {
		return out, nil
	}
	rows, err := quality.NewStore(a.db, a.logger).ListEditorFeedbacks(a.ctx, novelID)
	if err != nil {
		return out, err
	}
	for i := range rows {
		r := &rows[i]
		out = append(out, EditorFeedbackRow{
			ID:          r.ID,
			NovelID:     r.NovelID,
			WorkTitle:   r.WorkTitle,
			Platform:    r.Platform,
			Channel:     r.Channel,
			Verdict:     r.Verdict,
			ParseStatus: r.ParseStatus,
			Summary:     r.Summary,
			IssueCount:  len(r.FeedbackIssues()),
			TicketRunID: r.RunID,
			CreatedAt:   r.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return out, nil
}

func mapString(v any) string {
	s, _ := v.(string)
	return s
}

func mapInt(v any) int {
	n, _ := v.(int)
	return n
}

func mapInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func mapStringList(v any) []string {
	list, _ := v.([]string)
	return list
}
