package quality

// tension_block_test.go —— 「情绪曲线摘要」块（P0-2 dim4 证据）：按章一行渲染/
// L1 优先合并/体量封顶/空清单零提示。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockRow 构造一行 ChapterTension（测试夹具）。
func blockRow(chapterID int64, reading int, source string, seg int, tension, emotion float64, beat string) ChapterTension {
	return ChapterTension{
		NovelID: 1, ChapterID: chapterID, Source: source, Seg: seg,
		ReadingNumber: reading, StartRune: seg * 400, EndRune: seg*400 + 390,
		Emotion: emotion, Tension: tension, BeatType: beat,
	}
}

// TestRenderTensionBlockChapterLine 按章一行：tension 值串/情绪峰/拖沓/开篇坡度齐备。
func TestRenderTensionBlockChapterLine(t *testing.T) {
	// 章 3：4 段——seg 0-2 平坦（拖沓 3 连）、seg 3 climax；开篇窗口（seg 0-1）升幅 +2（不足）。
	rows := []ChapterTension{
		blockRow(30, 3, TensionSourceL0, 0, 10, 8, BeatTypeFlat),
		blockRow(30, 3, TensionSourceL0, 1, 12, 8, BeatTypeFlat),
		blockRow(30, 3, TensionSourceL0, 2, 11, 8, BeatTypeFlat),
		blockRow(30, 3, TensionSourceL0, 3, 80, 90, BeatTypeClimax),
	}
	out := RenderTensionBlock(rows)
	require.Contains(t, out, "【情绪曲线摘要】")
	require.Contains(t, out, "- #3｜")
	assert.Contains(t, out, "tension 10/12/11/80（4 段）")
	assert.Contains(t, out, "情绪峰 seg 3")
	assert.Contains(t, out, "拖沓 seg 0–2")
	assert.Contains(t, out, "开篇坡度 +2.0（不足）")
}

// TestRenderTensionBlockL1Priority 同段 L1 优先合并（EffectiveTensionCurve 口径）。
func TestRenderTensionBlockL1Priority(t *testing.T) {
	rows := []ChapterTension{
		blockRow(30, 3, TensionSourceL0, 0, 10, 8, BeatTypeFlat),
		blockRow(30, 3, TensionSourceL1, 0, 55, 75, BeatTypeClimax),
	}
	out := RenderTensionBlock(rows)
	assert.Contains(t, out, "tension 55（1 段）", "同段取 L1")
	assert.Contains(t, out, "情绪峰 seg 0", "L1 标注的 climax 生效")
}

// TestRenderTensionBlockCaps 体量封顶：每章 16 段、全书 120 章，截断如实标注。
func TestRenderTensionBlockCaps(t *testing.T) {
	// 20 段单章 → 值串列 16 段 + "…"，总段数照报。
	var rows []ChapterTension
	for i := 0; i < 20; i++ {
		rows = append(rows, blockRow(30, 1, TensionSourceL0, i, float64(10+i), 8, BeatTypeTransition))
	}
	out := RenderTensionBlock(rows)
	assert.Contains(t, out, "（20 段）")
	assert.Contains(t, out, "/…")

	// 125 章 → 列 120 章 + 余 5 章略。
	rows = rows[:0]
	for c := 1; c <= 125; c++ {
		rows = append(rows, blockRow(int64(100+c), c, TensionSourceL0, 0, 50, 50, BeatTypeTransition))
	}
	out = RenderTensionBlock(rows)
	assert.Contains(t, out, fmt.Sprintf("- #%d｜", 120))
	assert.NotContains(t, out, fmt.Sprintf("- #%d｜", 121))
	assert.Contains(t, out, "余 5 章略")
}

// TestRenderTensionBlockEmpty 空清单返回空串（零提示）。
func TestRenderTensionBlockEmpty(t *testing.T) {
	assert.Equal(t, "", RenderTensionBlock(nil))
}

// TestRenderTensionBlockDeterministic 同输入同输出（章序按阅读序号升序）。
func TestRenderTensionBlockDeterministic(t *testing.T) {
	rows := []ChapterTension{
		blockRow(31, 2, TensionSourceL0, 0, 40, 40, BeatTypeTransition),
		blockRow(30, 1, TensionSourceL0, 0, 30, 30, BeatTypeTransition),
	}
	out := RenderTensionBlock(rows)
	assert.Less(t, strings.Index(out, "- #1｜"), strings.Index(out, "- #2｜"), "章序升序")
	assert.Equal(t, out, RenderTensionBlock(rows))
}
