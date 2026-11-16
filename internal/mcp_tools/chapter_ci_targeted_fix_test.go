//go:build cgo

package mcp_tools_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/quality"
)

// 定向修复接线锁：上轮目标必须在作废前读出、锚点复验必须在写后 L0 链路里、
// 档案视图必须走定向处方。Windows CRLF 归一后再断言（既有纪律）。

func readTargetedFixSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	require.NoError(t, err, "读取接线源文件 %s", rel)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestTargetedFixWiringStatic(t *testing.T) {
	h := readTargetedFixSource(t, "../../app/handler.go")
	iPrev := strings.Index(h, "prevTargets := mcp_tools.PreviousL1Targets")
	iRecalc := strings.Index(h, "mcp_tools.RecalcChapterL0FromDisk")
	require.GreaterOrEqual(t, iPrev, 0, "handler 必须读取上一轮 L1 目标")
	require.GreaterOrEqual(t, iRecalc, 0)
	assert.Less(t, iPrev, iRecalc,
		"上轮目标必须在 L0 重算作废旧 L1 之前读取，否则每轮仍是开放式全量重评审")
	assert.Contains(t, h, "prevTargets...)", "上轮目标必须真正传给 L1 执行体")

	ci := readTargetedFixSource(t, "chapter_ci.go")
	for _, want := range []string{
		"buildChapterCIInstructionWithTargets(", "PreviousL1Targets(",
		"anchorReportForChapter(ctx, qs", "SetAnchorReport(", "quality.WithAnchors(",
		"上轮待核验目标", "回执格式不变",
	} {
		assert.Contains(t, ci, want, "chapter_ci.go 缺少定向修复接线 %q", want)
	}
	ds := readTargetedFixSource(t, "../pipeline/detect_status.go")
	assert.Contains(t, ds, "BuildPrescriptionsAnchored(", "档案视图必须用定向处方")
	assert.Contains(t, ds, "out.AnchorReport = round.AnchorReport", "本地复验结论必须透出")

	// 零新增自动修复：定向修复依旧只把上下文交给 chat，不派发后台执行体。
	ciAll := ci + ds
	for _, banned := range []string{"StartIssueFixTickets", "autoFix", "FixRounds"} {
		assert.NotContains(t, ciAll, banned, "定向修复不得复活后台自动修复：%s", banned)
	}
}

func TestPreviousL1TargetsReadsSnapshotBeforeInvalidation(t *testing.T) {
	db, _, _ := setupCIEnv(t)
	ctx := context.Background()
	qs := quality.NewStore(db, ciLogger())
	require.NoError(t, qs.UpsertL1(ctx, &quality.ChapterQuality{
		NovelID: 1, ChapterID: 42,
		L1Status: quality.L1StatusChecked,
		L1Findings: quality.MarshalFindings([]quality.Finding{
			{ID: "L1-1", Check: "continuity", Desc: "台阶数不一致", Anchor: "台阶一共七级，他数着下"},
			{ID: "L1-2", Check: "task_card", Desc: "任务卡未兑现", LedgerRefs: []string{"1068"}},
		}),
	}))

	got := mcp_tools.PreviousL1Targets(ctx, db, ciLogger(), 1, 42)
	require.Len(t, got, 2)
	assert.Contains(t, got[0], "[continuity]")
	assert.Contains(t, got[0], "上轮锚点原句「台阶一共七级，他数着下」")
	assert.Contains(t, got[1], "[task_card]")
	assert.NotContains(t, got[1], "上轮锚点原句", "无锚点的老条目不臆造")

	// 零发现/无档案返回 nil ⇒ 指令逐字回落改动前的旧路径。
	assert.Nil(t, mcp_tools.PreviousL1Targets(ctx, db, ciLogger(), 1, 999))
}
