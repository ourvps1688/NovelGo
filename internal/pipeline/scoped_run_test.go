package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedScopedFixtures 迁移 chapters/volumes 表并种入两卷各两章，返回两卷 id。
// 最新写就章节（id 最大）落在第二卷，即当前卷=v2。
func seedScopedFixtures(t *testing.T, db *gorm.DB, novelID int64) (int64, int64) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	v1 := &volume.Volume{NovelID: novelID, Name: "第一卷", SortOrder: 1}
	v2 := &volume.Volume{NovelID: novelID, Name: "第二卷", SortOrder: 2}
	require.NoError(t, db.Create(v1).Error)
	require.NoError(t, db.Create(v2).Error)
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: novelID, Title: fmt.Sprintf("v1-%d", i), SortOrder: i, VolumeID: &v1.ID,
		}).Error)
	}
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: novelID, Title: fmt.Sprintf("v2-%d", i), SortOrder: i, VolumeID: &v2.ID,
		}).Error)
	}
	return v1.ID, v2.ID
}

// TestServiceVolumeScopedAuditDelegates 验证 G1 卷级作用域运行模型：
// book-run 的卷级体检（golden3/milestone/volume）委托给「当前卷」的 scoped sub-run，
// S4 只扫当前卷；G4 选 continue 后 scoped 收口，体检结论回写并唤醒 book-run 回 S3。
func TestServiceVolumeScopedAuditDelegates(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(500)
	_, v2 := seedScopedFixtures(t, db, novelID)

	var auditPrompt string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			auditPrompt = input.Prompt
			return SkillOutput{Text: "结论：\n```json\n" +
				`{"verdict":"pass","score":31,"summary":"能签","issues":[]}` + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	run, err := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS3, StatusRunning, run.ContextJSON))

	// 触发卷级体检（golden3 首检入口）。
	book, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	svc.enterAudit(ctx, book, "golden3")

	// scoped sub-run 应已派生：定界在当前卷（第二卷），挂 book-run。
	scoped, err := svc.store.GetActiveVolumeRun(ctx, novelID, v2)
	require.NoError(t, err, "应派生当前卷的 scoped sub-run")
	require.Equal(t, run.RunID, scoped.ParentRunID)
	require.NotNil(t, scoped.VolumeID)
	require.Equal(t, v2, *scoped.VolumeID)
	require.Equal(t, StageS4, scoped.CurrentStage)

	// book-run 转入等待（阶段保持 S3）。
	book, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StatusWaiting, book.Status)
	require.Equal(t, StageS3, book.CurrentStage)

	// S4 审计范围=当前卷：提示词应带卷级作用域说明。
	waitForStageStatus(t, svc, scoped.RunID, StatusAwaitingGate)
	require.Contains(t, auditPrompt, fmt.Sprintf("volume_id=%d", v2), "审计提示词应限定当前卷")
	require.Contains(t, auditPrompt, "只审属于该卷的章节")

	// G4 选 continue：scoped 收口，book-run 唤醒回 S3 且结论回写。
	require.NoError(t, svc.ResumePipeline(ctx, scoped.RunID, "continue", ""))
	book, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS3, book.CurrentStage)
	require.Equal(t, StatusRunning, book.Status)
	var bc GateContext
	require.NoError(t, json.Unmarshal([]byte(book.ContextJSON), &bc))
	require.True(t, bc.S4Audited, "收口后 book-run 应标记已体检")
	require.Contains(t, bc.Audit, "pass", "体检结论应回写 book-run")

	scopedAfter, _ := svc.store.GetByRunID(ctx, scoped.RunID)
	require.Equal(t, StatusFinished, scopedAfter.Status)

	// GetByNovel 切回 book-run（scoped 已收口，不再顶替）。
	cur, err := svc.GetPipelineState(ctx, novelID)
	require.NoError(t, err)
	require.Equal(t, run.RunID, cur.RunID)
}

// TestServiceVolumeScopedAbortPropagates 验证终止联动（G1）：
// 终止卷级 scoped sub-run → 父 book-run 一并终止，不会永久停在 waiting。
func TestServiceVolumeScopedAbortPropagates(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(501)
	_, v2 := seedScopedFixtures(t, db, novelID)
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "结论：\n```json\n{\"verdict\":\"pass\",\"score\":30,\"summary\":\"ok\",\"issues\":[]}\n```"}, nil
	}

	run, err := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS3, StatusRunning, run.ContextJSON))
	book, _ := svc.store.GetByRunID(ctx, run.RunID)
	svc.enterAudit(ctx, book, "milestone")

	scoped, err := svc.store.GetActiveVolumeRun(ctx, novelID, v2)
	require.NoError(t, err)
	waitForStageStatus(t, svc, scoped.RunID, StatusAwaitingGate)

	// 终止 scoped run（G4 abort）。
	require.NoError(t, svc.ResumePipeline(ctx, scoped.RunID, "abort", ""))
	book, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StatusAborted, book.Status, "scoped 终止应联动终止父 book-run")
}

// TestServiceBookRunStopAbortsScopedChildren 验证反向联动：
// 终止 book-run → 其活跃卷级 scoped sub-run 一并终止，不留孤儿。
func TestServiceBookRunStopAbortsScopedChildren(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(502)
	_, v2 := seedScopedFixtures(t, db, novelID)
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "结论：\n```json\n{\"verdict\":\"pass\",\"score\":30,\"summary\":\"ok\",\"issues\":[]}\n```"}, nil
	}

	run, err := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS3, StatusRunning, run.ContextJSON))
	book, _ := svc.store.GetByRunID(ctx, run.RunID)
	svc.enterAudit(ctx, book, "milestone")
	scoped, err := svc.store.GetActiveVolumeRun(ctx, novelID, v2)
	require.NoError(t, err)
	waitForStageStatus(t, svc, scoped.RunID, StatusAwaitingGate)

	// 直接终止 book-run（控制条 stop）。
	require.NoError(t, svc.ControlPipeline(ctx, run.RunID, "stop"))
	scopedAfter, _ := svc.store.GetByRunID(ctx, scoped.RunID)
	require.Equal(t, StatusAborted, scopedAfter.Status, "book-run 终止应联动终止活跃 scoped run")
}

// TestServiceUnvolumedBookAuditLegacyPath 验证向后兼容：未分卷书（章节无归属卷）不派生
// scoped run，体检仍走 book-run 全书路径，行为与旧版一致。
func TestServiceUnvolumedBookAuditLegacyPath(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(503)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	for i := 1; i <= 3; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: novelID, Title: fmt.Sprintf("第%d章", i), SortOrder: i,
		}).Error)
	}
	var auditPrompt string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			auditPrompt = input.Prompt
			return SkillOutput{Text: "结论：\n```json\n{\"verdict\":\"pass\",\"score\":30,\"summary\":\"ok\",\"issues\":[]}\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	run, err := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS3, StatusRunning, run.ContextJSON))
	book, _ := svc.store.GetByRunID(ctx, run.RunID)
	svc.enterAudit(ctx, book, "golden3")

	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS4, got.CurrentStage, "未分卷书应在 book-run 上直接体检")
	require.Empty(t, got.ParentRunID)
	require.Nil(t, got.VolumeID)
	require.NotContains(t, auditPrompt, "本次体检作用域", "全书路径提示词不应带卷级作用域")

	children, err := svc.store.ListActiveScopedChildren(ctx, run.RunID)
	require.NoError(t, err)
	require.Empty(t, children, "未分卷书不应派生 scoped run")
}

// TestServiceRecoverWakesOrphanWaitingBookRun 验证启动恢复自愈：waiting 的 book-run 若其
// 卷级子 run 已全部收口（崩溃发生在收口与唤醒之间），重启后应被唤醒回 S3 写作。
func TestServiceRecoverWakesOrphanWaitingBookRun(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	run, err := svc.StartPipeline(ctx, 504, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS3, StatusWaiting, run.ContextJSON))

	svc.RecoverInterruptedRuns()

	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Equal(t, StatusRunning, got.Status, "孤儿 waiting book-run 应被唤醒")
	require.Equal(t, StageS3, got.CurrentStage)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.True(t, ctxt.S4Audited, "自愈唤醒应置已体检，避免重复首检")

	// 有活跃子 run 的 waiting book-run 不应被误唤醒。
	run2, err := svc.StartPipeline(ctx, 505, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run2.RunID, StageS3, StatusWaiting, run2.ContextJSON))
	child := &PipelineRun{NovelID: 505, RunID: "scoped-505", Mode: "new",
		CurrentStage: StageS4, Status: StatusRunning, ParentRunID: run2.RunID}
	require.NoError(t, svc.store.Create(ctx, child))
	svc.RecoverInterruptedRuns()
	got2, _ := svc.store.GetByRunID(ctx, run2.RunID)
	require.Equal(t, StatusWaiting, got2.Status, "仍有活跃子 run 的 book-run 不应被唤醒")
}

// TestSignabilityPromptScopeText 验证卷级作用域提示词：空作用域与旧文案一致，非空时含定界句。
func TestSignabilityPromptScopeText(t *testing.T) {
	legacy := signabilityPrompt(7, "", "")
	require.True(t, strings.HasPrefix(legacy, "本书 novel_id=7。请基于 signability-audit"), "空作用域应保持旧文案")
	scoped := signabilityPrompt(7, "本次体检作用域=《第二卷》（volume_id=2）：只审；", "")
	require.Contains(t, scoped, "本次体检作用域")
	require.Contains(t, scoped, "请基于 signability-audit")
}
