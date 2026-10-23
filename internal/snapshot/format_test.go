package snapshot

import (
	"strings"
	"testing"
)

// TestFormatDeltaEmpty 验证无差异返回空串（调用方据此识别「无台账变更」）。
func TestFormatDeltaEmpty(t *testing.T) {
	if got := FormatDelta(nil); got != "" {
		t.Errorf("FormatDelta(nil) = %q, want \"\"", got)
	}
	if got := FormatDelta(&Delta{Tables: []TableDelta{}}); got != "" {
		t.Errorf("空差异 = %q, want \"\"", got)
	}
}

// TestFormatDeltaContent 验证留痕摘要渲染：表计数、增删行 id、字段级 old→new
// （字符串加引号、null 显式、字段序稳定）。
func TestFormatDeltaContent(t *testing.T) {
	d := &Delta{
		SchemaVersion: SchemaVersion,
		Tables: []TableDelta{
			{
				Table:   "characters",
				Added:   []map[string]any{{"id": int64(9), "name": "新角色"}},
				Removed: []map[string]any{{"id": int64(3), "name": "退场"}},
				Changed: []RowDelta{
					{ID: 1, Fields: map[string]FieldDelta{
						"alive": {Old: true, New: false},
						"name":  {Old: "陈三斗", New: "陈四斗"},
					}},
				},
			},
			{
				Table:   "locations",
				Changed: []RowDelta{{ID: 2, Fields: map[string]FieldDelta{"controller": {Old: nil, New: "官府"}}}},
			},
		},
	}
	got := FormatDelta(d)
	want := strings.Join([]string{
		"characters: +1 -1 ~1",
		"  + #9",
		"  - #3",
		"  ~ #1 alive: true → false",
		"  ~ #1 name: \"陈三斗\" → \"陈四斗\"",
		"locations: +0 -0 ~1",
		"  ~ #2 controller: null → \"官府\"",
	}, "\n")
	if got != want {
		t.Errorf("FormatDelta mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatDeltaVoiceRowsReadable 验证 voice_dimensions 行变更经通用渲染可读
// （P1 T4.2，WBS §0.5：diff 可读性走既有 FormatDelta，retcon 留痕同款）：
// 表计数、增删行 id、字段级 old→new（字符串加引号、null 显式）与其他台账表同款。
func TestFormatDeltaVoiceRowsReadable(t *testing.T) {
	d := &Delta{
		SchemaVersion: SchemaVersion,
		Tables: []TableDelta{
			{
				Table:   "voice_dimensions",
				Added:   []map[string]any{{"id": int64(7), "dimension": "taboo_words", "content": "禁用突然/忽然"}},
				Removed: []map[string]any{{"id": int64(4), "dimension": "humor", "content": "冷幽默"}},
				Changed: []RowDelta{
					{ID: 3, Fields: map[string]FieldDelta{
						"content": {Old: "先结论后铺垫", New: "先铺垫后结论"},
						"metrics": {Old: nil, New: `{"avg_sent_len":12}`},
					}},
				},
			},
		},
	}
	got := FormatDelta(d)
	want := strings.Join([]string{
		"voice_dimensions: +1 -1 ~1",
		"  + #7",
		"  - #4",
		`  ~ #3 content: "先结论后铺垫" → "先铺垫后结论"`,
		`  ~ #3 metrics: null → "{\"avg_sent_len\":12}"`,
	}, "\n")
	if got != want {
		t.Errorf("FormatDelta mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatDeltaBudget 验证超预算截断（字节预算不切碎 UTF-8）与长值截断。
func TestFormatDeltaBudget(t *testing.T) {
	long := strings.Repeat("长", 100) // 300+ 字节，应截到 maxValRunes=60 rune
	d := &Delta{Tables: []TableDelta{{
		Table:   "characters",
		Changed: []RowDelta{{ID: 1, Fields: map[string]FieldDelta{"voice_traits": {Old: long, New: long + "改"}}}},
	}}}
	got := FormatDelta(d)
	if !strings.Contains(got, "…") {
		t.Errorf("长值应截断加省略号: %q", got)
	}
	// old/new 两个值各截到 maxValRunes=60 rune，「长」最多出现 120 次。
	if c := strings.Count(got, "长"); c > 120 {
		t.Errorf("长值渲染 rune 数 = %d, want <= 120", c)
	}

	// 大差异撑爆预算 → 尾部截断标注。
	big := &Delta{Tables: []TableDelta{{
		Table:   "story_arcs",
		Changed: []RowDelta{},
	}}}
	for i := 0; i < 200; i++ {
		big.Tables[0].Changed = append(big.Tables[0].Changed, RowDelta{
			ID:     int64(i),
			Fields: map[string]FieldDelta{"motif": {Old: strings.Repeat("旧", 20), New: strings.Repeat("新", 20)}},
		})
	}
	out := FormatDelta(big)
	if len(out) <= deltaSummaryBudgetBytes {
		t.Errorf("大差异应按预算截断: len=%d", len(out))
	}
	if !strings.Contains(out, "超预算截断") {
		t.Errorf("应有截断标注: tail=%q", out[len(out)-60:])
	}
}
