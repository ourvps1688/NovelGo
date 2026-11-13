package quality

// tension.go —— 情绪账（P0-2）：章内张力/情绪曲线的真理源。
//
// 每章正文落盘后由 L0 确定性近似自动落一条基线曲线（写后/评分链路共用，
// 零 LLM、毫秒级、纯函数）：按 beat 粒度分段，每段以对话比、冲突词密度、
// 句长方差、感叹/反问密度四项确定性指标映射 tension（0-100），情绪词密度
// 映射 emotion（0-100），并启发式标注 beat_type。L1 chapter-ci 回执的
// tension_curve（LLM 标注）经 L1Receipt.ToTensionPoints 清洗后落 source=l1
// 行（契约 v3，与 L0 基线并存）；消费侧经 EffectiveTensionCurve 合并，
// 同 seg 取优先生效源（l1 > l0）。
//
// 契约要点（勿改）：
//   - 曲线行按 (novel_id, chapter_id, source, seg) 唯一，整章按来源先删后插（幂等）；
//   - 正文变化（写后链路）→ L0 行整体重算、既有 L1 行一并作废（旧内容标注不再可信）；
//   - 四指标口径为常量启发式（饱和点常量），实证调参前不动；宁缺勿错：空正文空曲线；
//   - #114：新增「结构信号」（具体动作细节 / 对抗密度 / 短句节奏），作为情绪峰与
//     张力的第二通路。原 emotion 数值逐字不变（图表/存档兼容），但 climax 之外的
//     show-not-tell 写法不再必然被判「无爽点 / 拖沓」——这是与去 AI 味同源的口径。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/text"
)

// 曲线来源（source 枚举，禁造值）。
const (
	TensionSourceL0 = "l0" // L0 确定性基线（写后/评分链路自动落库）
	TensionSourceL1 = "l1" // L1 chapter-ci 回执 tension_curve（LLM 标注，#91）
)

// BeatType 是 beat 段类型标注（L0 启发式；L1 可带自由值，消费侧未知值按 transition 处理）。
const (
	BeatTypeClimax     = "climax"     // 情绪峰/爽点（emotion 高位）
	BeatTypeConflict   = "conflict"   // 冲突对抗（冲突词密集）
	BeatTypeHook       = "hook"       // 钩子/悬念（悬念信号或问句/省略收束）
	BeatTypeDialogue   = "dialogue"   // 对话节奏（对白占比高）
	BeatTypeFlat       = "flat"       // 平缓（低信号低张力）
	BeatTypeTransition = "transition" // 过渡叙事（其余）
)

// tensionSegTargetRunes 是 beat 段目标长度（rune）：段落累积到该值切一段。
const tensionSegTargetRunes = 400

// 四指标饱和点（归一到 0-1 的满刻度；常量口径，实证调参前不动）。
const (
	tensionDialogueCap    = 0.5 // 对话比满刻度
	tensionConflictCap    = 6.0 // 冲突词密度满刻度（次/百字）
	tensionExclCap        = 8.0 // 感叹/反问密度满刻度（个/百字）
	tensionSentenceVarCap = 250 // 句长方差满刻度（rune²）
	tensionEmotionCap     = 6.0 // 情绪词密度满刻度（次/百字）
)

// 曲线基线与 beat 标注阈值。
const (
	tensionBase            = 8.0  // 全零信号段的 tension/emotion 基线
	tensionEmotionClimaxAt = 70.0 // emotion ≥ 该值判 climax
	tensionFlatBelow       = 25.0 // tension < 该值判 flat
	tensionConflictBeatAt  = 0.5  // 冲突词归一密度 ≥ 该值判 conflict
	tensionDialogueBeatAt  = 0.45 // 对话比 ≥ 该值判 dialogue
)

// #114 结构信号（show-not-tell 通路）：不再把「堆直白情绪词 + 感叹号」当作
// 达成情绪峰/张力的唯一手段——那与去 AI 味的 tell-not-show 硬约束数学互斥
// （冷峻克制文风 emotion 恒为基线 8.0，必然报爽点断档 + 拖沓段）。
// 结构信号 = 对抗密度 + 具体动作/身体细节密度 + 短句节奏，三者都是去 AI 味
// 明确推荐的写法；零信号时为 0，原 tension/emotion 逐字保持缺省行为。
//
// #114 P3 真实语料重标定（勿回退）：旧帽按「堆词合成样本」标定，实测真实
// 第一章 10 段的动作密度仅 0.000–0.777 次/百字（合成样本高达 7.50）、冲突词
// 全 0、感叹/情绪词全 0，故旧帽下 structural_signal 理论上限仅 0.343，
// 低于阈值 0.40 —— 无论 chat 怎么改都数学上不可能达标（用户实测改 8 轮不消）。
// 现按真实语料量级重标定密度帽（动作 1.0 次/百字 ≈ 400 字段 4 处具体动作、
// 短句占比 0.40 满刻度，真实区间 0.167–0.714），峰阈值 0.45 保留区分度：
//   - 真实第一章（10 段）：0 峰 → 恰好 1 峰，落在 seg1 ——正是 chat 定点改过的
//     对抗升级段（action 0.777 / short 0.350 → 0.491）；其余 9 段最高 0.420；
//   - 纯平铺段：0.25，恒 < 0.45；一处正常对抗升级段：0.656，恒 ≥ 0.45；
//   - 逐字枚举过候选帽：短句 0.50 时 seg1 仅 0.447（差 0.003）仍是 0 峰，
//     动作 0.80 则会误判出 2 峰，故取 (1.0, 0.40, 0.45) 这一组。
//
// 冲突词表刻意不动（保守表多武侠词，非武侠题材恒 0；添词等于把判定式改成
// 题材专用），结构峰靠动作 + 短句两条通路达标。判定式结构与权重一律不动。
const (
	tensionActionCap        = 1.0 // 动作细节密度满刻度（次/百字，真实语料实测上限 0.777）
	tensionShortRatioCap    = 0.4 // 短句占比满刻度（≤18 rune 的句子，真实 0.167–0.714）
	tensionShortSentenceMax = 18  // 短句判定长度（rune）
	// structuralPeakAt 按「一段 400 字里一处明确的对抗升级」标定，且与重标定
	// 后的密度帽配套；勿单独回调 0.40（会与真实语料可达性回归锁冲突）。
	structuralPeakAt       = 0.45
	tensionStructuralBoost = 0.35 // 结构信号对 tension 的最大加成比例
)

// tensionActionWords 是具体动作 / 身体细节信号词（show-not-tell 口径，保守表）。
// 与 deai-flavor 的「用动作或场景细节替代直白情绪」同源，禁止在此塞回情绪词。
var tensionActionWords = []string{
	"攥", "按住", "抬手", "抬眼", "抬头", "低头", "转身", "侧身", "退后", "上前",
	"指尖", "指节", "喉结", "脚步", "掌心", "后背", "膝盖", "屏息", "摸到", "碰到",
	"抓住", "松开", "顿住", "停住", "僵住", "别开", "侧过", "半晌", "没接", "没答",
	"没动", "没回头", "咬住", "攥紧", "蹭过", "压低", "靠在", "蹲下", "站起",
}

// tensionConflictWords 是冲突/对抗信号词（保守表：多字词为主，宁缺勿错）。
var tensionConflictWords = []string{
	"怒吼", "咆哮", "嘶吼", "爆喝", "大喝", "厉喝", "杀意", "杀气", "煞气",
	"握拳", "咬牙", "拳头", "掌风", "剑气", "刀光", "轰然", "爆裂", "碎裂",
	"撞击", "厮杀", "拼杀", "激战", "混战", "血战", "战斗", "扑去", "冲上",
	"交手", "出手", "目眦", "暴起", "杀招", "杀到",
}

// tensionEmotionWords 是高唤醒情绪信号词（爽点/情绪峰近似，保守表）。
var tensionEmotionWords = []string{
	"震惊", "惊骇", "骇然", "狂喜", "大喜", "暴怒", "呆住", "懵了",
	"颤抖", "哆嗦", "惨叫", "哀嚎", "大笑", "狂笑", "哭了", "落泪",
	"心头一紧", "头皮发麻", "浑身一震", "倒吸", "瞳孔", "呼吸一滞", "难以置信",
}

// TensionPoint 是张力曲线上的一个 beat 段（章内段序 0-based）。
type TensionPoint struct {
	// Seg 是章内段序（0-based，升序连续）。
	Seg int `json:"seg"`
	// StartRune/EndRune 是段在正文中的 rune 偏移区间（图表横轴映射用，近似即可）。
	StartRune int `json:"start_rune"`
	EndRune   int `json:"end_rune"`
	// Emotion/Tension 是情绪强度/张力（0-100）。
	Emotion float64 `json:"emotion"`
	Tension float64 `json:"tension"`
	// BeatType 是段类型标注（BeatType*，L1 可带自由值）。
	BeatType string `json:"beat_type"`
	// Metrics 是确定性四指标（键=tensionMetric*，L1 曲线可为空）。
	Metrics map[string]float64 `json:"metrics,omitempty"`
}

// ChapterTension 是情绪账的一行：章内一个 beat 段的张力/情绪标注。
// (novel_id, chapter_id, source, seg) 唯一。
type ChapterTension struct {
	ID        int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID   int64 `gorm:"column:novel_id;not null;uniqueIndex:uk_chapter_tension,priority:1;index" json:"novel_id"`
	ChapterID int64 `gorm:"column:chapter_id;not null;uniqueIndex:uk_chapter_tension,priority:2" json:"chapter_id"`
	// Source 是曲线来源（TensionSource*）。
	Source string `gorm:"column:source;not null;uniqueIndex:uk_chapter_tension,priority:3" json:"source"`
	// Seg 是章内段序（0-based）。
	Seg int `gorm:"column:seg;not null;uniqueIndex:uk_chapter_tension,priority:4" json:"seg"`
	// ReadingNumber 是全局阅读序号（写入时取当前值）。
	ReadingNumber int `gorm:"column:reading_number;not null;index" json:"reading_number"`
	// StartRune/EndRune 是段的正文 rune 偏移区间。
	StartRune int `gorm:"column:start_rune;not null" json:"start_rune"`
	EndRune   int `gorm:"column:end_rune;not null" json:"end_rune"`
	// Emotion/Tension 是情绪强度/张力（0-100）。
	Emotion float64 `gorm:"column:emotion" json:"emotion"`
	Tension float64 `gorm:"column:tension" json:"tension"`
	// BeatType 是段类型标注（BeatType*）。
	BeatType string `gorm:"column:beat_type" json:"beat_type"`
	// Metrics 是确定性四指标 JSON（L1 行可空）。
	Metrics   string    `gorm:"column:metrics;type:text" json:"metrics"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (ChapterTension) TableName() string { return "chapter_tension" }

// 四指标 JSON 键（稳定契约：L0 写、诊断/前端读，禁两处各写一份）。
const (
	tensionMetricDialogue    = "dialogue_ratio"       // 对话比 0-1
	tensionMetricConflict    = "conflict_density"     // 冲突词密度（次/百字）
	tensionMetricSentenceVar = "sentence_len_var"     // 句长方差（rune²）
	tensionMetricExcl        = "excl_density"         // 感叹/反问密度（个/百字）
	tensionMetricEmotion     = "emotion_density"      // 情绪词密度（次/百字）
	tensionMetricRunes       = "runes"                // 段长度（rune）
	tensionMetricAction      = "action_density"       // #114 具体动作细节密度（次/百字）
	tensionMetricShortRatio  = "short_sentence_ratio" // #114 短句占比 0-1
	tensionMetricStructural  = "structural_signal"    // #114 结构信号 0-1（爽点第二通路）
)

// validTensionSource 判断曲线来源枚举值是否合法。
func validTensionSource(s string) bool {
	return s == TensionSourceL0 || s == TensionSourceL1
}

// NormalizeBeatType 把段类型标注归一到 BeatType* 枚举：合法值原样（去首尾空白），
// 未知/空值按 transition 处理（L1 自由值容错契约，tension.go 头注）。
func NormalizeBeatType(s string) string {
	switch strings.TrimSpace(s) {
	case BeatTypeClimax:
		return BeatTypeClimax
	case BeatTypeConflict:
		return BeatTypeConflict
	case BeatTypeHook:
		return BeatTypeHook
	case BeatTypeDialogue:
		return BeatTypeDialogue
	case BeatTypeFlat:
		return BeatTypeFlat
	case BeatTypeTransition:
		return BeatTypeTransition
	}
	return BeatTypeTransition
}

// EffectiveTensionCurve 从全来源曲线行取优先生效曲线（消费侧合并策略单源）：
// 同 seg 取 l1（LLM 标注）优先、无 l1 取 l0（确定性基线），按 seg 升序；
// 来源未知行忽略（宁缺勿错）。返回行保留来源，供展示溯源。
func EffectiveTensionCurve(rows []ChapterTension) []ChapterTension {
	bySeg := map[int]ChapterTension{}
	for _, r := range rows {
		switch r.Source {
		case TensionSourceL1:
			bySeg[r.Seg] = r
		case TensionSourceL0:
			if _, ok := bySeg[r.Seg]; !ok {
				bySeg[r.Seg] = r
			}
		}
	}
	if len(bySeg) == 0 {
		return nil
	}
	segs := make([]int, 0, len(bySeg))
	for seg := range bySeg {
		segs = append(segs, seg)
	}
	sort.Ints(segs)
	out := make([]ChapterTension, 0, len(segs))
	for _, seg := range segs {
		out = append(out, bySeg[seg])
	}
	return out
}

// TensionPointsFromRows 把张力曲线行转为图表点（seg 升序输入 → 同序输出；
// 消费侧取 EffectiveTensionCurve 合并结果时逐行直转即可）。Metrics 经解析随行带出。
func TensionPointsFromRows(rows []ChapterTension) []TensionPoint {
	pts := make([]TensionPoint, 0, len(rows))
	for _, r := range rows {
		pts = append(pts, TensionPoint{
			Seg: r.Seg, StartRune: r.StartRune, EndRune: r.EndRune,
			Emotion: r.Emotion, Tension: r.Tension, BeatType: r.BeatType,
			Metrics: ParseTensionMetrics(r.Metrics),
		})
	}
	return pts
}

// BuildTensionCurve 单章正文 → L0 确定性基线张力曲线（纯函数、零 LLM、毫秒级）。
// 分段：段落累积到 tensionSegTargetRunes 切一段（beat 粒度，末段可短）；
// 空正文/纯空白返回空曲线（宁缺勿错）。同输入输出逐字一致。
func BuildTensionCurve(text string) []TensionPoint {
	type seg struct {
		start, end int
		paras      []string
	}
	var segs []seg
	pos := 0 // rune 偏移游标（含换行，近似即可）
	acc := seg{start: 0, end: 0}
	accRunes := 0
	flush := func() {
		if len(acc.paras) > 0 {
			segs = append(segs, acc)
		}
		acc = seg{}
		accRunes = 0
	}
	for _, line := range strings.Split(text, "\n") {
		paraRunes := len([]rune(line))
		paraStart := pos
		pos += paraRunes + 1
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		if len(acc.paras) == 0 {
			acc.start = paraStart
		}
		acc.paras = append(acc.paras, p)
		acc.end = paraStart + paraRunes
		accRunes += len([]rune(p))
		if accRunes >= tensionSegTargetRunes {
			flush()
		}
	}
	flush()

	out := make([]TensionPoint, 0, len(segs))
	for i, s := range segs {
		segText := strings.Join(s.paras, "\n")
		st := computeTensionSegStats(segText)
		tension := tensionScore(st)
		emotion := emotionScore(st)
		out = append(out, TensionPoint{
			Seg:       i,
			StartRune: s.start,
			EndRune:   s.end,
			Emotion:   emotion,
			Tension:   tension,
			BeatType:  tensionBeatType(segText, st, tension, emotion),
			Metrics: map[string]float64{
				tensionMetricDialogue:    round1(st.dialogue),
				tensionMetricConflict:    round1(st.conflictPer),
				tensionMetricSentenceVar: round1(st.sentenceVar),
				tensionMetricExcl:        round1(st.exclPer),
				tensionMetricEmotion:     round1(st.emotionPer),
				tensionMetricRunes:       float64(st.runes),
				tensionMetricAction:      round1(st.actionPer),
				tensionMetricShortRatio:  round1(st.shortRatio),
				tensionMetricStructural:  round1(structuralSignal(st)),
			},
		})
	}
	return out
}

// tensionSegStats 是单段确定性指标（四指标 + 情绪词密度 + 段长）。
type tensionSegStats struct {
	runes       int
	dialogue    float64 // 对话比 0-1
	conflictPer float64 // 冲突词密度（次/百字）
	exclPer     float64 // 感叹/反问密度（个/百字）
	sentenceVar float64 // 句长方差（rune²）
	emotionPer  float64 // 情绪词密度（次/百字）
	actionPer   float64 // #114 具体动作/身体细节密度（次/百字）
	shortRatio  float64 // #114 短句占比 0-1
}

// computeTensionSegStats 计算单段指标（纯确定性）。
func computeTensionSegStats(seg string) tensionSegStats {
	st := tensionSegStats{}
	// 段长（非空白 rune）与对话比。
	inQuote := false
	for _, r := range seg {
		if unicode.IsSpace(r) {
			continue
		}
		st.runes++
		switch r {
		case '“', '「', '『':
			inQuote = true
			continue
		case '”', '」', '』':
			inQuote = false
			continue
		}
		if inQuote {
			st.dialogue++
		}
	}
	if st.runes > 0 {
		st.dialogue /= float64(st.runes)
	}
	// 感叹/反问密度（！？半全角，个/百字）。
	excl := 0
	for _, r := range seg {
		if r == '！' || r == '？' || r == '!' || r == '?' {
			excl++
		}
	}
	// 冲突/情绪词密度（次/百字）。
	conflict := countTensionHits(seg, tensionConflictWords)
	emotion := countTensionHits(seg, tensionEmotionWords)
	per100 := func(n int) float64 {
		if st.runes == 0 {
			return 0
		}
		return float64(n) * 100 / float64(st.runes)
	}
	st.exclPer = per100(excl)
	st.conflictPer = per100(conflict)
	st.emotionPer = per100(emotion)
	st.actionPer = per100(countTensionHits(seg, tensionActionWords))
	st.sentenceVar, st.shortRatio = sentenceLenStats(seg)
	return st
}

// sentenceLenStats 返回句长方差与短句占比（一次切分，避免两处各切一遍）。
// variance 保持既有总体方差口径（无信号段逐字一致），ratio 是 #114 结构信号输入。
func sentenceLenStats(seg string) (float64, float64) {
	var lens []float64
	cur := 0
	for _, r := range seg {
		switch r {
		case '。', '！', '？', '!', '?', '…', '；', ';', '\n':
			if cur > 0 {
				lens = append(lens, float64(cur))
				cur = 0
			}
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		cur++
	}
	if cur > 0 {
		lens = append(lens, float64(cur))
	}
	if len(lens) == 0 {
		return 0, 0
	}
	mean := 0.0
	short := 0
	for _, v := range lens {
		mean += v
		if v <= tensionShortSentenceMax {
			short++
		}
	}
	mean /= float64(len(lens))
	sum := 0.0
	for _, v := range lens {
		d := v - mean
		sum += d * d
	}
	return sum / float64(len(lens)), float64(short) / float64(len(lens))
}

// structuralSignal 结构信号（0-1）：对抗 0.40 + 具体动作细节 0.35 + 短句节奏 0.25。
// 三项都是去 AI 味明确推荐的写法；与「堆直白情绪词/感叹号」通路互不依赖。
func structuralSignal(st tensionSegStats) float64 {
	cn := clamp01(st.conflictPer / tensionConflictCap)
	an := clamp01(st.actionPer / tensionActionCap)
	sr := clamp01(st.shortRatio / tensionShortRatioCap)
	return clamp01(0.40*cn + 0.35*an + 0.25*sr)
}

// structuralPeakCount 统计结构峰段数（#114 爽点判定的第二通路）。
// 仅看 L0 基线段指标；手工构造/无指标的段不计（宁缺勿错，旧测试逐字兼容）。
func structuralPeakCount(curve []TensionPoint) int {
	n := 0
	for _, pt := range curve {
		if pt.Metrics[tensionMetricStructural] >= structuralPeakAt {
			n++
		}
	}
	return n
}

// beatPeakCount 是「爽点段数」单源（#114）：climax 段与结构峰段按段去重计数，
// 供 beat_density / beat_gap / 图表标记共用，杜绝两处各算一套导致判定漂移。
func beatPeakCount(curve []TensionPoint) int {
	n := 0
	for _, pt := range curve {
		if NormalizeBeatType(pt.BeatType) == BeatTypeClimax ||
			pt.Metrics[tensionMetricStructural] >= structuralPeakAt {
			n++
		}
	}
	return n
}

// countTensionHits 统计信号词出现次数（同一位置多词各计，密度近似口径）。
func countTensionHits(seg string, words []string) int {
	n := 0
	for _, w := range words {
		n += strings.Count(seg, w)
	}
	return n
}

// tensionScore 四指标 → 张力（0-100）：饱和归一加权 + #114 结构信号加成。
// 结构信号为 0 时与原计算逐字一致；有具体动作/对抗/短句节奏时抬升张力，
// 让 show-not-tell 的段落不再被固定判为 tension<25 的「拖沓段」。
func tensionScore(st tensionSegStats) float64 {
	dn := clamp01(st.dialogue / tensionDialogueCap)
	cn := clamp01(st.conflictPer / tensionConflictCap)
	en := clamp01(st.exclPer / tensionExclCap)
	vn := clamp01(st.sentenceVar / tensionSentenceVarCap)
	base := tensionBase + (100-tensionBase)*clamp01(0.30*dn+0.30*cn+0.20*en+0.20*vn)
	boost := (100 - tensionBase) * tensionStructuralBoost * structuralSignal(st)
	return round1(math.Min(100, base+boost))
}

// emotionScore 情绪词/感叹密度 → 情绪强度（0-100）：基线 tensionBase。
func emotionScore(st tensionSegStats) float64 {
	mn := clamp01(st.emotionPer / tensionEmotionCap)
	en := clamp01(st.exclPer / tensionExclCap)
	return round1(tensionBase + (100-tensionBase)*clamp01(0.60*mn+0.40*en))
}

// tensionBeatType 启发式段类型标注（优先级：climax > conflict > hook > dialogue > flat > transition）。
func tensionBeatType(seg string, st tensionSegStats, tension, emotion float64) string {
	switch {
	case emotion >= tensionEmotionClimaxAt:
		return BeatTypeClimax
	case clamp01(st.conflictPer/tensionConflictCap) >= tensionConflictBeatAt:
		return BeatTypeConflict
	case hasTensionHookSignal(seg):
		return BeatTypeHook
	case st.dialogue >= tensionDialogueBeatAt:
		return BeatTypeDialogue
	case tension < tensionFlatBelow:
		return BeatTypeFlat
	default:
		return BeatTypeTransition
	}
}

// hasTensionHookSignal 段内钩子/悬念信号：悬念信号词（tailHookSignals 单源）
// 或以问句/省略收束。
func hasTensionHookSignal(seg string) bool {
	trimmed := strings.TrimSpace(seg)
	if trimmed == "" {
		return false
	}
	if strings.HasSuffix(trimmed, "？") || strings.HasSuffix(trimmed, "?") ||
		strings.HasSuffix(trimmed, "…") || strings.HasSuffix(trimmed, "……") {
		return true
	}
	return text.ContainsAny(seg, tailHookSignals)
}

// clamp01 截断到 [0,1]。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// round1 保留一位小数（确定性输出，避免浮点尾差）。
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

// marshalTensionMetrics 序列化段指标 JSON；空 map 序列化为空串（坏数据宁缺勿错）。
func marshalTensionMetrics(m map[string]float64) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseTensionMetrics 解析段指标 JSON；空串/坏数据返回 nil（宁缺勿错，不报错）。
func ParseTensionMetrics(s string) map[string]float64 {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var m map[string]float64
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// ReplaceTensionCurve 整章替换某来源的张力曲线行（先删后插，同来源幂等）。
// points 为空 = 仅清该来源（L1 作废/空曲线）。
func (s *Store) ReplaceTensionCurve(ctx context.Context, novelID, chapterID int64, readingNumber int, source string, points []TensionPoint) error {
	if novelID <= 0 || chapterID <= 0 {
		return errors.New("quality store: tension: novel_id/chapter_id 缺失")
	}
	if !validTensionSource(source) {
		return fmt.Errorf("quality store: tension: 非法 source %q", source)
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("novel_id = ? AND chapter_id = ? AND source = ?", novelID, chapterID, source).
			Delete(&ChapterTension{}).Error; err != nil {
			return fmt.Errorf("quality store: tension replace: %w", err)
		}
		if len(points) == 0 {
			return nil
		}
		rows := make([]ChapterTension, 0, len(points))
		for _, p := range points {
			rows = append(rows, ChapterTension{
				NovelID:       novelID,
				ChapterID:     chapterID,
				ReadingNumber: readingNumber,
				Source:        source,
				Seg:           p.Seg,
				StartRune:     p.StartRune,
				EndRune:       p.EndRune,
				Emotion:       p.Emotion,
				Tension:       p.Tension,
				BeatType:      p.BeatType,
				Metrics:       marshalTensionMetrics(p.Metrics),
			})
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("quality store: tension replace: %w", err)
		}
		return nil
	})
}

// RefreshTensionCurve 重算 L0 基线张力曲线并整章落库（写后链路共用）：
// L0 行整体替换，既有 L1 行一并清空——正文已变，旧内容的 L1 标注不再可信（宁缺勿错）。
func (s *Store) RefreshTensionCurve(ctx context.Context, novelID, chapterID int64, readingNumber int, text string) error {
	if err := s.ReplaceTensionCurve(ctx, novelID, chapterID, readingNumber, TensionSourceL0, BuildTensionCurve(text)); err != nil {
		return err
	}
	return s.ReplaceTensionCurve(ctx, novelID, chapterID, readingNumber, TensionSourceL1, nil)
}

// ListTensionCurve 返回某章全部张力曲线行，按 source 升序、seg 升序
// （消费侧取优先生效源：l1 > l0）。
func (s *Store) ListTensionCurve(ctx context.Context, novelID, chapterID int64) ([]ChapterTension, error) {
	var rows []ChapterTension
	err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND chapter_id = ?", novelID, chapterID).
		Order("source ASC, seg ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("quality store: tension list: %w", err)
	}
	return rows, nil
}

// ListTensionCurveByNovel 返回一本书的全部张力曲线行（L0+L1 全源，章升序、
// 同章 source/seg 升序；消费侧经 EffectiveTensionCurve 按章合并取优先生效源）。
// 供 dim4「情绪曲线摘要」块（RenderTensionBlock）消费。
func (s *Store) ListTensionCurveByNovel(ctx context.Context, novelID int64) ([]ChapterTension, error) {
	var rows []ChapterTension
	err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("chapter_id ASC, source ASC, seg ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("quality store: tension list by novel: %w", err)
	}
	return rows, nil
}
