package quality

// platform_profile.go —— 平台风格 profile 机制化（P0-1，docs/quality-signability-upgrade-plan-2026-11-05.md）。
//
// 编辑反馈回流（P1-1）沉淀的平台口径（如七猫「前 500 字爆点/章长 1000–1500/爽点密度」）从文案约定
// 升格为机器可检参数：CheckerConfig.PlatformProfile 指名 profile → RunL0Checks 按 profile 窗口/区间/
// 密度下限出确定性发现（opening_hook / chapter_length_fit / beat_density，platform_checks.go）。
//
// 注册表纪律（勿臆造）：只发有真实编辑口径依据的 preset（当前仅七猫）；
// 其余平台（番茄/起点/晋江等）量化口径未获实证前不注册——未知名一律拒收（Validate），
// 消费侧未知/空 profile → nil=三查不跑（缺省=现行行为逐字一致）。

import "strings"

// PlatformProfile 是平台风格量化口径（L0 平台三查与开篇体检的窗口/区间参数单源）。
// 各字段 <=0 视为未设：开篇窗口/区间回落 P0-0 缺省常量（逐字一致）；
// 平台三查侧（opening_hook/beat_density）未设=该项不启用（宁缺勿错）。
type PlatformProfile struct {
	// Name 是 profile 注册名（落库值，如 "qimao"）。
	Name string
	// Title 是展示名（发现文案口径，如 "七猫"）。
	Title string
	// ChapterLenMin/ChapterLenMax 是章长适配区间（字）：黄金三章由 opening_length 覆盖，
	// 其余章由 chapter_length_fit 覆盖（作用域零重叠）。
	ChapterLenMin int
	ChapterLenMax int
	// HookEarlyWindow/HookEarlyMin 是开篇爆点前置窗口（字）与最低信号数：
	// 前 N 字内冲突/悬念/异常信号少于下限 → opening_hook 发现。
	HookEarlyWindow int
	HookEarlyMin    int
	// HookWindow/HookMin 是开篇爆点主窗口（字）与最低信号数（前 N 字口径）。
	HookWindow int
	HookMin    int
	// PremiseWindow 是核心看点实体化窗口（字，premise_late；缺省口径 500）。
	PremiseWindow int
	// CheatWindow 是金手指出场窗口（字，cheat_late；缺省口径 800）。
	CheatWindow int
	// ProtagonistWindow 是主角首现窗口（字，protagonist_late；缺省口径 200）。
	ProtagonistWindow int
	// OpeningChapterWindow 是黄金三章窗口（全局阅读序号 1..N）：opening_length 作用域。
	OpeningChapterWindow int
	// BeatDensityMin 是爽点密度下限（次/章，近似词表口径）：0=不限；
	// CheckerConfig.EffectiveBeatDensityMin 优先用显式列值，缺省回落本值。
	BeatDensityMin float64
	// EmotionSegMax 是单情绪段长度上限（字）：P0-2 情绪账消费（本阶段只入参不检查）。
	EmotionSegMax int
}

// QimaoProfile 返回七猫男频悬疑量化口径 preset（两本真实编辑驳回样本的口径，
// docs/quality-signability-upgrade-plan-2026-11-05.md §7）：
//   - 黄金三章章长 1000–1500 字；
//   - 前 500 字爆点（核心看点实体化 500 / 金手指出场 800 / 主角首现 200）；
//   - 爽点密度下限 1 次/章（近似词表口径，P0-2 张力曲线升级）；
//   - 情绪段长度上限 300 字（P0-2 消费）。
func QimaoProfile() *PlatformProfile {
	return &PlatformProfile{
		Name:                 "qimao",
		Title:                "七猫",
		ChapterLenMin:        1000,
		ChapterLenMax:        1500,
		HookEarlyWindow:      200,
		HookEarlyMin:         1,
		HookWindow:           500,
		HookMin:              2,
		PremiseWindow:        500,
		CheatWindow:          800,
		ProtagonistWindow:    200,
		OpeningChapterWindow: 3,
		BeatDensityMin:       1.0,
		EmotionSegMax:        300,
	}
}

// profileRegistry 是平台 profile 注册表（注册名/别名 → preset）。
// 只发有真实编辑口径依据的 preset（勿臆造其余平台量化口径）。
var profileRegistry = map[string]func() *PlatformProfile{
	"qimao": QimaoProfile,
	"七猫":    QimaoProfile,
}

// PlatformProfileByName 按名取平台 profile（大小写不敏感、TrimSpace；含别名 "七猫"）。
// 空/未注册 → nil（宁缺勿错：未知 profile 不武装任何检查）。
func PlatformProfileByName(name string) *PlatformProfile {
	key := normalizeProfileName(name)
	if key == "" {
		return nil
	}
	if preset, ok := profileRegistry[key]; ok {
		return preset()
	}
	return nil
}

// normalizeProfileName 归一 profile 名：TrimSpace + 小写（中文原样）。
func normalizeProfileName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
