package quality

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 稳定 key 身份：无台账引用的 L1 语义发现原本恒为 l1|check|#0/#1，改好上一条后
// 本轮换一批全新的 continuity 问题仍撞同一 key，被算成「一直没修」，3 轮保护
// 永远触发不了（第 12 章 9 轮实证）。锚点短哈希让 key 真正区分问题本身。

func TestAnchorCarriesIdentityInL1Keys(t *testing.T) {
	two := []Finding{
		{ID: "L1-1", Check: "continuity", Desc: "台阶数矛盾", Anchor: "台阶一共七级，他数着下"},
		{ID: "L1-2", Check: "continuity", Desc: "步数折算矛盾", Anchor: "六千七百步按你的步幅折"},
	}
	keys := FindingKeys(nil, two)
	assert.Len(t, keys, 2)
	assert.NotEqual(t, keys[0], keys[1], "同一个 check 下的两条不同问题不得撞号")
	assert.Contains(t, keys[0], "|q")
	assert.Contains(t, keys[1], "|q")

	same := []Finding{{ID: "L1-9", Check: "continuity", Desc: "换了措辞的同一问题", Anchor: "台阶一共七级，他数着下"}}
	assert.Equal(t, keys[0], FindingKeys(nil, same)[0], "原句没变 = 同一条问题，key 不变")

	changed := []Finding{{ID: "L1-9", Check: "continuity", Desc: "已改", Anchor: "台阶一共三级半，他数着下"}}
	assert.NotEqual(t, keys[0], FindingKeys(nil, changed)[0], "原句被改掉 = 该 key 自然消失（resolved）")
}

func TestAnchorKeyDoesNotDisturbLegacyOrL0Keys(t *testing.T) {
	noAnchor := []Finding{{ID: "L1-1", Check: "continuity", Desc: "老档案无锚点"}}
	assert.Equal(t, "l1|continuity|#0", FindingKeys(nil, noAnchor)[0],
		"老档案无 anchor 时逐字降级原 key，历史脏数据不受影响")

	withRefs := []Finding{{
		ID: "L1-1", Check: "foreshadow", Desc: "坑1039",
		LedgerRefs: []string{"1039"}, Anchor: "线又亮了一回，比刚才短",
	}}
	assert.Equal(t, "l1|foreshadow|1039#0", FindingKeys(nil, withRefs)[0],
		"已有台账引用的发现 key 不变，避免整库 key 一次性跳变")

	l0 := []Finding{{ID: "C1", Check: "word_count", Desc: "偏短", Anchor: "误带的锚点"}}
	assert.Equal(t, "l0|word_count|#0", FindingKeys(l0, nil)[0], "L0 完全不受锚点影响")
}

func TestFindingDiffWithAnchorKeysReportsRealPersistedOnly(t *testing.T) {
	prev := FindingKeys(nil, []Finding{
		{ID: "L1-1", Check: "continuity", Anchor: "台阶一共七级，他数着下"},
		{ID: "L1-2", Check: "continuity", Anchor: "六千七百步按你的步幅折"},
	})
	// 改掉第一条，第二条原句没动，同时新冒出一条全新的 continuity。
	cur := FindingKeys(nil, []Finding{
		{ID: "L1-1", Check: "continuity", Anchor: "六千七百步按你的步幅折"},
		{ID: "L1-2", Check: "continuity", Anchor: "跑的这五公里是它排的"},
	})
	d := DiffFindingKeys(prev, cur)
	assert.Len(t, d.Resolved, 1, "台阶数那条被真正消解")
	assert.Len(t, d.Persisted, 1, "只有步数那条真的没修")
	assert.Len(t, d.New, 1, "新冒出的问题如实记为 new，不再伪装成老问题")
}
