package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/stretchr/testify/require"
)

const bibleMarker = "MARKER-FACTS：左手有疤（快照出处：volume_id=1，snapshot_version=2）"

// TestEstablishedFactsInjectedIntoAuditPrompt 验证 WBS 2.3：S4 体检提示词携带
// 「已冻结卷既定事实」块（依据前卷快照校验）；未接提供者时提示词与旧行为一致。
func TestEstablishedFactsInjectedIntoAuditPrompt(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var prompt string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			prompt = input.Prompt
		}
		return SkillOutput{Text: "结论：\n```json\n" +
			`{"verdict":"pass","score":31,"summary":"ok","issues":[]}` + "\n```"}, nil
	}
	svc.SetBibleProvider(func(_ context.Context, _ int64) (string, error) { return bibleMarker, nil })

	runID := startS3Run(t, svc, 800, `{}`)
	book, err := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, err)
	svc.enterAudit(ctx, book, "milestone")
	waitForStageStatus(t, svc, runID, StatusAwaitingGate)

	require.Contains(t, prompt, "【已冻结卷既定事实（系列圣经快照）】", "S4 提示词应含既定事实块")
	require.Contains(t, prompt, bibleMarker, "S4 提示词应含快照内容")
	require.Contains(t, prompt, "不得**为消解冲突回改已冻结卷", "既定事实块应声明前向修复依据")

	// 未接提供者：提示词不含注入块（向后兼容）。
	svc2 := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	var prompt2 string
	svc2.skillRunner = func(_ context.Context, _ string, input SkillInput) (SkillOutput, error) {
		prompt2 = input.Prompt
		return SkillOutput{Text: "结论：\n```json\n" +
			`{"verdict":"pass","score":31,"summary":"ok","issues":[]}` + "\n```"}, nil
	}
	runID2 := startS3Run(t, svc2, 801, `{}`)
	book2, _ := svc2.store.GetByRunID(ctx, runID2)
	svc2.enterAudit(ctx, book2, "milestone")
	waitForStageStatus(t, svc2, runID2, StatusAwaitingGate)
	require.NotContains(t, prompt2, "【已冻结卷既定事实", "无快照提供者不得注入")
}

// TestBibleProviderErrorDegrades 验证提供者出错时降级为不注入（提示词照常生成、不崩溃）。
func TestBibleProviderErrorDegrades(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var prompt string
	svc.skillRunner = func(_ context.Context, _ string, input SkillInput) (SkillOutput, error) {
		prompt = input.Prompt
		return SkillOutput{Text: "结论：\n```json\n" +
			`{"verdict":"pass","score":31,"summary":"ok","issues":[]}` + "\n```"}, nil
	}
	svc.SetBibleProvider(func(_ context.Context, _ int64) (string, error) {
		return "", errors.New("快照读取失败")
	})

	runID := startS3Run(t, svc, 802, `{}`)
	book, err := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, err)
	svc.enterAudit(ctx, book, "milestone")
	waitForStageStatus(t, svc, runID, StatusAwaitingGate)
	require.NotContains(t, prompt, "【已冻结卷既定事实", "出错应降级为不注入")
	require.Contains(t, prompt, "signability-audit", "提示词本体照常生成")
}

// TestFixAndRewritePromptsCarryEstablishedFacts 验证 S5 修复提示词与章节重写提示词
// 同样携带既定事实块（S5 前向修复以快照为唯一依据）；空注入时与旧文案一致。
func TestFixAndRewritePromptsCarryEstablishedFacts(t *testing.T) {
	issues := []AuditIssue{{ID: "I1", Level: "P0", Area: "设定自洽", Desc: "冲突"}}

	p := fixPrompt(1, issues, "摘要", contextasm.ContextBlocks{Facts: bibleMarker})
	require.Contains(t, p, "【已冻结卷既定事实（系列圣经快照）】")
	require.Contains(t, p, bibleMarker)
	require.Contains(t, fixPrompt(1, issues, "摘要", contextasm.ContextBlocks{}), "待修复的 issue 清单", "空注入应保持旧文案")

	rp := chapterRewritePrompt(1, RewriteItem{ID: "I2", Level: "P1", Area: "节奏", Desc: "拖沓"},
		chapter.Chapter{ID: 9, ReadingNumber: 3, Title: "第三章"}, contextasm.ContextBlocks{Facts: bibleMarker})
	require.Contains(t, rp, "【已冻结卷既定事实（系列圣经快照）】")
	require.Contains(t, rp, bibleMarker)

	fp := finalAuditPrompt(1, contextasm.ContextBlocks{Facts: bibleMarker})
	require.Contains(t, fp, "【已冻结卷既定事实（系列圣经快照）】")
	require.True(t, strings.HasPrefix(fp, "本书 novel_id=1。"), "注入块应在提示词尾部")
}
