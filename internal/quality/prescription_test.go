package quality

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #114 行为契约：冷峻克制（show-not-tell）的写法不能再被数学上锁死在
// emotion=8 / tension<25，从而无论改多少轮都必然报「爽点断档 + 拖沓段」。

func coldShowNotTellChapter() string {
	para := "他攥紧指节，没答。脚步顿住，抬眼，压低声音。转身撞上那道目光，咬牙上前，掌心按住门框。碎裂声从背后传来，他没回头。"
	return strings.Repeat(para+"\n", 24)
}

func plainChapter() string {
	para := "院子很安静。树叶轻轻摇晃。他在台阶上站了一会儿。天空很蓝。云慢慢移动。远处有山。"
	return strings.Repeat(para+"\n", 24)
}

func TestShowNotTellReachesStructuralBeat(t *testing.T) {
	cold := BuildTensionCurve(coldShowNotTellChapter())
	plain := BuildTensionCurve(plainChapter())
	require.GreaterOrEqual(t, len(cold), 3)
	require.GreaterOrEqual(t, len(plain), 3)

	assert.Greater(t, beatPeakCount(cold), beatPeakCount(plain),
		"具体动作/对抗/短句节奏必须能构成结构峰（爽点第二通路）")
	assert.Greater(t, structuralPeakCount(cold), 0)

	coldMin, coldMax := 100.0, 0.0
	for _, p := range cold {
		if p.Tension < coldMin {
			coldMin = p.Tension
		}
		if p.Tension > coldMax {
			coldMax = p.Tension
		}
	}
	assert.GreaterOrEqual(t, coldMax, tensionFlatBelow,
		"结构信号须把至少一段 tension 抬过平坦阈值，否则拖沓段永远不可消除")

	// 同一份正文跑完整 L0：不得再必然命中爽点断档。
	_, fs := RunL0Checks(L0Input{Text: coldShowNotTellChapter(), ReadingNumber: 1})
	assert.False(t, hasFindingCheckFlag(fs, CheckBeatGap), "冷峻写法不应再必然报爽点断档")
	assert.False(t, hasFindingCheckFlag(fs, CheckPacingDrag), "冷峻写法不应再必然报拖沓段")

	// 平铺直叙仍应被判平坦（宁缺勿错，原有能力不回退）。
	_, plainFs := RunL0Checks(L0Input{Text: plainChapter(), ReadingNumber: 1})
	assert.True(t, hasFindingCheckFlag(plainFs, CheckPacingDrag), "平铺直叙仍须报拖沓段")
}

// TestStructuralPeakReachableByOneConcreteBeat 锁死阈值可达性：一处正常的
// 对抗升级段（具体动作 + 一次冲突 + 短句，且完全不堆直白情绪词/感叹号）
// 必须能构成结构峰；纯平铺段必须不能。防止把阈值调到「怎么改都不达标」。
func TestStructuralPeakReachableByOneConcreteBeat(t *testing.T) {
	beat := strings.Repeat("他攥住门框，没动。指节压得发白，抬眼。门外那道脚步停了，没进来。他侧身，把掌心按在冷铁上，呼吸一顿。碎裂声从走廊尽头传来，很轻。他没回头，只把手指往回收了收。对面的人忽然开口，声音压得很低，说的却不是账。他抬眼，第一次看清对方袖口那道旧疤。", 3)
	plain := strings.Repeat("院子很安静。树叶轻轻摇晃。他在台阶上站了一会儿。天空很蓝。云慢慢移动。远处有山。", 8)

	beatSignal := structuralSignal(computeTensionSegStats(beat))
	plainSignal := structuralSignal(computeTensionSegStats(plain))
	assert.GreaterOrEqual(t, beatSignal, structuralPeakAt,
		"一处正常对抗升级段必须可达结构峰（当前 %.2f < %.2f）", beatSignal, structuralPeakAt)
	assert.Less(t, plainSignal, structuralPeakAt,
		"平铺段不得被误判为结构峰（当前 %.2f）", plainSignal)
	assert.Equal(t, tensionBase, emotionScore(computeTensionSegStats(beat)),
		"该段 emotion 保持基线，证明结构峰通路不依赖直白情绪词/感叹号")
}

func hasFindingCheckFlag(list []Finding, check string) bool {
	for _, f := range list {
		if f.Check == check {
			return true
		}
	}
	return false
}

func TestPrescriptionSharesDetectorConstants(t *testing.T) {
	curve := BuildTensionCurve(coldShowNotTellChapter())
	findings := []Finding{
		{ID: "C1", Check: CheckTailHook, Desc: "章尾无钩"},
		{ID: "C2", Check: CheckPacingDrag, Desc: "拖沓"},
		{ID: "C3", Check: CheckBeatGap, Desc: "爽点断档"},
		{ID: "C4", Check: CheckOpeningSlope, Desc: "坡度不足"},
		{ID: "C5", Check: CheckWordCount, Desc: "偏短"},
		{ID: "C6", Check: CheckAIFlavor, Desc: "套话"},
		{ID: "L1-1", Check: "reader_state", Desc: "台账矛盾", LedgerRefs: []string{"entry_id:2108"}},
	}
	words := ChapterWordsTarget{Min: 1000, Max: 1500, Target: 1500, Source: "platform_profile", Label: "七猫 1000–1500"}
	lines := RenderPrescriptions(BuildPrescriptions(findings, curve, 1180, words))
	require.Len(t, lines, len(findings), "一条发现对应一条处方，保序不去重")
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		fmt.Sprintf("%d rune", tailHookWindow),
		fmt.Sprintf("%.0f", tensionFlatBelow),
		fmt.Sprintf("%.1f", openingSlopeMinRise),
		fmt.Sprintf("%d 字", 1180),
		"1000–1500 字",
		"entry_id:2108",
		"避免：",
	} {
		assert.Contains(t, joined, want, "处方必须由检测侧常量/实测值投影，不得另写数字")
	}
	assert.Contains(t, lines[6], "更新/作废过时台账条目", "L1 台账矛盾须给出「改正文或改台账」的二选一")
	for i, line := range lines {
		assert.True(t, strings.HasPrefix(line, "["+findings[i].Check+"]"),
			"处方行必须以 [check] 开头供前端对齐")
	}
}

func TestReconcileFindingConflicts(t *testing.T) {
	base := []Finding{
		{Check: CheckAIFlavor, Desc: "AI 味套话"},
		{Check: CheckPacingDrag, Desc: "拖沓段"},
		{Check: CheckWordCount, Desc: "偏短"},
	}
	got := reconcileFindingConflicts(append([]Finding{}, base...))
	assert.Contains(t, got[1].Desc, "禁止靠堆直白情绪词或感叹号抬张力")
	assert.Contains(t, got[1].Desc, "在现有字数预算内替换冗余段落")
	assert.Equal(t, base[0].Desc, got[0].Desc, "非曲线类发现逐字不动")
	assert.Equal(t, base[2].Desc, got[2].Desc, "非曲线类发现逐字不动")

	single := []Finding{{Check: CheckPacingDrag, Desc: "拖沓段"}}
	assert.Equal(t, single[0].Desc, reconcileFindingConflicts(single)[0].Desc,
		"无互斥项时逐字保持缺省行为")
}

func TestOpeningSlopeDirectionMatchesPrescription(t *testing.T) {
	// 首段高、后续回落 → 报（旧取末段口径也会报）；首段低、后续抬升 → 不报。
	down := []TensionPoint{
		{Seg: 0, StartRune: 0, Tension: 60},
		{Seg: 1, StartRune: 400, Tension: 50},
		{Seg: 2, StartRune: 800, Tension: 55},
	}
	up := []TensionPoint{
		{Seg: 0, StartRune: 0, Tension: 20},
		{Seg: 1, StartRune: 400, Tension: 80},
		{Seg: 2, StartRune: 800, Tension: 30},
	}
	require.NotNil(t, openingSlopeFinding(down))
	assert.Contains(t, openingSlopeFinding(down).Desc, "不要只在首段堆冲突")
	assert.Nil(t, openingSlopeFinding(up))
}
