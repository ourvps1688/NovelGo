package quality

// platform_checks.go —— 平台风格 profile 驱动的 L0 确定性检查（P0-1）。
//
// 编辑反馈回流（P1-1）沉淀的平台口径从文案约定升格为机器可检参数后的三项确定性检查
// （验收口径：七猫 profile 下前 500 字爆点/章长/爽点密度三项 L0 可检，不达标出 finding）：
//   - opening_hook：开篇爆点/悬念信号密度（前 200 字 + 前 500 字双窗口，近似词表口径）；
//   - chapter_length_fit：黄金三章外全书章长贴合平台区间（黄金三章由 opening_length 覆盖，零重叠）；
//   - beat_density：爽点标记密度下限（次/章，近似词表口径，P0-2 张力曲线升级前的近似实现）。
//
// 缺省纪律：profile 未配置=平台三查不跑、密度下限为 0=beat_density 不跑
// （缺省=现行行为逐字一致）。词表保守高精度（tailHookSignals 同风格）：近似口径漏报无害、
// 误报以粗粒度复验回落兜底（pipeline.coarseHeuristicChecks 含 opening_hook/beat_density）。

import (
	"fmt"
	"strings"
)

// openingHookSignals 是开篇爆点/悬念/异常事件信号词（保守表，宁缺勿错）。
var openingHookSignals = []string{
	// 冲突
	"死了", "杀了", "尸体", "凶手", "追杀", "逃命", "灭门", "重伤", "死了人", "血",
	// 悬念
	"忽然", "骤然", "蓦地", "竟然", "居然", "没想到", "不料", "诡异", "不对劲", "秘密", "警告",
	// 异常事件
	"穿越", "重生", "醒来", "爆炸", "坠落", "失踪", "消失", "异变",
}

// beatSignals 是爽点标记词（密度近似词表：打脸/逆袭/高光时刻的确定性代理，
// P0-2 张力曲线数据源落地后升级）。
var beatSignals = []string{
	"打脸", "逆袭", "碾压", "秒杀", "完虐", "震惊", "目瞪口呆", "倒吸一口凉气",
	"不可能", "突破", "觉醒", "获得", "奖励", "越级", "首杀",
}

// platformFindings 平台三查（P0-1）：opening_hook/chapter_length_fit 由 profile 武装，
// beat_density 由生效密度下限（L0Input.BeatDensityMin，CheckerConfig.EffectiveBeatDensityMin 口径）武装。
// 未配置=对应检查自然跳过零发现（缺省=现行行为逐字一致）。
func platformFindings(in L0Input, wordCount int) []Finding {
	if strings.TrimSpace(in.Text) == "" || in.ReadingNumber <= 0 {
		return nil
	}
	var out []Finding
	if p := in.Profile; p != nil {
		if f := openingHookFinding(in.Text, p); f != nil {
			out = append(out, *f)
		}
		// 黄金三章（1..OpeningChapterWindow）由 opening_length 覆盖，此处只查其余章（零重叠）。
		if p.OpeningChapterWindow > 0 && in.ReadingNumber > p.OpeningChapterWindow {
			if f := chapterLengthFitFinding(wordCount, p); f != nil {
				out = append(out, *f)
			}
		}
	}
	if in.BeatDensityMin > 0 {
		if f := beatDensityFinding(in.Text, in.BeatDensityMin); f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// openingHookFinding 开篇爆点密度（每章）：前 HookEarlyWindow 字信号数 < HookEarlyMin
// 或前 HookWindow 字信号数 < HookMin → 合并单条发现（双窗口都报，便于定点补爆点）。
// 窗口/下限 <=0 的一侧不设限；双侧都未设 → nil。
func openingHookFinding(body string, p *PlatformProfile) *Finding {
	earlyArmed := p.HookEarlyWindow > 0 && p.HookEarlyMin > 0
	fullArmed := p.HookWindow > 0 && p.HookMin > 0
	if !earlyArmed && !fullArmed {
		return nil
	}
	early := 0
	if earlyArmed {
		early = countSignals(firstRunes(body, p.HookEarlyWindow), openingHookSignals)
	}
	full := 0
	if fullArmed {
		full = countSignals(firstRunes(body, p.HookWindow), openingHookSignals)
	}
	failEarly := earlyArmed && early < p.HookEarlyMin
	failFull := fullArmed && full < p.HookMin
	if !failEarly && !failFull {
		return nil
	}
	detail := make([]string, 0, 2)
	if earlyArmed {
		detail = append(detail, fmt.Sprintf("前 %d 字命中 %d 处爆点/悬念信号（下限 %d）",
			p.HookEarlyWindow, early, p.HookEarlyMin))
	}
	if fullArmed {
		detail = append(detail, fmt.Sprintf("前 %d 字命中 %d 处（下限 %d）",
			p.HookWindow, full, p.HookMin))
	}
	return &Finding{
		Check: CheckOpeningHook,
		Desc: fmt.Sprintf("开篇爆点不足：%s——开篇切入点缺吸引力是签约驳回高频签名，建议以冲突/悬念/异常事件开场",
			strings.Join(detail, "，")),
	}
}

// chapterLengthFitFinding 章长贴合平台区间（黄金三章外全书章）：
// ChapterLenMin/Max <=0 的一侧不设限；双侧都未设 → nil。
func chapterLengthFitFinding(wordCount int, p *PlatformProfile) *Finding {
	if p.ChapterLenMin <= 0 && p.ChapterLenMax <= 0 {
		return nil
	}
	lo, hi := p.ChapterLenMin, p.ChapterLenMax
	if lo > 0 && wordCount < lo {
		return &Finding{
			Check: CheckChapterLengthFit,
			Desc: fmt.Sprintf("章长不适配平台区间：本章 %d 字，低于%s区间 %d–%d 字（正文过短易被判信息量不足）",
				wordCount, p.Title, lo, hi),
		}
	}
	if hi > 0 && wordCount > hi {
		return &Finding{
			Check: CheckChapterLengthFit,
			Desc: fmt.Sprintf("章长不适配平台区间：本章 %d 字，超出%s区间 %d–%d 字（正文过长易被判节奏拖）",
				wordCount, p.Title, lo, hi),
		}
	}
	return nil
}

// beatDensityFinding 爽点标记密度（每章）：全文爽点标记词命中数 < 下限 → 发现。
// 近似词表口径（P0-2 张力曲线升级前的代理），漏报无害、误报由粗粒度复验回落兜底。
func beatDensityFinding(body string, min float64) *Finding {
	n := countSignals(body, beatSignals)
	if float64(n) >= min {
		return nil
	}
	return &Finding{
		Check: CheckBeatDensity,
		Desc: fmt.Sprintf("爽点标记密度不足：本章命中 %d 处爽点标记词，低于下限 %.1f 次/章（近似词表口径）——爽点稀疏易被判平淡/看点不足",
			n, min),
	}
}

// countSignals 统计信号词在文本中的出现次数（近似口径：子串计数求和）。
func countSignals(windowText string, signals []string) int {
	total := 0
	for _, s := range signals {
		total += strings.Count(windowText, s)
	}
	return total
}

// firstRunes 返回字符串前 n 个 rune（n<=0 返回空串）。
func firstRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
