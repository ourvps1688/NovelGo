package quality

// benchmark_book_test.go —— P2-3 对标书库：粘贴解析（字段序/表头跳过/缺特征跳过/
// 同书名后行覆盖）、upsert 幂等（最新粘贴胜）、清单/单删/全清、摘要块渲染（空块
// 零提示/体量封顶/缺位如实标注）。

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBenchmarkBooksTable(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		want    []BenchmarkBookRow
		skipped int
	}{
		{
			name: "全字段竖线分隔",
			text: "全球高武｜重生回归+金手指觉醒｜打脸逆袭｜快节奏连续爆点｜都市高武｜完本标杆",
			want: []BenchmarkBookRow{{
				Title:        "全球高武",
				HookType:     "重生回归+金手指觉醒",
				PleasureType: "打脸逆袭",
				RhythmShape:  "快节奏连续爆点",
				Track:        "都市高武",
				Notes:        "完本标杆",
			}},
		},
		{
			name: "Tab 分隔与列间空字段保位",
			text: "大奉打更人\t悬案开局\t\t长线伏笔慢热",
			want: []BenchmarkBookRow{{
				Title:       "大奉打更人",
				HookType:    "悬案开局",
				RhythmShape: "长线伏笔慢热",
			}},
		},
		{
			name: "两列最小样本（书名+钩子）",
			text: "诡秘之主|世界观悬念开局",
			want: []BenchmarkBookRow{{Title: "诡秘之主", HookType: "世界观悬念开局"}},
		},
		{
			name:    "表头与缺特征行跳过",
			text:    "书名｜钩子类型｜爽点类型｜节奏形态\n某书｜｜｜\n单列书名",
			skipped: 3,
		},
		{
			name:    "超列数行跳过",
			text:    "甲书｜钩｜爽｜节｜赛道｜备注｜多余列",
			skipped: 1,
		},
		{
			name: "同书名后行覆盖",
			text: "甲书|钩A|爽A|节A\n甲书|钩B|爽B|节B",
			want: []BenchmarkBookRow{{Title: "甲书", HookType: "钩B", PleasureType: "爽B", RhythmShape: "节B"}},
		},
		{
			name: "行尾分隔符与首尾空字段",
			text: "乙书｜钩子｜爽点｜｜｜\n｜丙书｜钩子",
			want: []BenchmarkBookRow{
				{Title: "乙书", HookType: "钩子", PleasureType: "爽点"},
				{Title: "丙书", HookType: "钩子"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, skipped := ParseBenchmarkBooks(tt.text)
			assert.Equal(t, tt.want, rows)
			assert.Len(t, skipped, tt.skipped)
		})
	}
}

func TestParseBenchmarkBooksBlankText(t *testing.T) {
	// 空文本报错由服务层拦截；解析层空文本=零行零跳过（纯函数契约）。
	rows, skipped := ParseBenchmarkBooks("   \n\n  ")
	assert.Empty(t, rows)
	assert.Empty(t, skipped)
}

func TestReplaceBenchmarkBooksUpsertListDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const novelID int64 = 901

	require.NoError(t, s.ReplaceBenchmarkBooks(ctx, novelID, []BenchmarkBook{
		{Title: "甲书", HookType: "钩A", PleasureType: "爽A", RhythmShape: "节A"},
		{Title: "乙书", HookType: "钩B"},
	}))
	rows, err := s.ListBenchmarkBooks(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "甲书", rows[0].Title, "按录入顺序（id）升序")

	// 同书名 upsert：最新粘贴胜，其余列覆盖更新。
	require.NoError(t, s.ReplaceBenchmarkBooks(ctx, novelID, []BenchmarkBook{
		{Title: "甲书", HookType: "钩A2", RhythmShape: "节A2"},
	}))
	rows, err = s.ListBenchmarkBooks(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows, 2, "同书名不新增行")
	assert.Equal(t, "钩A2", rows[0].HookType)
	assert.Empty(t, rows[0].PleasureType, "未带列被覆盖为空（整行最新粘贴胜）")
	assert.Equal(t, "节A2", rows[0].RhythmShape)

	// 单删（novel_id 限定）与全清。
	require.NoError(t, s.DeleteBenchmarkBook(ctx, novelID, rows[0].ID))
	rows, err = s.ListBenchmarkBooks(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "乙书", rows[0].Title)

	require.NoError(t, s.DeleteBenchmarkBooks(ctx, novelID))
	rows, err = s.ListBenchmarkBooks(ctx, novelID)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestReplaceBenchmarkBooksRejectsInvalid(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	assert.Error(t, s.ReplaceBenchmarkBooks(ctx, 0, []BenchmarkBook{{Title: "甲", HookType: "钩"}}))
	assert.Error(t, s.ReplaceBenchmarkBooks(ctx, 902, nil), "空行集拒写")
	assert.Error(t, s.ReplaceBenchmarkBooks(ctx, 902, []BenchmarkBook{{HookType: "钩"}}), "缺书名拒写")
	assert.Error(t, s.ReplaceBenchmarkBooks(ctx, 902, []BenchmarkBook{{Title: "甲"}}), "缺结构特征拒写")
	assert.Error(t, s.ReplaceBenchmarkBooks(ctx, 902, []BenchmarkBook{
		{NovelID: 903, Title: "甲", HookType: "钩"},
	}), "novel_id 不一致拒写")
}

func TestRenderBenchmarkBlock(t *testing.T) {
	assert.Empty(t, RenderBenchmarkBlock(nil), "空清单=空串（零提示）")

	block := RenderBenchmarkBlock([]BenchmarkBook{
		{Title: "全球高武", HookType: "重生回归", PleasureType: "打脸逆袭", RhythmShape: "快节奏", Track: "都市高武", Notes: "完本标杆"},
		{Title: "乙书", HookType: "钩B"},
		{Title: "丙书", PleasureType: "爽C"},
	})
	assert.Contains(t, block, "【对标书库摘要】")
	assert.Contains(t, block, "共 3 本")
	assert.Contains(t, block, "- 《全球高武》[都市高武]：钩子=重生回归；爽点=打脸逆袭；节奏=快节奏；备注=完本标杆")
	assert.Contains(t, block, "- 《乙书》：钩子=钩B")
	assert.Contains(t, block, "- 《丙书》：爽点=爽C")
	assert.Contains(t, block, "不得仅凭印象写「对标 XX 书」")

	// 防御渲染：特征全缺位如实标注（存储层已拦截，渲染不编造）。
	def := RenderBenchmarkBlock([]BenchmarkBook{{Title: "丁书"}})
	assert.Contains(t, def, "《丁书》：结构特征未录入")

	// 字段截断如实标注。
	long := RenderBenchmarkBlock([]BenchmarkBook{{Title: strings.Repeat("长", 200), HookType: "钩"}})
	assert.Contains(t, long, "…")
	assert.NotContains(t, long, strings.Repeat("长", 201))
}

func TestRenderBenchmarkBlockCap(t *testing.T) {
	rows := make([]BenchmarkBook, 0, 25)
	for i := 0; i < 25; i++ {
		rows = append(rows, BenchmarkBook{Title: fmt.Sprintf("书%d", i), HookType: "钩"})
	}
	block := RenderBenchmarkBlock(rows)
	assert.Contains(t, block, "共 25 本")
	assert.Contains(t, block, "余 5 本略，体量封顶")
	assert.NotContains(t, block, "《书24》", "超出封顶的样本不渲染")
}
