package pipeline

// P2 T5.1 双验收（WBS 验收标准本体）的端到端载体：
//   ①漂移 issue 自动进既有修复/重写路由——active 侧：V 系自动分流进 RewriteBacklog
//     且自动派发豁免（只报告不改写，I 系照常重写）；frozen 侧：V 系自动分流进
//     RetconBacklog→交回人工时自动立案（kind=style，Rollback 只还原 voice 行）。
//   ②冻结卷风格可回滚——由 internal/retcon TestStyleRollbackChainOnlyTouchesVoice
//     承载（style retcon 全链 Rollback 后 voice 行回冻结态、事实层不动）。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/retcon"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
)

// TestP2Acceptance_DriftAutoRouteAndExempt 是验收①的 active 侧端到端：
// 体检 V 系（文风漂移）issue 自动分流进 RewriteBacklog（进既有路由，零新路由），
// 自动章节重写只派发非豁免的 I 系——V 系涉及章节零自动改写。
func TestP2Acceptance_DriftAutoRouteAndExempt(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	for i := 1; i <= 5; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: 471, Title: fmt.Sprintf("第%d章", i), SortOrder: i,
		}).Error)
	}
	seeded, err := svc.chapterStore.ListAllByNovel(ctx, nil, 471)
	require.NoError(t, err)
	readingToID := map[int]int64{}
	for _, ch := range seeded {
		readingToID[ch.ReadingNumber] = ch.ID
	}

	issueV := `{"id":"V1","level":"P1","area":"文风漂移·章节#3","desc":"第3章节奏基线偏离","fix_type":"body",` +
		`"target_scope":"active_volume","dimension":"none","direction":"forward_fix"}`
	issueI := `{"id":"I3","level":"P0","area":"开篇过签力","desc":"第4章未立人设","fix_type":"body"}`
	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"验收场","issues":[` + issueV + `,` + issueI + `]}`

	var rewrittenIDs []int64
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "chapter-rewrite":
			rewrittenIDs = append(rewrittenIDs, extractChapterIDFromPrompt(t, input.Prompt))
			return SkillOutput{Text: "已整章重写"}, nil
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n" +
				`{"verdict":"needs_fix","score":16,"summary":"复评仍报","issues":[` + issueV + `,` + issueI + `]}` +
				"\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	oldProg := emitRewriteProgress
	emitRewriteProgress = func(_ context.Context, _ map[string]any) {}
	defer func() { emitRewriteProgress = oldProg }()

	run, err := svc.StartPipeline(ctx, 471, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	// 自动进路由：V 系与 I 系都沉淀进 RewriteBacklog（V 系供作者裁决）。
	var backlogIDs []string
	for _, it := range finalCtxt.RewriteBacklog {
		backlogIDs = append(backlogIDs, it.ID)
	}
	require.ElementsMatch(t, []string{"V1", "I3"}, backlogIDs, "V 系应自动分流进 RewriteBacklog")
	// 派发豁免：只重写 I 系指向的第 4 章，V 系第 3 章零自动改写（只报告不改写）。
	require.Equal(t, []int64{readingToID[4]}, rewrittenIDs, "V 系章节不得被自动重写")
}

// TestP2Acceptance_DriftRetconFiling 是验收①的 frozen 侧端到端：
// 命中冻结卷写保护的 V 系 issue 自动分流进 RetconBacklog，交回人工裁决时自动
// 沉淀为 retcon 候选案（kind=style：Rollback 走 RestoreStyle 只还原 voice 行）。
func TestP2Acceptance_DriftRetconFiling(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(472)
	gate := retconGateOn(t, svc, db)

	runID := startS3Run(t, svc, novelID, "")
	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"验收场","issues":[` +
		`{"id":"V1","level":"P1","area":"文风漂移·章节#7","desc":"冻结卷第7章文风指标偏离基线","fix_type":"body",` +
		`"target_scope":"frozen_volume","dimension":"none","direction":"retcon_candidate"}]}`

	var fixDonePayload map[string]any
	oldEmit := emitFixDone
	emitFixDone = func(_ context.Context, p map[string]any) { fixDonePayload = p }
	defer func() { emitFixDone = oldEmit }()

	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, runID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, runID, "fix", ""))
	waitForStageStatus(t, svc, runID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, runID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Len(t, finalCtxt.RetconBacklog, 1, "frozen V 系应自动分流进 RetconBacklog")
	require.Equal(t, "V1", finalCtxt.RetconBacklog[0].ID)

	// 自动立案：kind=style（风格回改，Rollback 只还原 voice 行）。
	rows, err := gate.ListByNovel(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "V 系应自动沉淀为 retcon 候选案")
	require.Equal(t, retcon.KindStyle, rows[0].Kind, "V 系立案应带 style")
	require.Equal(t, retcon.OriginPipeline, rows[0].Origin)
	require.Equal(t, retcon.StatusCandidate, rows[0].Status)

	require.NotNil(t, fixDonePayload)
	require.Equal(t, true, fixDonePayload["retcon_handoff"], "fix_done 应带 retcon_handoff（前端零改动）")
}
