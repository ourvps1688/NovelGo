package pipeline

// ── RAG-Verify（2026-11-05）复验向量检索证据装配回归 ─────────────────────
//
// 契约：逐工单证据切片（作用域章内向量命中段 + 引用台账定点行）；检索零命中/
// 向量库缺失回退窗口语料，绝不阻断；单批/单工单证据帽生效；禁工具引导照常。

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/rag"
	"github.com/stretchr/testify/require"
)

// fakeVerifySearcher 按作用域章 ID 返回固定命中段（global 供无作用域检索），可注入错误。
type fakeVerifySearcher struct {
	byChapter map[int64][]rag.SearchResult
	global    []rag.SearchResult
	err       error
}

func (f *fakeVerifySearcher) SearchBatch(_ context.Context, _ int64, _ []string, _ int, filter *rag.SearchFilter) ([][]rag.SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if filter != nil && len(filter.ChapterIDs) > 0 {
		res := f.byChapter[filter.ChapterIDs[0]]
		// 两条查询（主张+修复）返回同组命中，验证去重。
		return [][]rag.SearchResult{res, res}, nil
	}
	return [][]rag.SearchResult{f.global, f.global}, nil
}

// retrievalFixture 构造带章索引/台账快照/正文 stub 的复验语料环境。
func retrievalFixture(t *testing.T) (verifyCorpusSource, *Service) {
	t.Helper()
	src := verifyCorpusSource{byReading: map[int64]chapter.Chapter{
		1: {ID: 101, ReadingNumber: 1, Title: "第一章"},
		2: {ID: 102, ReadingNumber: 2, Title: "第二章"},
		3: {ID: 103, ReadingNumber: 3, Title: "第三章"},
	},
		ledger: "#### characters（2 条）\n- id=1903; name=林川\n- id=1904; name=苏瑶\n",
	}
	svc := NewService(newTestDB(t), nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.SetChapterTextReader(func(_ context.Context, _ int64, chapterID int64) (string, error) {
		return fmt.Sprintf("正文-%d", chapterID), nil
	})
	return src, svc
}

// TestVerifyEvidenceRetrievalPerIssueSections 验收 RAG-Verify 主契约：逐工单证据切片
// （命中段按章序排序、双查询命中去重、台账引用定点行替代整表快照）。
func TestVerifyEvidenceRetrievalPerIssueSections(t *testing.T) {
	src, svc := retrievalFixture(t)

	fake := &fakeVerifySearcher{byChapter: map[int64][]rag.SearchResult{
		101: {{ChunkID: "c-1", Content: "命中段落甲", ChapterID: 101, StartRunePos: 30}},
		102: {{ChunkID: "c-2", Content: "命中段落乙", ChapterID: 102, StartRunePos: 10}},
	}}
	old := verifySearcherProvider
	verifySearcherProvider = func() verifyEvidenceSearcher { return fake }
	defer func() { verifySearcherProvider = old }()

	targets := []verifyTarget{
		{ID: "I1", Area: "情节", Desc: "前后矛盾，涉及 character#1903", Chapters: []int64{1, 2}, FixNote: "已统一设定"},
	}
	got := svc.buildVerifyEvidenceBatch(context.Background(), 901, src, targets)

	require.Contains(t, got, "【本次复验语料", "语料块头保持（契约兼容）")
	require.Contains(t, got, "禁止调用任何工具", "禁工具引导保持")
	require.Contains(t, got, "工单 I1 的证据切片", "应有逐工单证据小节")
	require.Contains(t, got, "命中段落甲")
	require.Contains(t, got, "命中段落乙")
	require.Contains(t, got, "第1章《第一章》片段", "命中段应带章定位")
	// 双查询（主张+修复）命中同一 chunk → 去重后只出现一次。
	require.Equal(t, 1, strings.Count(got, "命中段落甲"), "chunk 应按 ID 去重")
	// 台账引用定点：只保留 id=1903 行及其表头，不带无关 id=1904。
	require.Contains(t, got, "id=1903")
	require.NotContains(t, got, "id=1904", "未引用台账行不应进入证据")
}

// TestVerifyEvidenceFallbacks 验收降级链：向量库缺失→整体窗口语料；检索零命中→
// 该工单窗口兜底；检索报错→同样兜底。任何情况都内联可用证据、不阻断。
func TestVerifyEvidenceFallbacks(t *testing.T) {
	src, svc := retrievalFixture(t)
	targets := []verifyTarget{
		{ID: "I1", Area: "情节", Desc: "前后矛盾", Chapters: []int64{1}},
	}

	// ① 向量库缺失：整体降级窗口语料（与旧 buildVerifyCorpus 行为一致）。
	old := verifySearcherProvider
	defer func() { verifySearcherProvider = old }()
	verifySearcherProvider = func() verifyEvidenceSearcher { return nil }
	got := svc.buildVerifyEvidenceBatch(context.Background(), 901, src, targets)
	require.Contains(t, got, "第1章", "降级窗口语料应含作用域章")
	require.Contains(t, got, "正文-", "降级窗口语料应含章正文")
	require.NotContains(t, got, "证据切片", "无向量库不应出现检索切片标记")

	// ② 检索零命中：该工单窗口兜底。
	verifySearcherProvider = func() verifyEvidenceSearcher { return &fakeVerifySearcher{} }
	got = svc.buildVerifyEvidenceBatch(context.Background(), 901, src, targets)
	require.Contains(t, got, "向量检索零命中，作用域章窗口兜底")
	require.Contains(t, got, "正文-", "兜底应内联章正文窗口")

	// ③ 检索报错：同样窗口兜底（错误仅降级，绝不阻断）。
	verifySearcherProvider = func() verifyEvidenceSearcher {
		return &fakeVerifySearcher{err: fmt.Errorf("sqlite-vec 不可用")}
	}
	got = svc.buildVerifyEvidenceBatch(context.Background(), 901, src, targets)
	require.Contains(t, got, "向量检索零命中，作用域章窗口兜底")
	require.Contains(t, got, "正文-")
}

// TestVerifyEvidenceCaps 验收证据体量纪律：单段截断保头、单工单帽生效，远小于
// 旧窗口语料的有效体量（40k 字节≈13k 字符）。
func TestVerifyEvidenceCaps(t *testing.T) {
	src, svc := retrievalFixture(t)

	huge := strings.Repeat("啊", verifyEvidenceChunkCap*2)
	fake := &fakeVerifySearcher{byChapter: map[int64][]rag.SearchResult{
		101: {{ChunkID: "big", Content: huge, ChapterID: 101, StartRunePos: 0}},
	}}
	old := verifySearcherProvider
	verifySearcherProvider = func() verifyEvidenceSearcher { return fake }
	defer func() { verifySearcherProvider = old }()

	targets := []verifyTarget{
		{ID: "I1", Area: "情节", Desc: "前后矛盾", Chapters: []int64{1}},
	}
	got := svc.buildVerifyEvidenceBatch(context.Background(), 901, src, targets)
	require.Contains(t, got, "啊", "命中段应内联")
	require.Less(t, len([]rune(got)), len([]rune(huge)), "超长 chunk 应被单段帽截断")
	require.LessOrEqual(t, len([]rune(got)), verifyEvidencePerIssueCap+300, "单工单证据应在帽内（含少量标注）")
}

// TestExtractLedgerRefIDsAndFilter 纯函数边界：#\d+ 提取（章#N 剔除）、id 字段精确
// 匹配（novel_id 不误匹配）、表头随首个命中行输出。
func TestExtractLedgerRefIDsAndFilter(t *testing.T) {
	ids := extractLedgerRefIDs("情节：见 character#1903 与 #1905", "章#12 不是台账")
	require.True(t, ids["1903"])
	require.True(t, ids["1905"])
	require.False(t, ids["12"], "章#N 是章节引用，不得进台账引用集")

	ledger := "#### characters（2 条）\n- id=1903; name=林川\n- id=1904; name=苏瑶\n#### locations（1 条）\n- id=1905; name=青云宗\n"
	got := filterLedgerByRefs(ledger, ids)
	require.Contains(t, got, "#### characters")
	require.Contains(t, got, "id=1903; name=林川")
	require.Contains(t, got, "#### locations")
	require.Contains(t, got, "id=1905")
	require.NotContains(t, got, "苏瑶", "未引用行应丢弃")

	// novel_id 值命中陷阱：id=1904 行带 novel_id=1903 时，引用 1903 不得命中该行。
	got = filterLedgerByRefs("- id=1904; novel_id=1903\n", map[string]bool{"1903": true})
	require.Empty(t, got, "novel_id 值不得被当作 id 字段匹配")

	require.Empty(t, filterLedgerByRefs(ledger, nil))
	require.Empty(t, filterLedgerByRefs("", map[string]bool{"1": true}))
}
