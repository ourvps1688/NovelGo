package agentcfg

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/novelgo/novelgo/internal/preference"
)

// effectiveConstraint 把题材卡（创作约束）与平台卡（平台约束）之间冲突的"节奏时序"参数
// 在代码层解析成单张权威表，消除 LLM 自行调和多卡数值的矛盾。
//
// 设计要点：
//   - 题材卡管「调性/禁忌/内容 flavor」，平台卡管「时序/节奏/签约模型」；二者冲突时平台卡赢在节奏。
//   - 题材卡的质性约束（三定律/禁忌/爽点密度/情绪曲线）保持原卡生效，本块不重复，只解析两卡冲突的数值旋钮。
//   - 解析宽松：某字段缺失不影响其它字段；非模板自由文本（无 marker）不会被误识别。
//   - 无题材卡且无平台卡时返回空串，零成本注入。
//
// 卡片识别与前端 platformTemplates.ts / genreTemplates.ts 的 marker 对齐。

var (
	genreMarkerRe    = regexp.MustCompile(`(?m)^【创作约束·`)
	platformMarkerRe = regexp.MustCompile(`(?m)^【平台约束·`)

	// 数值抽取（宽松，字段缺失不影响其它字段）。
	genreFloorRe      = regexp.MustCompile(`单章正文不少于\s*(\d+)\s*字`)
	platformChapterRe = regexp.MustCompile(`单章目标字数\s*=\s*(\d+)\D+(\d+)\s*字`)
	platformHookRe    = regexp.MustCompile(`hook_window_open\s*=\s*(\d+)\s*字`)
	platformMicroRe   = regexp.MustCompile(`micro_hook_interval\s*=\s*(\d+)\s*字`)
	platformSetupRe   = regexp.MustCompile(`allow_setup_chapters\s*=\s*(是|否)`)
	platformPaywallRe = regexp.MustCompile(`(?:付费墙\s*aware|paywall_aware)\s*=\s*(是|否)`)
)

type effectiveValues struct {
	genreFloor     int
	hasGenreFloor  bool
	platChapterMin int
	platChapterMax int
	hasPlatChapter bool
	hookWindow     int
	hasHookWindow  bool
	microHook      int
	hasMicroHook   bool
	allowSetup     string
	paywallAware   string
	hasPlatform    bool
}

// parseConstraints 扫描偏好列表，抽取题材卡与平台卡的节奏时序数值。
// 只识别带 marker 的模板条目，用户自定义同分类自由文本不误伤。
func parseConstraints(prefItems []preference.PreferenceItem) effectiveValues {
	var v effectiveValues
	for _, item := range prefItems {
		content := strings.TrimSpace(item.Content)
		switch {
		case platformMarkerRe.MatchString(content):
			v.hasPlatform = true
			if m := platformChapterRe.FindStringSubmatch(content); m != nil {
				v.platChapterMin = atoiSafe(m[1])
				v.platChapterMax = atoiSafe(m[2])
				v.hasPlatChapter = true
			}
			if m := platformHookRe.FindStringSubmatch(content); m != nil {
				v.hookWindow = atoiSafe(m[1])
				v.hasHookWindow = true
			}
			if m := platformMicroRe.FindStringSubmatch(content); m != nil {
				v.microHook = atoiSafe(m[1])
				v.hasMicroHook = true
			}
			if m := platformSetupRe.FindStringSubmatch(content); m != nil {
				v.allowSetup = m[1]
			}
			if m := platformPaywallRe.FindStringSubmatch(content); m != nil {
				v.paywallAware = m[1]
			}
		case genreMarkerRe.MatchString(content):
			if m := genreFloorRe.FindStringSubmatch(content); m != nil {
				v.genreFloor = atoiSafe(m[1])
				v.hasGenreFloor = true
			}
		}
	}
	return v
}

// atoiSafe 解析纯数字前缀，非数字字符截断（无依赖，避免 strconv 开销）。
func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// BuildEffectiveConstraint 生成「签约约束解析（已去冲突）」单表。
// 返回空串表示无题材卡/平台卡，调用方据此跳过注入（零成本）。
func BuildEffectiveConstraint(prefItems []preference.PreferenceItem) string {
	v := parseConstraints(prefItems)
	if !v.hasPlatform && !v.hasGenreFloor {
		return ""
	}

	var b strings.Builder
	b.WriteString("【签约约束解析（已去冲突，节奏以本表为准）】\n")
	b.WriteString("题材卡约束内容调性/禁忌，平台卡约束节奏时序；二者冲突时平台卡赢在节奏时序，题材卡赢在内容禁忌。\n")

	// 单章字数（唯一真正冲突的数值旋钮）。
	b.WriteString("- 单章字数：")
	switch {
	case v.hasPlatChapter:
		fmt.Fprintf(&b, "目标 %d–%d 字（平台卡为准）", v.platChapterMin, v.platChapterMax)
		if v.hasGenreFloor && v.genreFloor > v.platChapterMax {
			fmt.Fprintf(&b, "；题材卡下限≥%d字与平台上限冲突，以平台为准（签约节奏优先）", v.genreFloor)
		}
	case v.hasGenreFloor:
		fmt.Fprintf(&b, "题材卡为准，单章不少于 %d 字", v.genreFloor)
	default:
		b.WriteString("系统默认 2000 字")
	}
	b.WriteString("\n")
	// 明确覆盖题材卡自身的字数下限行，避免 LLM 仍按旧卡折算。
	if v.hasPlatChapter {
		b.WriteString("  （若题材卡含「单章不少于 X 字」而与本表区间冲突，一律以本表平台优先为准。）\n")
	}

	if v.hasHookWindow {
		fmt.Fprintf(&b, "- 首个冲突钩子窗口：前 %d 字内必出钩子（平台卡）\n", v.hookWindow)
	}
	if v.hasMicroHook {
		fmt.Fprintf(&b, "- 章内微钩间隔：约每 %d 字一个悬念/进展点（平台卡）\n", v.microHook)
	}
	if v.allowSetup != "" {
		fmt.Fprintf(&b, "- 纯铺垫章：%s（平台卡）\n", v.allowSetup)
	}
	if v.paywallAware != "" {
		fmt.Fprintf(&b, "- 付费墙感知：%s（平台卡）\n", v.paywallAware)
	}
	b.WriteString("- 题材卡的爽点密度/情绪曲线/三定律/禁忌仍全程生效，约束内容质量，不覆盖上述节奏。\n")
	return b.String()
}
