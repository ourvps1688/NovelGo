package pipeline

// tension_run_test.go —— 情绪账曲线发现工单（P0-2）：E 系投影 / 按章前缀整批替换 /
// 落单回填语义（零发现=清该章旧工单、无容器零发现=不建容器）/ dim4 摘要块注入。

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/quality"
)

func tensionFinding(check string) quality.Finding {
	return quality.Finding{
		ID: "C1", Check: check, Level: quality.FindingLevelDefault,
		Desc: check + " 描述", Status: quality.FindingStatusPending,
	}
}

// TestTensionIssuesToAudit E 系投影：只收曲线四查、键 E{rn}-{seq} 顺编、
// 修复路由字段固定（body/none/none/forward_fix）+ 章作用域。
func TestTensionIssuesToAudit(t *testing.T) {
	findings := []quality.Finding{
		tensionFinding(quality.CheckPacingDrag),
		tensionFinding(quality.CheckWordCount), // 非四查：不出 E 系工单
		tensionFinding(quality.CheckBeatGap),
	}
	got := TensionIssuesToAudit(5, findings)
	require.Len(t, got, 2)
	assert.Equal(t, "E5-1", got[0].ID)
	assert.Equal(t, "E5-2", got[1].ID)
	assert.Equal(t, "拖沓段", got[0].Area)
	assert.Equal(t, "爽点断档", got[1].Area)
	assert.Equal(t, FixTypeBody, got[0].FixType)
	assert.Equal(t, TargetScopeNone, got[0].TargetScope)
	assert.Equal(t, DimensionNone, got[0].Dimension)
	assert.Equal(t, DirectionForwardFix, got[0].Direction)
	assert.Equal(t, []int64{5}, got[0].ChapterIDs)
	assert.Empty(t, TensionIssuesToAudit(5, nil))
}

// TestLandTensionTicketsReplaceByChapter 落单/跨章累积/同章整批替换/零发现清本章旧单，
// 其它章工单不受影响；每书一个容器 ensure 复用。
func TestLandTensionTicketsReplaceByChapter(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	s := NewStore(db)

	// 首落：建容器 + 2 条（E3-*）。
	n, err := LandTensionTickets(ctx, db, 7, 3, []quality.Finding{
		tensionFinding(quality.CheckPacingDrag), tensionFinding(quality.CheckBeatGap)})
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	run, err := s.GetDetectRun(ctx, 7, TensionKind, nil)
	require.NoError(t, err)
	assert.Equal(t, detectModeOf(TensionKind), run.Mode)
	assert.Equal(t, StatusFinished, run.Status)
	rows, err := s.ListIssues(ctx, 7, run.RunID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "E3-1", rows[0].IssueKey)
	assert.Equal(t, IssueStatusPending, rows[0].Status)
	assert.Equal(t, "[3]", rows[0].ChapterIDsJSON)

	// 另一章落单（E4-*）：跨章累积。
	_, err = LandTensionTickets(ctx, db, 7, 4, []quality.Finding{tensionFinding(quality.CheckOpeningSlope)})
	require.NoError(t, err)
	rows, _ = s.ListIssues(ctx, 7, run.RunID)
	assert.Len(t, rows, 3)

	// 同章重落（新结果 1 条）：整批替换只动本章前缀。
	_, err = LandTensionTickets(ctx, db, 7, 3, []quality.Finding{tensionFinding(quality.CheckBeatDensity)})
	require.NoError(t, err)
	rows, _ = s.ListIssues(ctx, 7, run.RunID)
	require.Len(t, rows, 2)
	keys := []string{rows[0].IssueKey, rows[1].IssueKey}
	assert.ElementsMatch(t, []string{"E3-1", "E4-1"}, keys)

	// 同章零发现：清本章旧工单（回填语义），其它章保留。
	_, err = LandTensionTickets(ctx, db, 7, 3, nil)
	require.NoError(t, err)
	rows, _ = s.ListIssues(ctx, 7, run.RunID)
	require.Len(t, rows, 1)
	assert.Equal(t, "E4-1", rows[0].IssueKey)

	// 每书一个容器 ensure 复用（重落不再新建）。
	run2, err := s.GetDetectRun(ctx, 7, TensionKind, nil)
	require.NoError(t, err)
	assert.Equal(t, run.RunID, run2.RunID)
}

// TestLandTensionTicketsNoContainerNoFindings 无容器且零发现=不建容器；
// 有容器零发现=清旧单但容器保留；novelID/阅读序号缺失=跳过。
func TestLandTensionTicketsNoContainerNoFindings(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	s := NewStore(db)

	n, err := LandTensionTickets(ctx, db, 9, 1, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, err = s.GetDetectRun(ctx, 9, TensionKind, nil)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "无容器且零发现不得建容器")

	// 落 1 条建容器后零发现 → 清旧单、容器保留。
	_, err = LandTensionTickets(ctx, db, 9, 1, []quality.Finding{tensionFinding(quality.CheckPacingDrag)})
	require.NoError(t, err)
	n, err = LandTensionTickets(ctx, db, 9, 1, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	run, err := s.GetDetectRun(ctx, 9, TensionKind, nil)
	require.NoError(t, err)
	rows, err := s.ListIssues(ctx, 9, run.RunID)
	require.NoError(t, err)
	assert.Empty(t, rows)

	// 作用域缺失跳过（宁缺勿错）。
	n, err = LandTensionTickets(ctx, db, 0, 1, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	n, err = LandTensionTickets(ctx, db, 9, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// TestReplaceIssuesByPrefixEmptyPrefixRejected 空前缀显式报错（防误用成整 run 清除）。
func TestReplaceIssuesByPrefixEmptyPrefixRejected(t *testing.T) {
	db := newTestDB(t)
	err := NewStore(db).ReplaceIssuesByPrefix(context.Background(), 1, "r1", "", 0, nil)
	assert.Error(t, err)
}

// TestSignabilityPromptTensionBlock dim4 摘要块注入：structBlock 之后渲染、空块零 diff。
func TestSignabilityPromptTensionBlock(t *testing.T) {
	base := signabilityPrompt(1, auditInput{}, contextasm.ContextBlocks{})
	empty := signabilityPrompt(1, auditInput{tension: ""}, contextasm.ContextBlocks{})
	require.Equal(t, base, empty, "空块=提示词零 diff")

	withT := signabilityPrompt(1, auditInput{tension: "【情绪曲线摘要】x"}, contextasm.ContextBlocks{})
	assert.Contains(t, withT, "【情绪曲线摘要】x")

	both := signabilityPrompt(1, auditInput{structBlock: "【结构告警】y", tension: "【情绪曲线摘要】x"}, contextasm.ContextBlocks{})
	assert.Greater(t, strings.Index(both, "【情绪曲线摘要】x"), strings.Index(both, "【结构告警】y"),
		"tension 块在 structBlock 之后渲染")
}
