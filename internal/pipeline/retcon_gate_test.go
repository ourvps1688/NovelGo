package pipeline

import (
	"context"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/retcon"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// retconGateOn 建 retcon 闸门并注入 service（与 app 层接线同形）。
func retconGateOn(t *testing.T, svc *Service, db *gorm.DB) *retcon.Service {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&retcon.RetconCase{}))
	gate := retcon.NewService(retcon.NewStore(db))
	svc.SetRetconGate(gate)
	return gate
}

// TestHandoffPersistsRetconCandidates 验证 WBS 3.1 闸门接线：S5 交回人工裁决时
// RetconBacklog 逐项沉淀为持久化 retcon 候选案（origin=pipeline，状态=candidate，
// 三枚举字段随行）；同一 issue 重复沉淀幂等去重；终态后可再立新案。
func TestHandoffPersistsRetconCandidates(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(800)
	gate := retconGateOn(t, svc, db)

	runID := startS3Run(t, svc, novelID, "")
	ctxt := GateContext{RetconBacklog: []RewriteItem{
		{ID: "I2", Level: "P1", Desc: "硬矛盾：需让已死角色复活",
			TargetScope: TargetScopeFrozenVolume, Dimension: "character", Direction: DirectionRetconCandidate},
		{ID: "I3", Level: "P1", Desc: "硬矛盾：需推翻已确立时间框架",
			TargetScope: TargetScopeFrozenVolume, Dimension: "timeline", Direction: DirectionRetconCandidate},
	}}
	svc.handoffRetconToUser(ctx, runID, novelID, ctxt)

	rows, err := gate.ListByNovel(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows, 2, "两项候选都应沉淀")
	byIssue := map[string]retcon.RetconCase{}
	for _, r := range rows {
		byIssue[r.IssueID] = r
	}
	for _, id := range []string{"I2", "I3"} {
		r, ok := byIssue[id]
		require.True(t, ok, "缺少候选 %s", id)
		require.Equal(t, retcon.OriginPipeline, r.Origin)
		require.Equal(t, retcon.StatusCandidate, r.Status)
		require.Equal(t, DirectionRetconCandidate, r.Direction)
		require.Equal(t, TargetScopeFrozenVolume, r.TargetScope)
		require.NotEmpty(t, r.Title, "候选应有标题")
		require.NotEmpty(t, r.Description)
	}

	// 同一 issue 再次交回（体检每轮重算）→ 幂等去重，不堆重复候选。
	svc.handoffRetconToUser(ctx, runID, novelID, ctxt)
	rows2, err := gate.ListByNovel(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows2, 2, "重复沉淀应幂等去重")

	// 驳回后终态，同一 issue 可再立新案（新一轮体检的同号 issue 可能是新问题）。
	_, err = gate.Reject(ctx, byIssue["I2"].ID, "author", "前向适配已消解")
	require.NoError(t, err)
	svc.handoffRetconToUser(ctx, runID, novelID, ctxt)
	rows3, err := gate.ListByNovel(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows3, 3, "终态后同一 issue 应可再立新案")
}

// TestHandoffPersistsStyleCandidateKind 验证 P2 T4.2 kind 路由：文风漂移（V 系）
// 立案带 kind=style（Rollback 只还原 voice 行），常规（I 系）立案缺省 fact。
func TestHandoffPersistsStyleCandidateKind(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(802)
	gate := retconGateOn(t, svc, db)

	runID := startS3Run(t, svc, novelID, "")
	ctxt := GateContext{RetconBacklog: []RewriteItem{
		{ID: "V1", Level: "P1", Desc: "文风漂移：节奏基线偏离",
			TargetScope: TargetScopeFrozenVolume, Dimension: "none", Direction: DirectionRetconCandidate},
		{ID: "I2", Level: "P1", Desc: "硬矛盾：需让已死角色复活",
			TargetScope: TargetScopeFrozenVolume, Dimension: "character", Direction: DirectionRetconCandidate},
	}}
	svc.handoffRetconToUser(ctx, runID, novelID, ctxt)

	rows, err := gate.ListByNovel(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byIssue := map[string]retcon.RetconCase{}
	for _, r := range rows {
		byIssue[r.IssueID] = r
	}
	require.Equal(t, retcon.KindStyle, byIssue["V1"].Kind, "V 系立案应带 style")
	require.Equal(t, retcon.KindFact, byIssue["I2"].Kind, "I 系立案应缺省 fact")
}

// TestHandoffRetconCandidateDegradesWithoutGate 验证降级：未注入 retconGate 时
// 交回照常执行（暂停+广播），不沉淀也不报错。
func TestHandoffRetconCandidateDegradesWithoutGate(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(801)
	require.NoError(t, db.AutoMigrate(&retcon.RetconCase{}))

	runID := startS3Run(t, svc, novelID, "")
	ctxt := GateContext{RetconBacklog: []RewriteItem{
		{ID: "I1", Desc: "硬矛盾", TargetScope: TargetScopeFrozenVolume, Direction: DirectionRetconCandidate},
	}}
	svc.handoffRetconToUser(ctx, runID, novelID, ctxt) // 不应 panic / 不应写库

	var n int64
	require.NoError(t, db.Table("retcon_cases").Count(&n).Error)
	require.Equal(t, int64(0), n, "未注入闸门不应沉淀候选")
}
