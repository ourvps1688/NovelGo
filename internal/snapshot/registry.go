// Package snapshot 提供卷快照（WBS 2.2）的采集、提交、差异与回滚能力。
//
// 设计定案（2.2 勘察结论）：
//
//   - 采集口径 = 冻结时刻全量 series bible。WBS §7 点 5「每卷冻结提交快照，当前卷在其上
//     扩展；不断增长的快照即系列圣经」——快照是递增全量而非按卷过滤。台账条目 volume_id
//     写路径尚未填充（2.8 回填后仍多为 NULL），按 volume_id 过滤只会得到空快照；全量采集
//     同时保证 retcon 回滚能覆盖任意条目（含归属前卷的条目）。
//
//   - payload = §0.4 冻结清单的全部台账模型（带 volume_id/temporal）+ 正文清单
//     （只读上下文，不参与回滚——正文以 Git 文件为准）。
//
//   - diff_ref = 基准快照的对象键（base:<snapshot_id>）。相对上一版本的 diff 由
//     (基准, 本快照) 唯一确定，运行时计算（Diff），不物化衍生文件以免腐化；
//     首版无基准，diff_ref 为空。
//
//   - 回滚（Restore）= 把台账表整体恢复到快照提交时刻，其后的改动被丢弃（retcon 显式
//     受控操作）。恢复写入必须绕开 2.6 temporal/history 回调与 oplog 回调——它们是
//     全局 gorm callback，Session{SkipHooks} 拦不住（回调不检查 SkipHooks），
//     只有 raw SQL Exec 不触发 Create/Update/Delete 回调链。因此恢复保留原时间戳、
//     不污染 ledger_change_history / operation_log。
//
//   - 冻结即串行提交（WBS 阶段 2 风险对策）：unfrozen→frozen 迁移与快照提交在同一
//     事务内完成；uk_volume_snapshot (volume_id, snapshot_version) 在存储层兜底，
//     版本冲突时有限次重试。
package snapshot

import (
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/voice"
)

// tableSpec 注册一张 series bible 台账表的快照行为。
type tableSpec struct {
	// name 表名（与 gorm 模型表名一致）。
	name string
	// newRows 返回空的 *[]T 行集（采集 Find 与回滚解码共用）。
	newRows func() any
}

// registry 是快照覆盖的 series bible 台账表，顺序固定（diff 输出顺序稳定）。
// 口径 = WBS §0.4 冻结清单：带 volume_id/temporal 的全部台账模型——
// 7 张既有表（v170 ledgerTemporalTables）+ 3 张新表
// （character_fact_knowledge / pov_roster / chronology_anchors）。
// 邻接组织表（character_groups、character_group_members、location_relations、
// chapter_plans）不在冻结清单内，不入快照。
// P1 追加 voice_dimensions（WBS §0.5）：书级十维 Voice 台账，风格非卷级事实，
// 无 volume_id 且不入 temporal（无 changed_at）——是冻结清单的口径例外，
// 采集仍走全量（novel_id 维度），capture/diff/restore 机制零改动复用。
// 注意：存量卷首快照后新增本表 → 首个含 voice 的快照 diff 相对旧基准呈现
// 「全量新增行」属预期行为（旧 payload 无此表键=空基线），勿当 bug 修。
// styleTableName 书级风格台账表名（P2 §0.4 风格回滚唯一目标表，单源禁改）。
const styleTableName = "voice_dimensions"

var registry = []tableSpec{
	{"characters", func() any { return &[]character.Character{} }},
	{"character_relations", func() any { return &[]character.CharacterRelation{} }},
	{"character_fact_knowledge", func() any { return &[]character.CharacterFactKnowledge{} }},
	{"locations", func() any { return &[]location.Location{} }},
	{"story_arcs", func() any { return &[]storyarc.StoryArc{} }},
	{"arc_nodes", func() any { return &[]storyarc.ArcNode{} }},
	{"time_entries", func() any { return &[]timeline.TimelineEntry{} }},
	{"chronology_anchors", func() any { return &[]timeline.ChronologyAnchor{} }},
	{"reader_perspectives", func() any { return &[]reader.ReaderPerspective{} }},
	{"pov_roster", func() any { return &[]reader.PovRoster{} }},
	{styleTableName, func() any { return &[]voice.Entry{} }},
}

// tableByName 按表名取 registry 规格（RestoreStyle 按表回滚用，见 restore.go）。
func tableByName(name string) (tableSpec, bool) {
	for _, spec := range registry {
		if spec.name == name {
			return spec, true
		}
	}
	return tableSpec{}, false
}
