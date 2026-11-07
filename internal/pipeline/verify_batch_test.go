package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
)

// ── 复验终极提速（2026-11-05）：批量合验 + 语料内联回归 ─────────────────────
//
// 契约：调用次数 N→⌈N/verifyBatchSize⌉；证据后端预取内联、禁止工具往返；
// 多目标回执逐 id 落库、故障隔离语义（未确认→pending 留待下轮）不变。

// TestRunVerifyTicketsBatchesCalls 验收批量合验主契约：5 张语义类工单（L0 不接管）
// 应压成 ⌈5/4⌉=2 次 signability-verify 调用，多目标回执逐 id 落库全量通过。
func TestRunVerifyTicketsBatchesCalls(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var mu sync.Mutex
	calls := 0
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		require.Equal(t, "signability-verify", name)
		mu.Lock()
		calls++
		mu.Unlock()
		// 多目标回执：一次调用覆盖全部 5 张（逐 id 拆分不受批分组影响）。
		return SkillOutput{Text: "```json\n" +
			`{"verified":[{"id":"I1","ok":true,"note":"已修复1"},{"id":"I2","ok":true,"note":"已修复2"},{"id":"I3","ok":true,"note":"已修复3"},{"id":"I4","ok":true,"note":"已修复4"},{"id":"I5","ok":true,"note":"已修复5"}],"summary":"5 项确认"}` +
			"\n```"}, nil
	}

	var issues []AuditIssue
	var refs []IssueTicketRef
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("I%d", i)
		// Desc 含语义排除词「矛盾」→ L0 判歧义保守让位 LLM（快路径不接管，保证批数确定）。
		issues = append(issues, AuditIssue{ID: id, Level: "P1", Area: "情节", Desc: "前后矛盾", FixType: FixTypeLedger})
		refs = append(refs, IssueTicketRef{RunID: "run-b", IssueKey: id})
	}
	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-b", 0, issues))

	sum, err := svc.VerifyIssueTickets(ctx, 7, refs)
	require.NoError(t, err)
	require.Equal(t, 5, sum.Queued)

	mu.Lock()
	got := calls
	mu.Unlock()
	require.Equal(t, 2, got, "5 张工单应压成 ⌈5/verifyBatchSize⌉=2 次调用")

	rows, lerr := svc.store.ListIssues(ctx, 7, "run-b")
	require.NoError(t, lerr)
	require.Len(t, rows, 5)
	for _, r := range rows {
		require.Equal(t, IssueStatusVerified, r.Status, "工单 %s 应逐 id 落库为已复验", r.IssueKey)
		require.NotEmpty(t, r.VerifyReceipt, "复验回执应落库")
	}
}

// TestVerifyBatchInlinesCorpus 验收语料内联契约：复验提示词应内联作用域章正文 + 台账快照，
// 明示禁止工具往返（取数与推理分离），并保留复验回执契约格式串。
func TestVerifyBatchInlinesCorpus(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}, &character.Character{}))
	chStore := chapter.NewStore(db, slog.Default())
	_, err := chStore.Create(context.Background(), nil, 901, nil, "第一章")
	require.NoError(t, err)
	require.NoError(t, db.Table("characters").Create(map[string]any{
		"novel_id": 901, "name": "林川", "description": "主角",
	}).Error)

	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.SetChapterTextReader(func(_ context.Context, _ int64, chapterID int64) (string, error) {
		return fmt.Sprintf("正文-%d", chapterID), nil
	})

	var mu sync.Mutex
	var prompt string
	svc.skillRunner = func(_ context.Context, _ string, input SkillInput) (SkillOutput, error) {
		mu.Lock()
		prompt = input.Prompt
		mu.Unlock()
		return SkillOutput{Text: "```json\n" +
			`{"verified":[{"id":"I1","ok":true,"note":"已修复"}],"summary":"1 项确认"}` +
			"\n```"}, nil
	}

	require.NoError(t, svc.store.ReplaceIssues(ctx, 901, "run-c", 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "情节", Desc: "前后矛盾", FixType: FixTypeLedger, ChapterIDs: []int64{1}},
	}))
	_, err = svc.VerifyIssueTickets(ctx, 901, []IssueTicketRef{{RunID: "run-c", IssueKey: "I1"}})
	require.NoError(t, err)

	mu.Lock()
	got := prompt
	mu.Unlock()
	require.NotEmpty(t, got, "复验应已派发")
	require.Contains(t, got, "【本次复验语料", "应注入语料块头")
	require.Contains(t, got, "禁止调用任何工具", "应明示禁止工具往返（证据已内联）")
	require.Contains(t, got, "第1章", "作用域章（阅读序号）应进语料")
	require.Contains(t, got, "正文-", "章正文应内联")
	require.Contains(t, got, "林川", "台账快照应内联")
	require.Contains(t, got, verifyReceiptFormatLine(), "复验回执契约格式串照常在提示词内")
}

// TestVerifyBatchZeroToolsAndLeanPrompt 验收复验二次提速（2026-11-05）的三个收紧点：
// ① 工具零暴露（Whitelist 空集——schema 不给工具，调用结构性不可能，单轮必出回执）；
// ② 提示词不再教工具取数（旧「用 get_* 读台账」与语料块禁令自相矛盾，曾诱导首轮调工具
//
//	烧轮）；③ 预算与任务体量同级（轮数 ≤2、输出 ≤10k token，防思维膨胀拖满单批预算）。
func TestVerifyBatchZeroToolsAndLeanPrompt(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var mu sync.Mutex
	var gotIn SkillInput
	svc.skillRunner = func(_ context.Context, _ string, input SkillInput) (SkillOutput, error) {
		mu.Lock()
		gotIn = input
		mu.Unlock()
		return SkillOutput{Text: "```json\n" +
			`{"verified":[{"id":"I1","ok":true,"note":"已修复"}],"summary":"1 项确认"}` +
			"\n```"}, nil
	}

	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-d", 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "情节", Desc: "前后矛盾", FixType: FixTypeLedger},
	}))
	_, err := svc.VerifyIssueTickets(ctx, 7, []IssueTicketRef{{RunID: "run-d", IssueKey: "I1"}})
	require.NoError(t, err)

	mu.Lock()
	in := gotIn
	mu.Unlock()
	require.NotNil(t, in.Whitelist, "Whitelist 非 nil 才会覆盖默认白名单")
	require.Empty(t, in.Whitelist, "证据内联 → 工具零暴露（空集）")
	require.LessOrEqual(t, in.MaxTurns, 2, "预算与任务体量同级：单批轮数 ≤2")
	require.LessOrEqual(t, in.MaxTokens, 10000, "预算与任务体量同级：单轮输出 ≤10k token")
	require.NotContains(t, in.Prompt, "用 get_*", "提示词不得再教工具取数（与禁令自相矛盾）")
	require.NotContains(t, in.Prompt, "按 chapters 读", "提示词不得再教工具取数")
	require.Contains(t, in.Prompt, "不要调用任何工具", "方法论明示禁工具")
	require.Contains(t, in.Prompt, "输出从简", "输出从简约束（压思维块膨胀）")
	require.Contains(t, in.Prompt, "回答第一段", "回执前置到回答第一段（截断只切尾部闲笔，2026-11-06 事故防线）")
}

// TestChunkIssuesAndCapHeadTail 切批与长文掐头尾的纯函数边界。
func TestChunkIssuesAndCapHeadTail(t *testing.T) {
	rows := []PipelineIssue{{IssueKey: "I1"}, {IssueKey: "I2"}, {IssueKey: "I3"}, {IssueKey: "I4"}, {IssueKey: "I5"}}
	batches := chunkIssues(rows, 4)
	require.Len(t, batches, 2)
	require.Len(t, batches[0], 4)
	require.Len(t, batches[1], 1)
	require.Len(t, chunkIssues(nil, 4), 0)
	require.Len(t, chunkIssues(rows, 0), 5, "size<=0 视为 1")

	require.Equal(t, "短文", capHeadTail("短文", 8000))
	long := strings.Repeat("啊", 9000)
	capped := capHeadTail(long, 8000)
	require.Less(t, len([]rune(capped)), 9000)
	require.Contains(t, capped, "中段已截断")
	require.True(t, strings.HasPrefix(capped, "啊"), "头部应保留")
	require.True(t, strings.HasSuffix(capped, "啊"), "尾部应保留（章尾钩子修复点）")
}
