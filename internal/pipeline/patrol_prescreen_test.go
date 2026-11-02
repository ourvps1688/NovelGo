package pipeline

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ── 句子切分 ──

func TestSplitPrescreenSentences(t *testing.T) {
	got := splitPrescreenSentences("他失联了十一天。天空是蓝色的！这件事很奇怪？今天天气晴朗")
	require.Len(t, got, 4)
	require.Equal(t, "他失联了十一天", got[0])
	require.Equal(t, "天空是蓝色的", got[1])
	require.Equal(t, "这件事很奇怪", got[2])
	require.Equal(t, "今天天气晴朗", got[3])

	// 换行与分号也算句界
	got = splitPrescreenSentences("第一句。第二句\n第三句；第四句")
	require.Len(t, got, 4)

	// 空 / 纯空白
	require.Empty(t, splitPrescreenSentences(""))
	require.Empty(t, splitPrescreenSentences("   "))
}

// ── 候选抽取（仅疑似主张句）──

func TestExtractPrescreenCands(t *testing.T) {
	svc := NewService(&gorm.DB{}, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.chapterTextReader = func(_ context.Context, _ int64, id int64) (string, error) {
		switch id {
		case 1:
			return "他失联了十一天才被找到。天空是蓝色的。", nil
		case 2:
			return "三个月后他才恢复。这件事很奇怪。", nil
		}
		return "", nil
	}
	chapters := []chapter.Chapter{{ID: 1, ReadingNumber: 1}, {ID: 2, ReadingNumber: 2}}

	cands := svc.extractPrescreenCands(context.Background(), 1, chapters)

	// 仅「十一天」「三个月」是主张句；天空蓝 / 这件事奇怪 被丢弃。
	require.Len(t, cands, 2)
	require.Equal(t, 1, cands[0].ReadingNumber)
	require.Contains(t, cands[0].Values, "十一天")
	require.Equal(t, 2, cands[1].ReadingNumber)
	require.Contains(t, cands[1].Values, "三个月")
}

// ── 中文数字解析 ──

func TestCnToFloat(t *testing.T) {
	cases := map[string]float64{
		"140": 140,
		"3.5": 3.5,
		"三":   3,
		"两":   2,
		"半":   0.5,
		"十一":  11,
		"二十三": 23,
		"三百":  300,
		"三千":  3000,
		"两万":  20000,
	}
	for in, want := range cases {
		got, ok := cnToFloat(in)
		require.True(t, ok, "cnToFloat(%q) should parse", in)
		require.InDelta(t, want, got, 1e-9, "cnToFloat(%q)", in)
	}
	_, ok := cnToFloat("abc")
	require.False(t, ok)
}

// ── 分歧检测 ──

func TestDetectDivergence(t *testing.T) {
	require.True(t, detectDivergence([]string{"140秒", "180秒"}), "同单位不同数值 → 分歧")
	require.False(t, detectDivergence([]string{"三个月", "三个月"}), "同值 → 不分歧")
	require.False(t, detectDivergence([]string{"三个月", "90天"}), "不同单位 → 不判分歧（语义聚类仍会分组）")
}

// ── 聚类 + 分歧（注入 stub embedder）──

// stubPSEmbedder 返回受控向量：含「秒」的句归到 topic 1，其余归到 topic 7（正交）。
type stubPSEmbedder struct {
	dim int
	vec func(string) []float32
}

func (s stubPSEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = s.vec(t)
	}
	return out, nil
}
func (s stubPSEmbedder) Dim() int { return s.dim }

func unitVec(dim, idx int) []float32 {
	v := make([]float32, dim)
	v[idx] = 1
	return v
}

func TestClusterPrescreenCandsDivergence(t *testing.T) {
	old := patrolEmbedderProvider
	patrolEmbedderProvider = func() prescreenEmbedder {
		return stubPSEmbedder{dim: 512, vec: func(t string) []float32 {
			if strings.Contains(t, "秒") {
				return unitVec(512, 1)
			}
			return unitVec(512, 7)
		}}
	}
	defer func() { patrolEmbedderProvider = old }()

	cands := []PrescreenCand{
		{ReadingNumber: 3, Sentence: "每章约140秒", Values: []string{"140秒"}},
		{ReadingNumber: 10, Sentence: "每章约180秒", Values: []string{"180秒"}},
		{ReadingNumber: 5, Sentence: "今天天气晴朗", Values: nil}, // 不同 topic，应被滤为单条不成簇
	}

	clusters, err := clusterPrescreenCands(context.Background(), patrolEmbedderProvider(), cands)
	require.NoError(t, err)
	// 仅「秒」两条同 topic 跨章成簇；天气句正交被丢弃。
	require.Len(t, clusters, 1)
	c := clusters[0]
	require.Len(t, c.Cands, 2)
	require.True(t, c.Divergent, "140秒 vs 180秒 应判分歧")
	require.InDelta(t, 1.0, c.Sim, 1e-6)
	require.Equal(t, 2, countChapters(c.Cands))
}

func TestClusterPrescreenCandsNoDivergence(t *testing.T) {
	old := patrolEmbedderProvider
	patrolEmbedderProvider = func() prescreenEmbedder {
		return stubPSEmbedder{dim: 512, vec: func(string) []float32 { return unitVec(512, 1) }}
	}
	defer func() { patrolEmbedderProvider = old }()

	cands := []PrescreenCand{
		{ReadingNumber: 3, Sentence: "他在三个月后归来", Values: []string{"三个月"}},
		{ReadingNumber: 10, Sentence: "他在三个月后离开", Values: []string{"三个月"}},
	}
	clusters, err := clusterPrescreenCands(context.Background(), patrolEmbedderProvider(), cands)
	require.NoError(t, err)
	require.Len(t, clusters, 1)
	require.False(t, clusters[0].Divergent, "同值不判分歧")
}

// ── 报告渲染 ──

func TestFormatPrescreen(t *testing.T) {
	require.Equal(t, "", formatPrescreen(PrescreenReport{Ran: false}))
	require.Equal(t, "", formatPrescreen(PrescreenReport{Ran: true})) // 无簇 → 空

	r := PrescreenReport{Ran: true, Clusters: []PrescreenCluster{
		{
			Cands:     []PrescreenCand{{ReadingNumber: 3, Sentence: "每章约140秒"}, {ReadingNumber: 10, Sentence: "每章约180秒"}},
			Sim:       1.0,
			Divergent: true,
			Values:    []string{"140秒", "180秒"},
		},
	}}
	out := formatPrescreen(r)
	require.Contains(t, out, "预筛候选矛盾点")
	require.Contains(t, out, "【数值分歧】")
	require.Contains(t, out, "第3章")
	require.Contains(t, out, "第10章")
}

// ── 预筛编排：无 embedder / 无章节 降级 ──

func TestRunPatrolPrescreenNoEmbedder(t *testing.T) {
	old := patrolEmbedderProvider
	patrolEmbedderProvider = func() prescreenEmbedder { return nil }
	defer func() { patrolEmbedderProvider = old }()

	svc := NewService(&gorm.DB{}, nil, nil, nil, "", "", context.Background(), slog.Default())
	r := svc.runPatrolPrescreen(context.Background(), 1, []chapter.Chapter{{ID: 1, ReadingNumber: 1}})
	require.False(t, r.Ran)
	require.NotEmpty(t, r.Reason)
}

func TestRunPatrolPrescreenNoChapters(t *testing.T) {
	old := patrolEmbedderProvider
	patrolEmbedderProvider = func() prescreenEmbedder {
		return stubPSEmbedder{dim: 512, vec: func(string) []float32 { return unitVec(512, 1) }}
	}
	defer func() { patrolEmbedderProvider = old }()

	svc := NewService(&gorm.DB{}, nil, nil, nil, "", "", context.Background(), slog.Default())
	r := svc.runPatrolPrescreen(context.Background(), 1, nil)
	require.False(t, r.Ran)
}
