package quality

// chapter_words.go —— 单章目标字数的唯一真相源（#113）。
//
// 背景：检测侧 L0 的 word_count 用 DefaultTargetWords=2000，而 opening_length /
// chapter_length_fit 用 PlatformProfile 的 1000–1500；写作/修复侧常驻 system
// （agentcfg.identity / BuildEffectiveConstraint）另有一套「默认 2000 字」。
// 同一章因此会出现「体检要求 1500 字、修复后对比又要求 2000 字」的互斥口径。
//
// 本文件把「本章目标字数」收敛成一次决策：先平台注册 profile，再偏好自由文本
// （平台卡区间 / 题材卡下限），都没有才回落到 DefaultTargetWords。检测侧 L0、
// 写作侧 system、前端质量档案展示三处共用同一结果，禁止各自再写数字。
//
// 纪律：本文件不 import agentcfg / preference / pipeline（避免循环），只接收
// 调用方投影好的纯文本切片；检测侧 p0 golden 五构造器逐字不动、复验侧
// parseChapterLengthWindow（从工单处方取区间，#105）保持独立不动。

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultTargetWords 是「全无配置」时的兜底目标字数（与 L0 RunL0Checks 同包共用）。
// 只有 ChapterWordsTarget.Source == "default" 时才会用到它；一旦存在平台 profile
// 或偏好卡，检测与写作都以解析结果为准，禁止再套用本常量。
const DefaultTargetWords = 2000

// ChapterWordsTarget 是单章目标字数的解析结果（唯一真相源的数据形态）。
// JSON 字段由 GetChapterQualityArchive 透出，供前端质量档案卡片展示生效口径。
type ChapterWordsTarget struct {
	// Min/Max 是目标区间（字）；Min<=0 表示无区间下限。
	Min int `json:"min"`
	Max int `json:"max"`
	// Target 是写作/字数门槛使用的单点目标（字），用于 word_count 的「低于 70%」。
	Target int `json:"target"`
	// Source 是口径来源：platform_profile / platform_card / genre_card / default。
	Source string `json:"source"`
	// Label 是面向人/LLM 的口径说明（如「七猫平台预设 1000–1500 字」）。
	Label string `json:"label"`
}

var (
	// 与 frontend/src/components/preference/platformTemplates.ts 的
	// 「单章目标字数 = 1200–2000 字」及 genreTemplates.ts 的
	// 「单章正文不少于 X 字」marker 对齐；只认带 marker 的模板卡文本。
	platformChapterWordsRe = regexp.MustCompile(`单章目标字数\s*=\s*(\d+)\D+(\d+)\s*字`)
	genreFloorWordsRe      = regexp.MustCompile(`单章正文不少于\s*(\d+)\s*字`)
	platformCardMarkerRe   = regexp.MustCompile(`(?m)^【平台约束·`)
	genreCardMarkerRe      = regexp.MustCompile(`(?m)^【创作约束·`)
)

// atoiSafe 解析正整数（纯数字前缀截断），非法/溢出返回 0。
func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		if n > 100000 {
			return 0
		}
	}
	return n
}

// ResolveChapterWords 按优先级解析单章目标字数（纯函数、零 DB 依赖）。
//
//  1. profile != nil 且区间有效  → 注册平台预设（如七猫 1000–1500），最权威；
//  2. 偏好文本平台卡给出区间     → 平台卡区间（如番茄 1200–2000）；
//  3. 偏好文本题材卡给出下限     → 下限即目标（上限由区间外检查另判）；
//  4. 全都没有                   → DefaultTargetWords（缺省行为逐字一致）。
//
// 平台卡与题材卡同时存在时平台卡赢（与 BuildEffectiveConstraint 的
// 「平台卡赢在节奏时序」既有纪律一致），题材卡下限仅作 Min 参考不覆盖 Target。
func ResolveChapterWords(profile *PlatformProfile, prefContents []string) ChapterWordsTarget {
	if profile != nil && profile.ChapterLenMin > 0 && profile.ChapterLenMax > profile.ChapterLenMin {
		title := strings.TrimSpace(profile.Title)
		if title == "" {
			title = strings.TrimSpace(profile.Name)
		}
		return ChapterWordsTarget{
			Min:    profile.ChapterLenMin,
			Max:    profile.ChapterLenMax,
			Target: profile.ChapterLenMax,
			Source: "platform_profile",
			Label:  fmt.Sprintf("%s平台预设 %d–%d 字", title, profile.ChapterLenMin, profile.ChapterLenMax),
		}
	}

	var platMin, platMax, genreFloor int
	for _, raw := range prefContents {
		content := strings.TrimSpace(raw)
		if content == "" {
			continue
		}
		if platformCardMarkerRe.MatchString(content) {
			if m := platformChapterWordsRe.FindStringSubmatch(content); m != nil {
				lo, hi := atoiSafe(m[1]), atoiSafe(m[2])
				if lo > 0 && hi > lo {
					platMin, platMax = lo, hi
				}
			}
		}
		if genreCardMarkerRe.MatchString(content) {
			if m := genreFloorWordsRe.FindStringSubmatch(content); m != nil {
				if n := atoiSafe(m[1]); n > 0 {
					genreFloor = n
				}
			}
		}
	}
	if platMin > 0 && platMax > platMin {
		return ChapterWordsTarget{
			Min:    platMin,
			Max:    platMax,
			Target: platMax,
			Source: "platform_card",
			Label:  fmt.Sprintf("平台约束卡 %d–%d 字", platMin, platMax),
		}
	}
	if genreFloor > 0 {
		return ChapterWordsTarget{
			Min:    genreFloor,
			Target: genreFloor,
			Source: "genre_card",
			Label:  fmt.Sprintf("题材约束卡不少于 %d 字", genreFloor),
		}
	}
	return ChapterWordsTarget{
		Target: DefaultTargetWords,
		Source: "default",
		Label:  fmt.Sprintf("系统默认目标 %d 字（未配置平台/题材卡）", DefaultTargetWords),
	}
}

// EffectiveLengthWindow 返回本章生效的章长区间（供 opening_length /
// chapter_length_fit 的确定性判定使用）。无区间返回 (0,0,false)，
// 调用方须回落到各自缺省常量，保持「逐字一致」的缺省行为。
func (t ChapterWordsTarget) EffectiveLengthWindow() (min, max int, ok bool) {
	if t.Min > 0 && t.Max > t.Min {
		return t.Min, t.Max, true
	}
	return 0, 0, false
}

// WindowName 返回发现文案用的口径名（自带「区间」二字，配合
// opening.go 的「超出%s %d–%d 字」）。缺省返回 "七猫区间"，逐字一致。
func (t ChapterWordsTarget) WindowName() string {
	switch t.Source {
	case "platform_profile":
		return "平台区间"
	case "platform_card":
		return "平台约束卡区间"
	case "genre_card":
		return "题材约束卡区间"
	default:
		return "七猫区间"
	}
}

// ShortTitle 返回不带「区间」后缀的简称（配合 platform_checks.go 的
// 「低于%s区间 %d–%d 字」，避免「区间区间」重复）。
func (t ChapterWordsTarget) ShortTitle() string {
	switch t.Source {
	case "platform_profile":
		return "平台"
	case "platform_card":
		return "平台约束卡"
	case "genre_card":
		return "题材约束卡"
	default:
		return "七猫"
	}
}

// EffectiveTarget 返回写作/字数门槛的单点目标（恒 >0）。
func (t ChapterWordsTarget) EffectiveTarget() int {
	if t.Target > 0 {
		return t.Target
	}
	return DefaultTargetWords
}

// SystemLine 生成注入常驻 system 的权威口径行（检测与写作同源的唯一文本来源）。
// 写入侧禁止再自行拼 1000/1500/2000 等数字。
func (t ChapterWordsTarget) SystemLine() string {
	return fmt.Sprintf("- 单章字数：%s（检测 L0 / 修复处方 / 前后对比一律以此为准，勿另按其它数字估算）；字数按 word_count 口径（纯汉字+英文词，不含标点/换行）计。", t.Label)
}
