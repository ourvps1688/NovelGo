package quality

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── L0 基线曲线（纯函数）────────────────────────────────

// TestBuildTensionCurveEmpty 空正文/纯空白：空曲线（宁缺勿错）。
func TestBuildTensionCurveEmpty(t *testing.T) {
	assert.Empty(t, BuildTensionCurve(""))
	assert.Empty(t, BuildTensionCurve("   \n\t \n"))
}

// TestBuildTensionCurveSegments 多段长文：beat 分段有序、偏移区间递进覆盖。
func TestBuildTensionCurveSegments(t *testing.T) {
	para := strings.Repeat("今天天气很好，他出门散步，路上遇到几个熟人聊了一会儿天。", 3)
	text := strings.Repeat(para+"\n", 10)
	pts := BuildTensionCurve(text)
	require.Greater(t, len(pts), 1, "10 段长文应切出多个 beat 段")
	for i, p := range pts {
		assert.Equal(t, i, p.Seg, "段序连续升序")
		assert.Greater(t, p.EndRune, p.StartRune, "段区间非空")
		if i > 0 {
			assert.GreaterOrEqual(t, p.StartRune, pts[i-1].StartRune, "偏移递进")
		}
		assert.GreaterOrEqual(t, p.Tension, 0.0)
		assert.LessOrEqual(t, p.Tension, 100.0)
		assert.GreaterOrEqual(t, p.Emotion, 0.0)
		assert.LessOrEqual(t, p.Emotion, 100.0)
	}
}

// TestBuildTensionCurveDialogueRaises 对白密集段：对话比与张力显著高于平铺直叙段。
func TestBuildTensionCurveDialogueRaises(t *testing.T) {
	plain := BuildTensionCurve("院子很安静。树叶轻轻摇晃。他在台阶上站了一会儿。")
	dialogue := BuildTensionCurve("「你来了。」他看着她。「我等你很久了，一直在这儿。」「我知道。」她低声说。")
	require.Len(t, plain, 1)
	require.Len(t, dialogue, 1)
	assert.Greater(t, dialogue[0].Metrics[tensionMetricDialogue], plain[0].Metrics[tensionMetricDialogue])
	assert.Greater(t, dialogue[0].Tension, plain[0].Tension)
}

// TestBuildTensionCurveConflictBeat 冲突词密集段：beat_type=conflict、张力抬升。
func TestBuildTensionCurveConflictBeat(t *testing.T) {
	pts := BuildTensionCurve("他咬牙握拳，怒吼一声，冲上去交手，轰然一响，碎裂声中两人厮杀在一处。")
	require.Len(t, pts, 1)
	assert.Equal(t, BeatTypeConflict, pts[0].BeatType)
	assert.Greater(t, pts[0].Tension, tensionBase)
}

// TestBuildTensionCurveClimax 高唤醒情绪段：emotion 高位判 climax。
func TestBuildTensionCurveClimax(t *testing.T) {
	pts := BuildTensionCurve("全场震惊！他狂喜大笑，倒吸一口凉气，几乎难以置信！")
	require.Len(t, pts, 1)
	assert.Equal(t, BeatTypeClimax, pts[0].BeatType)
	assert.GreaterOrEqual(t, pts[0].Emotion, tensionEmotionClimaxAt)
}

// TestBuildTensionCurveHook 悬念收束段：问句/信号词收尾判 hook。
func TestBuildTensionCurveHook(t *testing.T) {
	q := BuildTensionCurve("他真的要这么做吗？")
	require.Len(t, q, 1)
	assert.Equal(t, BeatTypeHook, q[0].BeatType)

	sig := BuildTensionCurve("就在这时，门被人从外面推开了。")
	require.Len(t, sig, 1)
	assert.Equal(t, BeatTypeHook, sig[0].BeatType)
}

// TestBuildTensionCurveFlat 无信号平铺段：张力低于阈值判 flat。
func TestBuildTensionCurveFlat(t *testing.T) {
	pts := BuildTensionCurve("天空很蓝。云慢慢移动。远处有山。")
	require.Len(t, pts, 1)
	assert.Equal(t, BeatTypeFlat, pts[0].BeatType)
	assert.Less(t, pts[0].Tension, tensionFlatBelow)
}

// TestBuildTensionCurveDeterministic 同输入两次输出逐字一致（纯函数）。
func TestBuildTensionCurveDeterministic(t *testing.T) {
	text := "「站住！」他怒吼着扑上去。「你跑不掉的！」\n远处传来一声惨叫。然后是漫长的沉默，沉默里只有风声。"
	a := BuildTensionCurve(text)
	b := BuildTensionCurve(text)
	assert.Equal(t, a, b)
}

// ── 持久化（Replace/Refresh/List）────────────────────────

// TestChapterTensionSchema 建表 + (novel_id, chapter_id, source, seg) 唯一。
func TestChapterTensionSchema(t *testing.T) {
	db := newTestDB(t)
	require.True(t, db.Migrator().HasTable("chapter_tension"))
	for _, col := range []string{
		"novel_id", "chapter_id", "source", "seg", "reading_number",
		"start_rune", "end_rune", "emotion", "tension", "beat_type", "metrics",
	} {
		require.True(t, db.Migrator().HasColumn("chapter_tension", col), "缺列 %s", col)
	}
	first := ChapterTension{NovelID: 1, ChapterID: 7, Source: TensionSourceL0, Seg: 0}
	require.NoError(t, db.Create(&first).Error)
	dup := ChapterTension{NovelID: 1, ChapterID: 7, Source: TensionSourceL0, Seg: 0}
	assert.Error(t, db.Create(&dup).Error, "(novel_id, chapter_id, source, seg) 应唯一")
	other := ChapterTension{NovelID: 1, ChapterID: 7, Source: TensionSourceL0, Seg: 1}
	assert.NoError(t, db.Create(&other).Error, "不同段可共存")
}

// TestReplaceTensionCurve 同来源整章先删后插、异来源不受影响、空清单=仅清该来源。
func TestReplaceTensionCurve(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	l1Points := []TensionPoint{{Seg: 0, Tension: 90, Emotion: 80, BeatType: "custom"}}
	require.NoError(t, s.ReplaceTensionCurve(ctx, 1, 7, 3, TensionSourceL1, l1Points))

	l0Points := BuildTensionCurve("「你来了。」他看着她。\n就在这时，门被推开了。")
	require.NoError(t, s.ReplaceTensionCurve(ctx, 1, 7, 3, TensionSourceL0, l0Points))
	rows, err := s.ListTensionCurve(ctx, 1, 7)
	require.NoError(t, err)
	require.Len(t, rows, len(l0Points)+1, "L0 写入不动 L1 行")
	assert.Equal(t, TensionSourceL0, rows[0].Source, "按 source 升序")
	assert.Equal(t, TensionSourceL1, rows[len(rows)-1].Source)

	// 同来源重放：先删后插，段数以新值为准。
	require.NoError(t, s.ReplaceTensionCurve(ctx, 1, 7, 3, TensionSourceL0, l0Points[:1]))
	rows2, err := s.ListTensionCurve(ctx, 1, 7)
	require.NoError(t, err)
	assert.Len(t, rows2, 2, "L0 替换为 1 段，L1 保留")

	// 空清单 = 仅清该来源。
	require.NoError(t, s.ReplaceTensionCurve(ctx, 1, 7, 3, TensionSourceL1, nil))
	rows3, err := s.ListTensionCurve(ctx, 1, 7)
	require.NoError(t, err)
	assert.Len(t, rows3, 1)

	// 校验拒收：缺键/非法 source。
	assert.Error(t, s.ReplaceTensionCurve(ctx, 0, 7, 3, TensionSourceL0, nil))
	assert.Error(t, s.ReplaceTensionCurve(ctx, 1, 0, 3, TensionSourceL0, nil))
	assert.Error(t, s.ReplaceTensionCurve(ctx, 1, 7, 3, "l2", nil))
}

// TestRefreshTensionCurve 写后链路：L0 整体重算、旧 L1 行一并作废；重复刷新幂等。
func TestRefreshTensionCurve(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.ReplaceTensionCurve(ctx, 1, 7, 3, TensionSourceL1,
		[]TensionPoint{{Seg: 0, Tension: 90, Emotion: 80, BeatType: "custom"}}))
	require.NoError(t, s.RefreshTensionCurve(ctx, 1, 7, 3, "「你来了。」他看着她。\n就在这时，门被推开了。"))

	rows, err := s.ListTensionCurve(ctx, 1, 7)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, r := range rows {
		assert.Equal(t, TensionSourceL0, r.Source, "正文变化后旧 L1 行应作废")
	}

	// 幂等：同文本再刷一遍行数不变。
	require.NoError(t, s.RefreshTensionCurve(ctx, 1, 7, 3, "「你来了。」他看着她。\n就在这时，门被推开了。"))
	rows2, err := s.ListTensionCurve(ctx, 1, 7)
	require.NoError(t, err)
	assert.Len(t, rows2, len(rows))
}
