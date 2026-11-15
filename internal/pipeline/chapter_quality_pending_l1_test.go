package pipeline

// #130 契约：结论条要区分「整轮检测完成」与「仅 L0 落档、L1 沿用上一轮」。
// 单列 checked_at 由 UpsertL0 / UpsertL1 共同刷新，不能冒充整轮完成；
// 本轮 L1 是否收口必须透出 finding_round.pending_l1 给前端。

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/novelgo/novelgo/internal/quality"
)

func TestChapterQualityArchivePendingL1Wiring(t *testing.T) {
	src := readChapterDetectSource(t, "detect_status.go")
	assert.Contains(t, src, "PendingL1", "档案视图必须透出本轮 L1 未收口标记")
	assert.Contains(t, src, `json:"pending_l1"`, "该字段须按前端 snake_case 口径序列化")
	assert.Contains(t, src, "out.PendingL1 = round.PendingL1",
		"pending_l1 必须来自跨轮快照，不得另造口径")

	models := readChapterDetectSource(t, "../../frontend/src/lib/wailsjs/go/models.ts")
	assert.Contains(t, models, "pending_l1: boolean;", "Wails 生成物须有该字段")
	assert.Contains(t, models, `this.pending_l1 = source["pending_l1"];`,
		"Wails 生成物须真正反序列化该字段")
}

func TestPendingL1TracksL1ReceiptNotL1Status(t *testing.T) {
	// 一次「重新检测本章」：L0 先落档（PendingL1=true），L1 回执落档才收口。
	opened := quality.NextFindingRound(nil, "l0", []quality.Finding{{Check: quality.CheckWordCount}})
	assert.True(t, opened.PendingL1, "L0 落档时本轮 L1 尚未完成")

	// #115：L1 超时只保留上一轮 L1，不写 UpsertL1 → pending_l1 保持 true，
	// 而 l1_status 可能仍是上一轮的 checked。结论条必须据此判为「未完成」。
	old := &quality.ChapterQuality{L1Status: quality.L1StatusChecked, FindingRound: quality.MarshalFindingRound(opened)}
	assert.True(t, quality.ParseFindingRound(old.FindingRound).PendingL1,
		"L1 超时保留上一轮结果时，本轮仍须标记为未完成")

	// L1 回执真正落档才清标记。
	closed := quality.NextFindingRound(old, "l1", nil)
	assert.False(t, closed.PendingL1, "L1 收口后本轮才算完成")
}
