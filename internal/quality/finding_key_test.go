package quality

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// finding_key_test.go —— #114 P1 稳定身份契约。
// 核心不变量：跨轮身份只认「作用域+检查项+归一化命中特征+组内序号」，
// 不认顺编 ID、不认本轮实测数字、不认描述文案、不认状态。

func f(check, id, desc string) Finding {
	return Finding{ID: id, Check: check, Level: FindingLevelDefault, Desc: desc, Status: FindingStatusPending}
}

func TestFindingKeysIgnoreUnstableFields(t *testing.T) {
	a := []Finding{f("tail_hook", "C1", "章尾未见钩子信号（本章 1234 字）")}
	b := []Finding{f("tail_hook", "C7", "章尾未见钩子信号（本章 2100 字，第 9 段）")}
	b[0].Status = FindingStatusFixed
	b[0].ChapterIDs = []int64{3}
	b[0].Level = "P0"

	assert.Equal(t, FindingKeys(a, nil), FindingKeys(b, nil),
		"ID/Desc 实测值/Status/Level/ChapterIDs 变化不得改变稳定 key")
}

func TestFindingKeysScopeAndOrdinal(t *testing.T) {
	keys := FindingKeys(
		[]Finding{f("reader_state", "C1", "a"), f("reader_state", "C2", "b")},
		[]Finding{f("reader_state", "L1-1", "c")},
	)
	assert.Equal(t, []string{
		"l0|reader_state|#0", "l0|reader_state|#1", "l1|reader_state|#0",
	}, keys, "L0/L1 前缀隔离，同组按出现顺序编号")
}

func TestFindingKeysNormalizeLedgerRefs(t *testing.T) {
	a := f("continuity", "L1-1", "伏笔未回收")
	a.LedgerRefs = []string{"ledger#12", " ledger#5 "}
	b := f("continuity", "L1-9", "完全不同的描述")
	b.LedgerRefs = []string{"ledger#5", "ledger#12"}
	assert.Equal(t, FindingKeys(nil, []Finding{a}), FindingKeys(nil, []Finding{b}),
		"台账引用排序/去空格后归一；描述变化不影响身份")
}

func TestDiffFindingKeysThreeStates(t *testing.T) {
	d := DiffFindingKeys(
		[]string{"l0|a|#0", "l0|b|#0", "l1|c|#0"},
		[]string{"l0|b|#0", "l1|c|#0", "l1|d|#0"},
	)
	assert.Equal(t, []string{"l0|a|#0"}, d.Resolved)
	assert.Equal(t, []string{"l0|b|#0", "l1|c|#0"}, d.Persisted)
	assert.Equal(t, []string{"l1|d|#0"}, d.New)
}

func TestDiffFindingKeysEmptyAndDuplicates(t *testing.T) {
	empty := DiffFindingKeys(nil, nil)
	assert.Empty(t, empty.Resolved)
	assert.Empty(t, empty.Persisted)
	assert.Empty(t, empty.New)

	allNew := DiffFindingKeys(nil, []string{"a", "b", "a"})
	assert.Equal(t, []string{"a", "b"}, allNew.New, "集合差按首次出现保序去重")
}
