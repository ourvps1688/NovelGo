package pipeline

// P0 WBS T2.2/T2.4：提示词块装配的 golden 对照测试。
//
// 基线来源：改造前（bibleBlock 时代）五提示词构造器的真实输出，
// 已固化于 testdata/p0_baseline_*.txt（每构造器 empty/facts 两份）。
// 断言全部逐字（string equal，非 contains）：
//   ① 仅 Facts 非空 → 输出与改造前逐字一致（降级纪律最高验收标准）；
//   ② 全空 → 输出与改造前逐字一致（提示词与未接任何块逐字一致）；
//   ③ Facts+Voice → 空基线 + 尾部按 Facts→Voice 追加，前文一字不动。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/stretchr/testify/require"
)

// p0FactsMarker 与基线抓取时使用的既定事实正文一致。
const p0FactsMarker = "MARKER-FACTS：左手有疤（快照出处：volume_id=1，snapshot_version=2）"

func readP0Baseline(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "p0_baseline_"+name+".txt"))
	require.NoError(t, err, "缺少 golden 基线 %s（T2.2 改造前输出）", name)
	// 归一化 CRLF→LF：Windows checkout（autocrlf）可能把基线检出为 CRLF，
	// 而构造器输出恒为 LF；逐字比对前统一换行（.gitattributes 另有 eol=lf 双保险）。
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// p0PromptCase 一个提示词构造器的固定输入（与基线抓取时一致）。
type p0PromptCase struct {
	name string
	make func(blocks contextasm.ContextBlocks) string
	// purpose 决定 Voice 引导语（§0.6），须与该构造器内部 Render 的 purpose 一致。
	purpose contextasm.Purpose
}

func p0PromptCases() []p0PromptCase {
	issues := []AuditIssue{{ID: "I1", Level: "P0", Area: "设定自洽", Desc: "冲突"}}
	item := RewriteItem{ID: "I2", Level: "P1", Area: "节奏", Desc: "拖沓"}
	ch := chapter.Chapter{ID: 9, ReadingNumber: 3, Title: "第三章"}
	return []p0PromptCase{
		{"signability", func(b contextasm.ContextBlocks) string {
			return signabilityPrompt(1, "本次体检作用域=《第二卷》（volume_id=2）：只审；", "", b)
		}, contextasm.PurposeAudit},
		{"final_audit", func(b contextasm.ContextBlocks) string {
			return finalAuditPrompt(1, b)
		}, contextasm.PurposeAudit},
		{"fix", func(b contextasm.ContextBlocks) string {
			return fixPrompt(1, issues, "摘要", nil, b)
		}, contextasm.PurposeFix},
		{"chapter_rw", func(b contextasm.ContextBlocks) string {
			return chapterRewritePrompt(1, item, ch, b)
		}, contextasm.PurposeRewrite},
		{"patrol", func(b contextasm.ContextBlocks) string {
			return patrolPrompt(1, b)
		}, contextasm.PurposePatrol},
	}
}

// ① 仅 Facts 非空：输出与改造前逐字一致。
func TestPromptBlocksFactsOnlyMatchesBaseline(t *testing.T) {
	for _, tc := range p0PromptCases() {
		got := tc.make(contextasm.ContextBlocks{Facts: p0FactsMarker})
		require.Equal(t, readP0Baseline(t, tc.name+"_facts"), got,
			"Facts-only 输出须与改造前逐字一致：%s", tc.name)
	}
}

// ② 全空：输出与改造前逐字一致（未接任何块的旧行为）。
func TestPromptBlocksEmptyMatchesBaseline(t *testing.T) {
	for _, tc := range p0PromptCases() {
		got := tc.make(contextasm.ContextBlocks{})
		require.Equal(t, readP0Baseline(t, tc.name+"_empty"), got,
			"空块输出须与改造前逐字一致：%s", tc.name)
	}
}

// ④ Facts+Voice+Intent 三块全非空（T4.2）：尾部严格按 Facts→Voice→Intent 顺序追加。
// §0.5 矩阵（仅 rewrite 取 Intent）由 Assembler.Build 保证（context_blocks_test ③ 锁定），
// 本测试锁定构造器层的三块渲染顺序。
func TestPromptBlocksAllThreeOrderFactsVoiceIntent(t *testing.T) {
	const (
		voice  = "表达指纹：短句、克制、动词开头。"
		intent = "卖点：凡人流修仙，稳健发育。"
	)
	for _, tc := range p0PromptCases() {
		got := tc.make(contextasm.ContextBlocks{Facts: p0FactsMarker, Voice: voice, Intent: intent})
		want := readP0Baseline(t, tc.name+"_empty") +
			"\n\n【已冻结卷既定事实（系列圣经快照）】\n" + p0FactsMarker + "\n" +
			"以上是已冻结（定稿）卷在最近一次冻结时固化的系列圣经，是本次校验/修复的**既定事实依据**：\n" +
			"与之冲突的一律改本卷及后续来适配它（前向修复），**不得**为消解冲突回改已冻结卷；" +
			"与之不冲突的部分以正文与台账工具读到的现状为准。" +
			voiceBlockTail(tc.purpose, voice) +
			"\n\n【创作意图锚定】\n" + intent + "\n" +
			"以上是本书的创作意图与卖点定位，写作与校验以此为方向锚定；与之冲突时以意图为准调整执行细节，不得偏离卖点定位。"
		require.Equal(t, want, got,
			"三块全非空须按 Facts→Voice→Intent 顺序尾部追加：%s", tc.name)
	}
}

// ③ Facts+Voice：前文（空基线）一字不动，尾部按 Facts→Voice 顺序追加，
// 且追加的 Voice 块引导语按 purpose（§0.6）取。
func TestPromptBlocksFactsThenVoiceAppended(t *testing.T) {
	const voice = "表达指纹：短句、克制、动词开头。"
	guideFor := map[contextasm.Purpose]string{
		contextasm.PurposeAudit:  "以下创作心智是本书的文风基准，本轮仅作文风参照（不单独扣分）；修复与校验不得引入与之冲突的文风。",
		contextasm.PurposeFix:    "以下创作心智是本书的文风基准，本轮仅作文风参照（不单独扣分）；修复与校验不得引入与之冲突的文风。",
		contextasm.PurposePatrol: "以下创作心智是本书的文风基准，本轮仅作文风参照（不单独扣分）；修复与校验不得引入与之冲突的文风。",
		contextasm.PurposeRewrite: "以下创作心智是本书的文风基准。重写时保持该叙事习惯、表达指纹与创作决策启发式，" +
			"**不得因重写造成文风漂移**；解决既有问题优先于改写风格。",
	}
	for _, tc := range p0PromptCases() {
		got := tc.make(contextasm.ContextBlocks{Facts: p0FactsMarker, Voice: voice})
		// 追加部分逐字拼出（块格式 golden 在 contextasm 已锁，此处锁 pipeline 组合方式）：
		want := readP0Baseline(t, tc.name+"_empty") +
			"\n\n【已冻结卷既定事实（系列圣经快照）】\n" + p0FactsMarker + "\n" +
			"以上是已冻结（定稿）卷在最近一次冻结时固化的系列圣经，是本次校验/修复的**既定事实依据**：\n" +
			"与之冲突的一律改本卷及后续来适配它（前向修复），**不得**为消解冲突回改已冻结卷；" +
			"与之不冲突的部分以正文与台账工具读到的现状为准。" +
			"\n\n【写作人格 · 文风锚定】\n\n" + guideFor[tc.purpose] + "\n\n" + voice
		require.Equal(t, want, got,
			"Facts+Voice 须空基线+尾部 Facts→Voice 顺序追加：%s", tc.name)
	}
}
