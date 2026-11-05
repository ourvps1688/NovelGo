package review

// 6 维判官分（0-100）↔ 8 维体检分（1-5 星）的映射契约单源。
//
// 背景（docs/quality-signability-upgrade-plan-2026-11-05.md §P0-3）：
// 评分门禁的 6 维判官与签约体检的 8 维评分各说各话，「双轨评分无映射」。
// 本文件是两套维度之间的唯一映射口径：判官分维落库后按此表换算成体检口径，
// 前端「同源分」展示与趋势视图均以此为准，不得在别处另行硬编码映射。
//
// 契约约束：
//   - 判官侧键集 == score.go 的 scoringDimensions 六键（由测试锁定）；
//   - 体检侧键集 == signability-audit.md 八维 == 前端 SIGN_DIMENSIONS 八键
//     （opening/fit/character/pacing/world/longArc/compliance/reader）。
//   - fit（题材契合）是判官侧未覆盖维度（6 维判官不评题材契合），
//     换算结果中不出现，属契约预期而非遗漏。

// 8 维体检维度键（与 signability-audit.md / 前端 SIGN_DIMENSIONS 同名）。
const (
	AuditDimOpening    = "opening"
	AuditDimFit        = "fit"
	AuditDimCharacter  = "character"
	AuditDimPacing     = "pacing"
	AuditDimWorld      = "world"
	AuditDimLongArc    = "longArc"
	AuditDimCompliance = "compliance"
	AuditDimReader     = "reader"
)

// AuditDimOrder 是 8 维体检维度的规范展示顺序（与体检报告一致）。
var AuditDimOrder = []string{
	AuditDimOpening, AuditDimFit, AuditDimCharacter, AuditDimPacing,
	AuditDimWorld, AuditDimLongArc, AuditDimCompliance, AuditDimReader,
}

// ScoreDimToAuditDims 是 6 维判官 → 8 维体检的映射表（单源）。
// 一个判官维度可贡献多个体检维度；计划口径示例：plot_advance → dim4(pacing)、
// emotion_hook → dim1(opening)。
var ScoreDimToAuditDims = map[string][]string{
	"word_count":   {AuditDimPacing},                   // 篇幅/节奏控制
	"plot_advance": {AuditDimPacing, AuditDimLongArc},  // 情节推进 → 节奏 + 长线
	"emotion_hook": {AuditDimOpening, AuditDimReader},  // 情绪钩子 → 开篇 + 读者体验
	"consistency":  {AuditDimCharacter, AuditDimWorld}, // 人设一致 → 人物 + 世界观
	"deai":         {AuditDimReader},                   // 去AI味 → 读者体验
	"taboo":        {AuditDimCompliance},               // 禁忌合规 → 合规
}

// JudgeAuditDimScore 是换算后的体检口径分（同源展示用）。
type JudgeAuditDimScore struct {
	Key         string   `json:"key"`         // 体检维度键
	Score       int      `json:"score"`       // 1-5 星（与体检同刻度）
	Contributed []string `json:"contributed"` // 贡献该维度的判官维度键（规范顺序）
}

// ScoreDimInfo 是 6 维判官维度的元数据（键+中文名+映射目标），供趋势 API/前端图例消费。
// 单源 scoringDimensions + ScoreDimToAuditDims，禁止在别处手抄维度键/中文名。
type ScoreDimInfo struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Audit []string `json:"audit"` // 映射到的体检维度键
}

// ScoreDimInfos 返回六维判官元数据（scoringDimensions 规范顺序）。
func ScoreDimInfos() []ScoreDimInfo {
	out := make([]ScoreDimInfo, 0, len(scoringDimensions))
	for _, d := range scoringDimensions {
		out = append(out, ScoreDimInfo{Key: d.Key, Label: d.Label, Audit: ScoreDimToAuditDims[d.Key]})
	}
	return out
}

// StarsFromScore 把 0-100 的判官分归一为 1-5 星（与体检 1-5 同刻度）。
// 阈值：<40→1，<55→2，<70→3，<85→4，其余 5；越界先裁剪到 0-100。
func StarsFromScore(score int) int {
	score = clampScore(score)
	switch {
	case score >= 85:
		return 5
	case score >= 70:
		return 4
	case score >= 55:
		return 3
	case score >= 40:
		return 2
	default:
		return 1
	}
}

// JudgeToAuditScores 把 6 维判官分（0-100）按映射表换算成 8 维体检口径分。
// 同一体检维度有多个贡献源时取平均；无任何判官分输入时返回空切片。
// 返回顺序遵循 AuditDimOrder，只含至少有一个贡献源的维度（fit 等无源维度不出现）。
func JudgeToAuditScores(dims map[string]int) []JudgeAuditDimScore {
	if len(dims) == 0 {
		return nil
	}
	// 反向索引：体检维度 → 贡献的判官维度（按 scoringDimensions 规范顺序）。
	contributors := map[string][]string{}
	sums := map[string]int{}
	for _, jd := range scoringDimensions {
		v, ok := dims[jd.Key]
		if !ok {
			continue
		}
		for _, ad := range ScoreDimToAuditDims[jd.Key] {
			contributors[ad] = append(contributors[ad], jd.Key)
			sums[ad] += v
		}
	}
	out := make([]JudgeAuditDimScore, 0, len(contributors))
	for _, ad := range AuditDimOrder {
		cons, ok := contributors[ad]
		if !ok {
			continue
		}
		out = append(out, JudgeAuditDimScore{
			Key:         ad,
			Score:       StarsFromScore(sums[ad] / len(cons)),
			Contributed: cons,
		})
	}
	return out
}
