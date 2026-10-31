//go:build cgo

package rag

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// newStubReranker 用最小词表构造 OnnxReranker，供 encodePair 单元测试，无需真实模型。
func newStubReranker(t *testing.T) *OnnxReranker {
	t.Helper()
	vocab := map[string]int{
		"[CLS]": 0,
		"[SEP]": 1,
		"[UNK]": 2,
		"你":     3,
		"好":     4,
		"世":     5,
		"界":     6,
	}
	return &OnnxReranker{
		tokenizer: &Tokenizer{vocab: vocab},
		clsID:     0,
		sepID:     1,
	}
}

func TestEncodePair_Structure(t *testing.T) {
	r := newStubReranker(t)
	ids, tids := r.encodePair("你好", "世界")

	// [CLS] 你 好 [SEP] 世 界 [SEP]
	require.Equal(t, []int{0, 3, 4, 1, 5, 6, 1}, ids)
	// token_type_ids：query 段（CLS+你+好+首个SEP）为 0，passage 段（世界+末SEP）为 1
	require.Equal(t, []int{0, 0, 0, 0, 1, 1, 1}, tids)
	require.Len(t, ids, len(tids))
}

func TestEncodePair_TruncationKeepsQuery(t *testing.T) {
	r := newStubReranker(t)
	// passage 远长于上限：会按 bertMaxLen 截断 passage，query 完整保留。
	longPassage := strings.Repeat("世", 600)
	ids, tids := r.encodePair("你", longPassage)

	require.LessOrEqual(t, len(ids), bertMaxLen, "拼接后不得超过模型最大长度")
	require.Equal(t, len(ids), len(tids))
	// query 段保持完整：CLS + 你 + SEP
	require.Equal(t, []int{0, 3, 1}, ids[:3])
	// query 段 type 全 0，passage 段 type 全 1
	require.Equal(t, 0, tids[0])
	require.Equal(t, 0, tids[1])
	require.Equal(t, 0, tids[2])
	require.Equal(t, 1, tids[3])
}

func TestEncodePair_EmptyPassage(t *testing.T) {
	r := newStubReranker(t)
	ids, tids := r.encodePair("你好", "")
	// [CLS] 你 好 [SEP] [SEP]（passage 为空，仍补末位 SEP）
	require.Equal(t, []int{0, 3, 4, 1, 1}, ids)
	require.Equal(t, []int{0, 0, 0, 0, 1}, tids)
}

// TestRerank_NoRerankerFallsBackUnchanged 锁定优雅降级不变量：
// 未部署 reranker 模型时，Rerank 不能改变候选顺序/数量（回落到与历史一致的行为）。
func TestRerank_NoRerankerFallsBackUnchanged(t *testing.T) {
	cands := []SearchResult{
		{ChunkID: "a", Content: "阿尔法", Relevance: 0.9},
		{ChunkID: "b", Content: "贝塔", Relevance: 0.8},
	}
	got := Rerank(context.Background(), "查询", cands, 5)
	require.Len(t, got, 2)
	require.Equal(t, "a", got[0].ChunkID)
	require.Equal(t, "b", got[1].ChunkID)
}

// TestRerank_NoRerankerSortsByRelevance 锁死回落路径的排序语义：
// k < len 时触发 MMR；无 embedding（diversity=0）退化为按 Relevance 降序，与有无 reranker 无关。
func TestRerank_NoRerankerSortsByRelevance(t *testing.T) {
	cands := []SearchResult{
		{ChunkID: "low", Relevance: 0.2},
		{ChunkID: "high", Relevance: 0.9},
		{ChunkID: "mid", Relevance: 0.5},
	}
	got := Rerank(context.Background(), "q", cands, 2) // k < len，触发 MMR
	require.Len(t, got, 2)
	require.Equal(t, "high", got[0].ChunkID)
	require.Equal(t, "mid", got[1].ChunkID)
}

func TestTryGetReranker_NotInitialized(t *testing.T) {
	// 测试未调用 InitReranker 时返回 nil，保证未部署模型不影响运行。
	require.Nil(t, TryGetReranker())
}
