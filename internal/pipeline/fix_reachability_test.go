package pipeline

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// ── 修复派发前字段可达性（#102）────────────────────────────────────────────

func TestUnreachableLedgerColumn(t *testing.T) {
	cases := []struct {
		name string
		desc string
		want string
	}{
		{"结构化列带赋值", "角色关系 is_current=1 与历史快照冲突", "is_current"},
		{"结构化列带字段语境", "台账 sealed 字段仍为 true，但正文已解封", "sealed"},
		{"归属列", "角色 volume_id=3 归属错误", "volume_id"},
		{"时间戳列未改", "changed_at 未改导致时序对不上", "changed_at"},
		{"长列名不被短列名误匹配", "changed_in_volume_id=2 归属漂移", "changed_in_volume_id"},
		{"alive 已开放不得误杀", "台账角色 #121 赵彦 alive=1，与第2章正文死亡事实冲突", ""},
		{"普通叙述词不误杀", "这个角色在剧情中扮演关键 role，戏份不足", ""},
		{"自由文本字段不误杀", "description=旧设定 与正文不符", ""},
		{"空描述", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, unreachableLedgerColumn(tc.desc))
		})
	}
}

func TestFilterUnreachableFixIssues(t *testing.T) {
	issues := []AuditIssue{
		{ID: "I1", Desc: "台账 is_current=1 与历史关系冲突", FixType: FixTypeLedger},
		{ID: "I2", Desc: "角色 alive=1 与正文死亡事实冲突", FixType: FixTypeLedger},
		{ID: "I3", Desc: "开场灌水", FixType: FixTypeLedger},
	}
	kept, reasons := filterUnreachableFixIssues(issues)
	require.Len(t, kept, 2)
	require.Equal(t, "I2", kept[0].ID)
	require.Equal(t, "I3", kept[1].ID)
	require.Len(t, reasons, 1)
	require.Contains(t, reasons["I1"], "is_current")
	require.Contains(t, reasons["I1"], "人工")
}

// TestWritableCharacterColumnsIncludeAlive 锁住清单与工具面的同步：工具面已开放 alive，
// 若有人把它误加回不可写清单，会把本可修复的巡检张力项再次判成转人工（#102 回归）。
func TestWritableCharacterColumnsIncludeAlive(t *testing.T) {
	require.Contains(t, writableCharacterColumns, "alive")
	for _, w := range writableCharacterColumns {
		require.NotContains(t, unwritableLedgerColumns, w, "可写字段不得同时出现在不可写清单：%s", w)
	}
	require.Empty(t, unreachableLedgerColumn("台账角色 #121 赵彦 alive=1，与第2章正文死亡事实冲突"))
}

func TestFixReachabilityGuardContent(t *testing.T) {
	g := fixReachabilityGuard()
	require.Contains(t, g, "字段可达性护栏")
	require.Contains(t, g, "alive")
	require.Contains(t, g, "skipped", "护栏须明确指引「不可写列直接申报 skipped」")
	require.Contains(t, g, "不要只在 description", "护栏须禁止等效文本标注冒充修复")
}

// TestRunIssueFixTicketsInjectsReachabilityGuard 护栏在派发层注入（fixPrompt 全文被
// golden 基线逐字锁定，不得改动契约单源），实际派发给 LLM 的 prompt 必须带上它。
func TestRunIssueFixTicketsInjectsReachabilityGuard(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(782)

	var mu sync.Mutex
	var fixPromptSeen string
	svc.skillRunner = func(_ context.Context, name string, in SkillInput) (SkillOutput, error) {
		if name == "signability-fix" {
			mu.Lock()
			fixPromptSeen = in.Prompt
			mu.Unlock()
			return SkillOutput{Text: `{"fixed":["I1"],"skipped":[]}`}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}
	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[{"id":"I1","level":"P1","area":"设定","desc":"开场设定未交代清楚","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID)
	require.NoError(t, svc.StartIssueFixTickets(ctx, novelID,
		[]IssueTicketRef{{RunID: runID, IssueKey: "I1"}}))

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, fixPromptSeen, "字段可达性护栏")
	require.Contains(t, fixPromptSeen, "alive")
}

// TestRunIssueFixTicketsSkipsUnreachableColumn 回归空转：命中不可写结构化列的台账级
// 工单应在派发前直接落 skipped 转人工，一次 signability-fix 都不派发（旧路径会派
// 修复→只改自由文本→复验判只改一半→再修一轮）。
func TestRunIssueFixTicketsSkipsUnreachableColumn(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(781)

	var mu sync.Mutex
	fixCalls := 0
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-fix" {
			mu.Lock()
			fixCalls++
			mu.Unlock()
		}
		return SkillOutput{Text: `{"fixed":[],"skipped":[]}`}, nil
	}
	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[{"id":"I1","level":"P0","area":"台账","desc":"角色关系 is_current=1 与历史快照冲突，需重置","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID)

	require.NoError(t, svc.StartIssueFixTickets(ctx, novelID,
		[]IssueTicketRef{{RunID: runID, IssueKey: "I1"}}))

	rows, err := svc.store.ListIssues(ctx, novelID, runID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, IssueStatusSkipped, rows[0].Status)
	require.Contains(t, rows[0].SkipReason, "is_current")
	require.Contains(t, rows[0].SkipReason, "白名单")
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 0, fixCalls, "不可写列的问题不应派发 LLM 修复空转")
}
