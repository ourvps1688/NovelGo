package mcp_tools

// provenance_tools_test.go —— 台账 provenance 工具（R2b-7）接线闭环：
// 标注/局部覆盖/查看、契约拒收、跨书隔离、注册。

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProvenanceToolsRoundTrip provenance 工具闭环：标注→局部覆盖（nil=不改）→
// 查看；契约拒收（定位缺失/置信非法）；跨书隔离。
func TestProvenanceToolsRoundTrip(t *testing.T) {
	db := openPlanCheckToolDB(t)
	tc := planCheckTc(db)

	src := int64(7)
	res, err := (&UpsertLedgerProvenanceTool{}).Execute(context.Background(), &UpsertLedgerProvenanceArgs{
		EntryTable: "characters", EntryID: 3, SourceChapterID: &src, Confidence: strPtr("medium"),
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, "characters", res.Data["entry_table"])
	require.Equal(t, int64(7), res.Data["source_chapter_id"])

	// 局部覆盖：只标 confirmed，来源/置信保持现值。
	yes := true
	res, err = (&UpsertLedgerProvenanceTool{}).Execute(context.Background(), &UpsertLedgerProvenanceArgs{
		EntryTable: "characters", EntryID: 3, Confirmed: &yes,
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, int64(7), res.Data["source_chapter_id"], "未给字段保持现值")
	require.Equal(t, "medium", res.Data["confidence"], "未给字段保持现值")
	require.Equal(t, true, res.Data["confirmed"])
	require.Equal(t, false, res.Data["pinned"])

	// 查看清单。
	res, err = (&ListLedgerProvenanceTool{}).Execute(context.Background(), &ListLedgerProvenanceArgs{}, tc)
	require.NoError(t, err)
	rows, _ := res.Data["provenance"].([]map[string]any)
	require.Len(t, rows, 1)
	require.Equal(t, "characters", rows[0]["entry_table"])

	// 契约拒收：定位缺失 / 置信非法（业务拒绝 Success=false，不计入中断）。
	bad, err := (&UpsertLedgerProvenanceTool{}).Execute(context.Background(), &UpsertLedgerProvenanceArgs{
		EntryTable: "characters", EntryID: 0,
	}, tc)
	require.NoError(t, err)
	require.False(t, bad.Success)
	bad, err = (&UpsertLedgerProvenanceTool{}).Execute(context.Background(), &UpsertLedgerProvenanceArgs{
		EntryTable: "characters", EntryID: 3, Confidence: strPtr("sure"),
	}, tc)
	require.NoError(t, err)
	require.False(t, bad.Success)

	// 拒收后复核：置信仍是 medium。
	res, err = (&ListLedgerProvenanceTool{}).Execute(context.Background(), &ListLedgerProvenanceArgs{}, tc)
	require.NoError(t, err)
	rows, _ = res.Data["provenance"].([]map[string]any)
	require.Equal(t, "medium", rows[0]["confidence"], "契约拒收不应落库")

	// 跨书隔离。
	other := ToolContext{DB: db, NovelID: 2, Logger: tc.Logger}
	res, err = (&ListLedgerProvenanceTool{}).Execute(context.Background(), &ListLedgerProvenanceArgs{}, other)
	require.NoError(t, err)
	rows, _ = res.Data["provenance"].([]map[string]any)
	require.Empty(t, rows)
}

// TestProvenanceToolsRegistered 双工具注册进注册表（主 Agent 可派发）。
func TestProvenanceToolsRegistered(t *testing.T) {
	r := NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterProvenanceTools(r)
	_, ok := r.Get("upsert_ledger_provenance")
	require.True(t, ok)
	_, ok = r.Get("list_ledger_provenance")
	require.True(t, ok)
}

// strPtr 返回字符串指针（夹具用）。
func strPtr(s string) *string { return &s }
