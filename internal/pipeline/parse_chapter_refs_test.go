package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseChapterRefs_LedgerRefsNotChapters 回归：台账实体引用 `<type>#<id>`
// （如 timeline#42 / reader_perspective#1643）绝不可被误判为章节号。
// 这是签约体检工单「涉及章节」出现上千编号（实为台账自增主键）的根因修复。
func TestParseChapterRefs_LedgerRefsNotChapters(t *testing.T) {
	cases := []struct {
		name     string
		area     string
		desc     string
		expected []int64
	}{
		{
			name:     "纯台账引用不应产出章节",
			area:     "长线牵引",
			desc:     "timeline#42 伏笔未回收",
			expected: nil,
		},
		{
			name:     "reader_perspective 多台账引用不应产出章节",
			area:     "读者视角",
			desc:     "reader_perspective#1643 与 reader_perspective#1653 认知矛盾",
			expected: nil,
		},
		{
			name:     "章节与台账混排只取真实章节",
			area:     "读者视角",
			desc:     "第7章 reader_perspective#1643 未回收",
			expected: []int64{7},
		},
		{
			name:     "area 里的台账引用也不应产出章节",
			area:     "读者视角 reader_perspective#1617",
			desc:     "第3章与已揭示认知冲突",
			expected: []int64{3},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseChapterRefs(c.area, c.desc)
			assert.Equal(t, c.expected, got)
		})
	}
}

// TestParseChapterRefs_ChapterRefs 确认正常的章节引用写法仍被正确解析。
func TestParseChapterRefs_ChapterRefs(t *testing.T) {
	assert.Equal(t, []int64{7}, parseChapterRefs("", "第7章节奏断档"))
	assert.Equal(t, []int64{6, 7, 8, 9, 10}, parseChapterRefs("", "第6-10章爽点缺失"))
	assert.Equal(t, []int64{12}, parseChapterRefs("文风漂移·章节#12", "叙事腔调偏离契约"))
	assert.Equal(t, []int64{3}, parseChapterRefs("章节#3", ""))
	assert.Nil(t, parseChapterRefs("", "无具体章节的台账问题"))
}
