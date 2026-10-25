package pipeline

// R2b-1 结构告警块接线测试：
//   - 伏笔 lifecycle 告警进 L2 体检输入与档案汇总快审输入（与 R2-4 汇总块同装配通道）；
//   - 全口径注入（milestone/volume/final 同路，§8.5 结构索引全量），蓝图不涉；
//   - 零告警/取数失败 → 空块零提示（提示词与无该块逐字一致）。

import (
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedForeshadowData 建 time_entries/chapters 表并植入：2 章进度（now=2）+ 四条伏笔——
// 超期未收（target=1）、无收点规划（target=0）、在轨干净、收未埋（resolved 无源）。
func seedForeshadowData(t *testing.T, db *gorm.DB, novelID int64) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&timeline.TimelineEntry{}, &chapter.Chapter{}))
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{NovelID: novelID, Title: "章", SortOrder: i}).Error)
	}
	entries := []timeline.TimelineEntry{
		{NovelID: novelID, Category: "foreshadowing", Status: "pending", Title: "黑衣人身份", TargetReadingNumber: 1},
		{NovelID: novelID, Category: "foreshadowing", Status: "pending", Title: "母亲死因", TargetReadingNumber: 0},
		{NovelID: novelID, Category: "foreshadowing", Status: "pending", Title: "在轨伏笔", TargetReadingNumber: 30},
		{NovelID: novelID, Category: "foreshadowing", Status: "resolved", Title: "三年之约", TargetReadingNumber: 2},
		{NovelID: novelID, Category: "user_directive", Status: "pending", Title: "用户指令", TargetReadingNumber: 0},
	}
	for i := range entries {
		require.NoError(t, db.Create(&entries[i]).Error)
	}
}

// TestStructureBlockInFullAndQuickInputs 主路径：milestone + 有档案 + 有告警 →
// 结构告警块进全量输入（汇总块之后）且同路进快审输入；user_directive 不参与检查。
func TestStructureBlockInFullAndQuickInputs(t *testing.T) {
	rec := &dualRecorder{quickText: dualQuickPass}
	svc, runID := newDualTest(t, 811, "milestone", true, rec)
	seedForeshadowData(t, svc.store.db, 811)
	svc.executeAudit(runID, 811)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Contains(t, rec.fullPrompt, "【结构告警】")
	require.Contains(t, rec.fullPrompt, "S1[foreshadow_overdue] 伏笔「黑衣人身份」预期第 1 章前回收，当前进度第 2 章仍待收")
	require.Contains(t, rec.fullPrompt, "S2[foreshadow_no_target] 伏笔「母亲死因」待收但未规划回收位置")
	require.Contains(t, rec.fullPrompt, "S3[foreshadow_unsourced] 伏笔「三年之约」已标记回收但无埋设章记录")
	require.NotContains(t, rec.fullPrompt, "用户指令", "非伏笔分类不进结构检查")
	// 汇总块（R2-4）在前、结构告警块（R2b-1）在后，同路装配。
	require.Less(t, strings.Index(rec.fullPrompt, "【质量档案汇总】"), strings.Index(rec.fullPrompt, "【结构告警】"))
	// 同路进快审输入。
	require.Equal(t, 1, rec.quickCalls)
	require.Contains(t, rec.quickPrompt, "【结构告警】")
	require.Contains(t, rec.quickPrompt, "S1[foreshadow_overdue]")
}

// TestStructureBlockAllAuditReasons 非 milestone 口径（golden3/volume/final）同样注入
// （§8.5 结构索引全量）；无档案不起快审，但结构块照进全量输入。
func TestStructureBlockAllAuditReasons(t *testing.T) {
	for _, reason := range []string{"golden3", "volume", "final"} {
		rec := &dualRecorder{quickText: dualQuickPass}
		svc, runID := newDualTest(t, 812, reason, false, rec)
		seedForeshadowData(t, svc.store.db, 812)
		svc.executeAudit(runID, 812)

		rec.mu.Lock()
		require.Contains(t, rec.fullPrompt, "【结构告警】", reason+" 应注入结构告警块")
		require.NotContains(t, rec.fullPrompt, "【质量档案汇总】", reason+" 不注入档案汇总块（milestone 专轨）")
		require.Equal(t, 0, rec.quickCalls, reason+" 无档案不起快审轨")
		rec.mu.Unlock()
	}
}

// TestNoStructFindingsZeroPrompt 零告警 → 空块零提示：提示词与无该块逐字一致。
func TestNoStructFindingsZeroPrompt(t *testing.T) {
	rec := &dualRecorder{quickText: dualQuickPass}
	svc, runID := newDualTest(t, 813, "milestone", false, rec)
	require.NoError(t, svc.store.db.AutoMigrate(&timeline.TimelineEntry{}, &chapter.Chapter{}))
	// 只有干净伏笔：在轨 pending（未到预期收章）+ 有源 resolved。
	ch := chapter.Chapter{NovelID: 813, Title: "章", SortOrder: 1}
	require.NoError(t, svc.store.db.Create(&ch).Error)
	require.NoError(t, svc.store.db.Create(&timeline.TimelineEntry{NovelID: 813, Category: "foreshadowing",
		Status: "pending", Title: "在轨", TargetReadingNumber: 9}).Error)
	require.NoError(t, svc.store.db.Create(&timeline.TimelineEntry{NovelID: 813, Category: "foreshadowing",
		Status: "resolved", Title: "已收", TargetReadingNumber: 1, SourceChapterID: &ch.ID}).Error)
	svc.executeAudit(runID, 813)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.NotContains(t, rec.fullPrompt, "【结构告警】")
	require.Equal(t, signabilityPrompt(813, "", "", "", contextasm.ContextBlocks{}), rec.fullPrompt,
		"零告警时提示词应与无该块逐字一致")
}

// TestStructureBlockFetchFailSilent 取数失败（表缺失）→ 静默空块，提示词逐字一致、
// 主流程零感知（降级零提示零报错）。
func TestStructureBlockFetchFailSilent(t *testing.T) {
	rec := &dualRecorder{quickText: dualQuickPass}
	svc, runID := newDualTest(t, 814, "volume", false, rec)
	// 不建 time_entries 表：读取必失败。
	svc.executeAudit(runID, 814)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.NotContains(t, rec.fullPrompt, "【结构告警】")
	require.Equal(t, signabilityPrompt(814, "", "", "", contextasm.ContextBlocks{}), rec.fullPrompt,
		"取数失败时提示词应与无该块逐字一致")
	run, err := svc.store.GetByRunID(t.Context(), runID)
	require.NoError(t, err)
	require.Equal(t, StatusAwaitingGate, run.Status, "结构块取数失败不得影响体检落闸")
}
