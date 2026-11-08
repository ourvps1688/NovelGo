package pipeline

// ── #105 字数口径回归 ─────────────────────────────────────────────────────
//
// 事故：I1「第2章1799字超出黄金三章1000-1500字章长口径」修复后正文 1430 汉字
// 已达标，但复验语料只给正文原文、不给权威字数且禁工具，模型自行数「含标点字符」
// 得 1696（1430 汉字 + 266 标点/换行），误判超标并驳回两次。
//
// 本文件固定三道防线：① 工单处方区间解析；② L0 快路径本地确定性复验（零 profile
// 配置也拦得住）；③ 两条语料路径都显式标注权威 word_count 并声明口径。

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/rag"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
)

const issue105Desc = "第2章1799字超出黄金三章1000-1500字章长口径（第1章1450、第3章1497达标），触发七猫驳回实证硬节点；前向适配：压缩第2章正文至1000-1500字，不回改第1、3章"

// TestParseChapterLengthWindow 处方区间解析：真实工单文案命中、章号不误判、非法区间降级。
func TestParseChapterLengthWindow(t *testing.T) {
	lo, hi, ok := parseChapterLengthWindow(issue105Desc)
	require.True(t, ok)
	require.Equal(t, 1000, lo)
	require.Equal(t, 1500, hi)

	for _, desc := range []string{
		"第1章1450、第3章1497达标", // 章号+字数，无区间连接符
		"开篇钩子铺垫不足",
		"压缩至1500-1000字", // 反向区间
		"压缩至0-1500字",
	} {
		_, _, ok := parseChapterLengthWindow(desc)
		require.False(t, ok, "不应解析出区间: %q", desc)
	}
}

// TestClassifyMechanicalChecksOpeningLength 黄金三章章长应映射到 opening_length，
// 且不能同时挂 chapter_length_fit（后者作用域是黄金三章外全书章，且依赖 profile）。
func TestClassifyMechanicalChecksOpeningLength(t *testing.T) {
	checks, ambiguous := classifyMechanicalChecks("开篇过签力", issue105Desc, "body")
	require.False(t, ambiguous)
	require.True(t, checks[quality.CheckOpeningLength], "应识别为黄金三章章长")
	require.False(t, checks[quality.CheckChapterLengthFit], "不应再挂依赖 profile 的平台章长适配")

	// 非黄金三章的平台章长工单仍走原映射。
	checks, _ = classifyMechanicalChecks("章长不适配", "章长超出平台区间", "body")
	require.True(t, checks[quality.CheckChapterLengthFit])
	require.False(t, checks[quality.CheckOpeningLength])
}

// TestTryL0VerifyOpeningLengthLocally 零 checker 配置下也能按处方区间本地判定：
// 达标自动通过、超标明确驳回、正文不可读保守回落 LLM；全程免 LLM 数字符。
func TestTryL0VerifyOpeningLengthLocally(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	require.NoError(t, db.Create(&chapter.Chapter{NovelID: 1, SortOrder: 1, Title: "第一章"}).Error)

	// 1430 个汉字 + 200 个标点：含标点共 1630 字符，但 word_count 口径 = 1430（达标）。
	// 正是事故里被误算成「约1690字」的那类正文。
	inRange := strings.Repeat("文", 1430) + strings.Repeat("，", 200)
	overRange := strings.Repeat("文", 1799) + strings.Repeat("，", 200)
	row := PipelineIssue{Area: "开篇过签力", Desc: issue105Desc, ChapterIDsJSON: "[1]"}

	decided, verified, note := svc.tryL0Verify(ctx, 1, "r1", row,
		func(_, _ int64) (string, bool) { return inRange, true })
	require.True(t, decided, "零 profile 配置也应确定性判定（#105 回归）")
	require.True(t, verified, "1430 汉字落在 1000–1500 区间应自动确认通过")
	require.Contains(t, note, "word_count=1430", "回执应带权威字数口径说明")
	require.Contains(t, note, "标点/换行不计")

	decided, verified, note = svc.tryL0Verify(ctx, 1, "r1", row,
		func(_, _ int64) (string, bool) { return overRange, true })
	require.True(t, decided)
	require.False(t, verified, "1799 汉字仍超区间应明确驳回")
	require.Contains(t, note, "word_count=1799")
	require.Contains(t, note, "退回待修")

	// 正文不可读 → 不臆测，回落原 LLM 路径。
	decided, _, _ = svc.tryL0Verify(ctx, 1, "r1", row,
		func(_, _ int64) (string, bool) { return "", false })
	require.False(t, decided, "正文不可读应保守回落")

	// 无区间可解析 → 不启用本地区间判定（回落既有门控/LLM）。
	decided, _, _ = svc.tryL0Verify(ctx, 1, "r1",
		PipelineIssue{Area: "开篇过签力", Desc: "黄金三章章长需调整", ChapterIDsJSON: "[1]"},
		func(_, _ int64) (string, bool) { return inRange, true })
	require.False(t, decided, "无处方区间且无 profile 配置应回落 LLM")
}

// TestVerifyCorpusLabelsAuthoritativeWordCount 窗口语料必须标注权威字数（现算
// ComputeStats，非含标点字符数），并声明台账/标点不得计入。
func TestVerifyCorpusLabelsAuthoritativeWordCount(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return strings.Repeat("文", 1430) + strings.Repeat("，", 200), nil
	})
	src := verifyCorpusSource{
		byReading: map[int64]chapter.Chapter{1: {ID: 101, ReadingNumber: 1, Title: "第一章", WordCount: 1430}},
		wordCount: map[int64]int{},
	}
	var sb strings.Builder
	svc.appendChapterWindows(&sb, context.Background(), 901, src, []int64{1}, verifyChapterCap, verifyCorpusCap)
	got := sb.String()
	require.Contains(t, got, "权威字数 1430 字", "应标注纯汉字口径的权威字数，而非 1630 字符")
	require.Contains(t, got, "不含标点/换行")
	require.Equal(t, 1430, src.wordCount[1], "现算字数应回写共享缓存供 RAG 路径复用")

	prompt := verifyPrompt(901, nil, nil, "", contextasm.ContextBlocks{})
	require.Contains(t, prompt, "字数口径")
	require.Contains(t, prompt, "禁止自行数语料字符")
}

// TestVerifyEvidenceLabelsAuthoritativeWordCount RAG 证据切片路径同样标注权威字数，
// 并显式澄清 @字符偏移是含标点的位置坐标、不是字数（事故里 1413/char1477 的来源）。
func TestVerifyEvidenceLabelsAuthoritativeWordCount(t *testing.T) {
	src, svc := retrievalFixture(t)
	src.wordCount = map[int64]int{1: 1430, 2: 0}
	fake := &fakeVerifySearcher{byChapter: map[int64][]rag.SearchResult{
		101: {{ChunkID: "c-1", Content: "结尾段证据", ChapterID: 101, StartRunePos: 1413}},
	}}
	got := svc.buildIssueEvidence(context.Background(), 901, src, fake,
		verifyTarget{ID: "I1", Area: "开篇过签力", Desc: issue105Desc, Chapters: []int64{1}})
	require.Contains(t, got, "第1章权威字数 1430 字")
	require.Contains(t, got, "不含标点/换行")
	require.Contains(t, got, "@字符偏移 1413")
	require.Contains(t, got, "非字数坐标", "偏移必须显式声明不是字数坐标")
}
