package quality

// #114 P3 真实语料重标定回归锁（勿回退）。
//
// 实测真实第一章（非武侠、冷峻克制）10 段：
//   conflict_density 全 0.00（冲突词表多武侠词，非武侠题材恒 0）、
//   emotion_density / excl_density 全 0、action_density 仅 0.00–0.80 次/百字、
//   short_sentence_ratio 0.20–0.70。
// 旧帽（动作 3.0 / 短句 0.6）下 structure 理论上限
//   0.40×0 + 0.35×(0.80/3.0) + 0.25×1 = 0.343 < 峰阈值 0.40
// ——无论 chat 怎么改都数学上不可能达标，正是用户「改 8 轮仍报 beat_gap」的根因。
// P0 的可达性测试用「堆满动作词」的合成段（动作密度 7.50/百字）蒙混过关，
// 真实写作永远达不到，所以测试全绿却依然死循环。
//
// 现按真实量级重标定（动作 1.0 / 短句 0.40 / 峰阈值 0.45）。本文件锁死三件事：
//   ① 真实语料最高量级可达结构峰；② 平铺段仍不可达；③ 常量与派生口径不得回调。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealCorpusCalibrationConstants(t *testing.T) {
	assert.Equal(t, 1.0, tensionActionCap, "动作密度帽勿回调 3.0（真实语料实测上限 0.80）")
	assert.Equal(t, 0.4, tensionShortRatioCap, "短句占比帽勿回调 0.6/0.5（0.5 时真实 seg1 差 0.003 仍不达峰）")
	assert.Equal(t, 0.45, structuralPeakAt, "结构峰阈值勿回调 0.40/0.62")
	assert.Equal(t, 4, beatActionHitsNeeded(), "400 字 beat 段需 ≥4 处具体动作（由密度帽派生）")
}

func TestRealCorpusMagnitudeReachesStructuralPeak(t *testing.T) {
	// 真实第一章各段指标的上界组合（非合成堆词样本）。
	realMax := tensionSegStats{runes: 400, conflictPer: 0, actionPer: 0.80, shortRatio: 0.70}
	assert.GreaterOrEqual(t, structuralSignal(realMax), structuralPeakAt,
		"真实语料量级必须可达结构峰，否则 chat 改稿再多次也报 beat_gap")

	// 真实第一章 seg1 的原始指标（chat 定点改过的对抗升级段）：这一条才是
	// 用户「改了却仍报 beat_gap」的原件，必须可达峰。
	realSeg1 := tensionSegStats{runes: 515, conflictPer: 0, actionPer: 0.777, shortRatio: 0.350}
	assert.GreaterOrEqual(t, structuralSignal(realSeg1), structuralPeakAt,
		"chat 定点改过的对抗升级段必须被判定器看见")
	// 短句帽 0.5（上一版候选）会让该段差 0.003 仍不达峰，锁死勿回调。
	shortCap05 := clamp01(0.35*clamp01(realSeg1.actionPer/tensionActionCap) +
		0.25*clamp01(realSeg1.shortRatio/0.5))
	assert.Less(t, shortCap05, structuralPeakAt, "短句帽 0.5 会让真实 seg1 仍不达峰")

	// 反证：旧帽下同一段仍不可达（这正是被修掉的死循环）。
	legacy := clamp01(0.40*clamp01(realMax.conflictPer/6.0) +
		0.35*clamp01(realMax.actionPer/3.0) +
		0.25*clamp01(realMax.shortRatio/0.6))
	assert.Less(t, legacy, 0.40, "旧帽下真实语料数学上不可能达标（回归证据）")

	// 平铺段（无动作、长句、零冲突）仍必须判不出结构峰，避免把阈值放水成恒真。
	flat := tensionSegStats{runes: 400}
	assert.Less(t, structuralSignal(flat), structuralPeakAt, "平铺段不得被判成结构峰")
}

// realisticActiveBeat 是「正常非武侠叙事 + 一处对抗升级」的短句写实段落，
// 动作词只是零散几处（不是堆词），用来锁 BuildTensionCurve → beatPeakCount 全链。
const realisticActiveBeat = "舱门合上的声音很轻。他把纸摊在台面上。读数没变。" +
	"灯闪了两次。他抬眼。走廊尽头没有人。风从格栅里进来。带着一点铁锈味。" +
	"他停住。杯沿有一圈旧痕。记录本翻到第三页。墨色被指尖蹭淡了。" +
	"广播报了一串编号。重复了两遍。他把笔放下。等杂音过去。" +
	"屏幕上多出一行字。很短。他看了很久。然后合上本子。站起身。" +
	"灯比刚才暗了。他没回头。走廊尽头传来一声闷响。不是警报。" +
	"他把手按在门边。等下一轮读数。三十秒。数字跳了一格。他没动。" +
	"记录本上多了一道折痕。走廊重新安静下来。"

// realisticFlatPara 是长句、无具体动作、零冲突的平铺段（每句 >18 rune）。
const realisticFlatPara = "走廊两侧的舱壁在长期低照度环境里呈现出均匀而缺乏层次的灰白色，维护记录把这类色差归为正常老化，没有标注需要更换的部件。" +
	"通风系统按既定周期工作，气流经过格栅时的声音被压得很低，几乎与背景噪声混在一起，日常巡检也不把它列入异常项。" +
	"操作台上的读数每隔三十秒刷新一次，数值波动始终落在容许区间内，没有任何一项需要人工干预或额外记录。" +
	"值班表贴在门后，上一班的签名已经有些模糊，纸角因为潮气微微卷起，却仍然能看清交接时间。"

func realisticNonWuxiaChapter() string {
	// 每段 ≥400 rune，保证 BuildTensionCurve 各切成一个独立 beat 段。
	flat := strings.Repeat(realisticFlatPara, 2)
	active := strings.Repeat(realisticActiveBeat, 2)
	return strings.Join([]string{flat, active, flat}, "\n")
}

func TestRealisticNonWuxiaChapterHasStructuralBeat(t *testing.T) {
	curve := BuildTensionCurve(realisticNonWuxiaChapter())
	require.GreaterOrEqual(t, len(curve), 3)
	assert.GreaterOrEqual(t, structuralPeakCount(curve), 1,
		"一处正常对抗升级段必须被判定器看见")
	assert.GreaterOrEqual(t, beatPeakCount(curve), 1, "爽点单源计数必须 ≥1")

	// P0 的合成堆词样本与平铺样本结论不得回退。
	assert.Greater(t, beatPeakCount(BuildTensionCurve(coldShowNotTellChapter())), 0)
	assert.Equal(t, 0, structuralPeakCount(BuildTensionCurve(plainChapter())))
}

func TestBeatPrescriptionCarriesSegmentLevelDiagnostics(t *testing.T) {
	curve := BuildTensionCurve(realisticNonWuxiaChapter())
	lines := RenderPrescriptions(BuildPrescriptions(
		[]Finding{{Check: CheckBeatGap}}, curve, 2400, ChapterWordsTarget{}))
	require.Len(t, lines, 1)
	line := lines[0]
	assert.Contains(t, line, "判定：全章爽点段数 ≥1")
	assert.Contains(t, line, "structure = 0.40×", "处方必须暴露可执行判定式")
	assert.Contains(t, line, "离达标最近 seg ", "处方必须指出改哪一段")
	assert.Contains(t, line, "张力最弱 seg ", "处方必须指出最弱段")
	assert.Contains(t, line, "处具体动作", "处方必须给出可执行数字")
	assert.Contains(t, line, "不要整章重写", "定点改稿优先于整章重写")
	assert.NotContains(t, line, "%.2f", "渲染结果不得残留格式化占位符")
	assert.NotContains(t, line, "%d", "渲染结果不得残留格式化占位符")
}
