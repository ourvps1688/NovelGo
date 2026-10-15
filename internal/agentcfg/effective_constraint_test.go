package agentcfg

import (
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/preference"
)

// 模拟前端 platformTemplates.ts（番茄）与 genreTemplates.ts（爽文）的 content。
const fanqieContent = `【平台约束·番茄】
签约模型：免费畅读 + 广告/激励，编辑与算法以「前 3–10 章完读率 / 留存」为过签与推量生死线；首章定生死。
节奏参数（覆盖 opening-chapters / pacing-control 默认）：
- 首个冲突钩子窗口 hook_window_open = 300 字（前 300 字内必出第一个冲突/异常/悬念，几乎零铺垫）
- 单章目标字数 = 1200–2000 字
- 章内微钩子间隔 micro_hook_interval = 800 字（每约 800 字一个悬念/进展点，密度高于长付费文）
- 允许纯铺垫章 allow_setup_chapters = 否（每章必须推进剧情或给甜头，零容忍慢章）
- 付费墙 aware = 否（无付费墙，无需墙前强钩）
- 高潮节奏 arc_cadence = 章级（每章小高潮、快回报、情绪即时兑现）
- 核心指标 primary_metric = 完读率 / 留存`

const shuangwenContent = `【创作约束·爽文】
三定律：
1. 主角绝对中心——所有情节、配角、事件都服务于主角的成长、收益或爽感，不得让配角抢戏盖过主角。
2. 有仇不过夜——冲突与屈辱须在当章或最近几章内给出回应/反转，不让主角长期憋屈；拖延即流失爽感。
3. 金手指一致性——主角的能力、外挂、设定前后统一，不得为推进剧情临时无依据开挂或推翻前文设定。

禁忌（违反即劣）：
- 禁虐主：不得长期、无补偿地压制主角。
- 禁现实人名/真实机构：避免直接套用现实人物或单位。
- 禁烂尾/机械降神：结局须由前文伏笔与角色行动收束，不得外力突兀解局。
- 禁吃书：不得推翻已确立的人物关系、世界观或设定。
- 禁元叙述：不得出现"作者/读者/这本小说"等破墙表达。

爽点密度与节奏：
- 每章至少 1 个明确爽点（打脸/收获/反转/成长/被认可）。
- 章末留钩子（悬念、新危机、新目标），促使继续阅读。
- 情绪曲线完整：压抑 → 爆发 → 收获，单章内可见起伏。
- 单章正文不少于 3500 字，冲突推进不拖沓，少无意义日常流水。`

func TestBuildEffectiveConstraint_PlatformWinsChapterConflict(t *testing.T) {
	items := []preference.PreferenceItem{
		{Category: "创作约束", Content: shuangwenContent},
		{Category: "平台约束", Content: fanqieContent},
	}
	out := BuildEffectiveConstraint(items)
	if out == "" {
		t.Fatal("expected non-empty constraint block")
	}
	// 平台卡字数优先，且声明与题材卡下限冲突时以平台为准。
	if !strings.Contains(out, "目标 1200–2000 字（平台卡为准）") {
		t.Errorf("chapter range not resolved to platform: %q", out)
	}
	if !strings.Contains(out, "题材卡下限≥3500字与平台上限冲突") {
		t.Errorf("conflict override note missing: %q", out)
	}
	// 关键节奏旋钮应被抽取。
	for _, want := range []string{"前 300 字内必出钩子", "约每 800 字", "纯铺垫章：否", "付费墙感知：否"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected resolved knob %q in:\n%s", want, out)
		}
	}
	// 必须明确声明题材卡字数下限被本表覆盖。
	if !strings.Contains(out, "一律以本表平台优先为准") {
		t.Errorf("genre floor override sentence missing: %q", out)
	}
}

func TestBuildEffectiveConstraint_GenreOnly(t *testing.T) {
	items := []preference.PreferenceItem{
		{Category: "创作约束", Content: shuangwenContent},
	}
	out := BuildEffectiveConstraint(items)
	if !strings.Contains(out, "题材卡为准，单章不少于 3500 字") {
		t.Errorf("genre-only floor not resolved: %q", out)
	}
	// 无平台卡，不应出现平台旋钮。
	if strings.Contains(out, "付费墙感知") {
		t.Errorf("platform knob should be absent without platform card: %q", out)
	}
}

func TestBuildEffectiveConstraint_EmptyWhenNoCards(t *testing.T) {
	items := []preference.PreferenceItem{
		{Category: "风格", Content: "主角话少，冷峻。"},
		{Category: "平台约束", Content: "用户手写的平台相关自由文本，无 marker"},
	}
	if got := BuildEffectiveConstraint(items); got != "" {
		t.Errorf("expected empty when no template cards, got: %q", got)
	}
}

func TestBuildEffectiveConstraint_NoConflictWhenGenreBelowPlatformMax(t *testing.T) {
	// 起点：3000–4000；若题材卡下限 3000，不触发冲突注记。
	const qidian = "【平台约束·起点】\n- 单章目标字数 = 3000–4000 字\n"
	const suspense = "【创作约束·悬疑】\n- 单章正文不少于 3000 字，信息密度高，少闲笔。"
	items := []preference.PreferenceItem{
		{Category: "创作约束", Content: suspense},
		{Category: "平台约束", Content: qidian},
	}
	out := BuildEffectiveConstraint(items)
	if !strings.Contains(out, "目标 3000–4000 字（平台卡为准）") {
		t.Errorf("chapter range not resolved: %q", out)
	}
	if strings.Contains(out, "与平台上限冲突") {
		t.Errorf("should NOT flag conflict when genre floor <= platform max: %q", out)
	}
}
