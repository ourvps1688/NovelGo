//go:build cgo

package mcp_tools

import (
	"context"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/quality"
	"github.com/stretchr/testify/require"
)

// TestBuildChapterCIInstructionCorpusBranch #117 P1：语料内联只替换「检查范围」一段，
// 其余静态契约（检查项/只报告/收敛要求/情绪曲线/方法论/自检/回执）逐字不动；
// 语料为空的降级路径与 #126 输出完全一致。
func TestBuildChapterCIInstructionCorpusBranch(t *testing.T) {
	corpus := "\n\n【本章质检语料（测试）】\n本章正文……\n"
	got := buildChapterCIInstructionWithCorpus(nil, 27, 2, "", corpus)
	require.Contains(t, got, "【本章质检语料")
	require.Contains(t, got, "禁止调用任何工具")
	require.NotContains(t, got, "（get_chapter_list 定位）", "corpus 非空时不得再让模型自取材料")
	for _, want := range []string{
		"检查项：", "只报告不改写：不调用任何写工具", "收敛要求",
		"情绪曲线标注", "最终回复末尾必须附", quality.L1ReceiptFormatLine(),
	} {
		require.Contains(t, got, want, "方案 C 不得冲掉既有静态契约: "+want)
	}
	require.Less(t, strings.Index(got, "【本章质检语料"), strings.Index(got, "检查项："))
	require.Less(t, strings.Index(got, "检查项："), strings.Index(got, "情绪曲线标注"))
	require.Less(t, strings.Index(got, "情绪曲线标注"), strings.Index(got, "最终回复末尾必须附"))

	legacy := buildChapterCIInstruction(nil, 27, 2, "")
	require.Equal(t, legacy, buildChapterCIInstructionWithCorpus(nil, 27, 2, "", ""),
		"corpus 为空时全部文案逐字保持旧口径")
	require.Contains(t, legacy, "get_chapter_list 定位", "降级路径仍允许模型自取")
	require.NotContains(t, legacy, "【本章质检语料")
	require.NotContains(t, legacy, "禁止调用任何工具")

	// 有技能 store：1200 帽方法论与零发现最小集自检块仍在回执契约之前。
	s := newCISkillStore(t)
	full := buildChapterCIInstructionWithCorpus(s, 27, 2, "", corpus)
	require.Contains(t, full, "# 本次任务方法论")
	require.Contains(t, full, "（章节方法论节选，完整以技能原文为准）")
	require.Contains(t, full, "写后自检清单")
	require.Contains(t, full, "【写后自检·review】")
	require.Less(t, strings.Index(full, "# 本次任务方法论"), strings.Index(full, "最终回复末尾必须附"))
	require.Less(t, strings.Index(full, "写后自检清单"), strings.Index(full, "最终回复末尾必须附"))
	require.Contains(t, full, "只报告不改写：不调用任何写工具")
	require.Contains(t, full, "收敛要求")
}

// TestRunChapterCIForChapterInlinesCorpus #117 P1：两条链共用的唯一执行体
// （写后维护 + 手动重检）都经此接线；签名、12 轮上限、回执落库路径均不变。
func TestRunChapterCIForChapterInlinesCorpus(t *testing.T) {
	db := newCICorpusDB(t)
	withCIText(t, ciCorpusBodies(), nil)

	var got SubAgentRequest
	runSub := func(_ context.Context, req SubAgentRequest) (string, error) {
		got = req
		return "小结\n```json\n{\"findings\":[],\"summary\":\"零发现\"}\n```\n", nil
	}
	err := RunChapterCIForChapter(context.Background(), runSub, nil, db, ciCorpusLogger(), "tool-1", 27, 2)
	require.NoError(t, err)
	require.Equal(t, "chapterci", got.AgentType)
	require.Equal(t, chapterCIMaxTurns, got.MaxTurns, "方案 C 不动 12 轮上限")
	require.Equal(t, 12, got.MaxTurns)
	require.Contains(t, got.Instruction, "【本章质检语料")
	require.Contains(t, got.Instruction, "禁止调用任何工具")
	require.Contains(t, got.Instruction, "本章章尾钩子：门外没有人。")
	require.Contains(t, got.Instruction, quality.L1ReceiptFormatLine())
}
