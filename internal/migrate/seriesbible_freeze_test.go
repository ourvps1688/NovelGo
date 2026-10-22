package migrate_test

import (
	"testing"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/volume"
)

// TestSeriesBibleEnumFreeze 锁死 series bible 四维的枚举值与表名（WBS §0.4「枚举冻结」
// 与「新增列/表清单」是实现唯一依据）。任何一处改值/改名而未同步 WBS 契约，此测试即红——
// 与 internal/pipeline/contract_test.go 的 issue 契约回归同一治理思路。
func TestSeriesBibleEnumFreeze(t *testing.T) {
	cases := []struct {
		name string
		got  map[string]string
		want map[string]string
	}{
		{"relation_kind（R1 软枚举）", map[string]string{
			"mentor": character.RelationKindMentor, "rival": character.RelationKindRival,
			"lover": character.RelationKindLover, "kin": character.RelationKindKin,
			"other": character.RelationKindOther,
		}, map[string]string{
			"mentor": "mentor", "rival": "rival", "lover": "lover", "kin": "kin", "other": "other",
		}},
		{"location.state", map[string]string{
			"intact": location.StateIntact, "destroyed": location.StateDestroyed, "blocked": location.StateBlocked,
		}, map[string]string{
			"intact": "intact", "destroyed": "destroyed", "blocked": "blocked",
		}},
		{"chronology.anchor_type", map[string]string{
			"era": timeline.AnchorTypeEra, "date": timeline.AnchorTypeDate, "age": timeline.AnchorTypeAge,
			"season": timeline.AnchorTypeSeason, "gap": timeline.AnchorTypeGap,
		}, map[string]string{
			"era": "era", "date": "date", "age": "age", "season": "season", "gap": "gap",
		}},
		{"pov mode", map[string]string{
			"limited": reader.POVModeLimited, "omniscient": reader.POVModeOmniscient,
		}, map[string]string{
			"limited": "limited", "omniscient": "omniscient",
		}},
		{"history.source", map[string]string{
			"auto": storage.HistorySourceAuto, "author": storage.HistorySourceAuthor, "retcon": storage.HistorySourceRetcon,
		}, map[string]string{
			"auto": "auto", "author": "author", "retcon": "retcon",
		}},
	}
	for _, c := range cases {
		for k, got := range c.got {
			if want := c.want[k]; got != want {
				t.Errorf("%s: %s = %q, want %q（WBS §0.4 冻结值）", c.name, k, got, want)
			}
		}
	}
}

// TestSeriesBibleNewTableNames 锁死 5 张新表的表名（WBS §0.4 新增表清单）。
func TestSeriesBibleNewTableNames(t *testing.T) {
	for _, c := range []struct {
		model any
		want  string
	}{
		{timeline.ChronologyAnchor{}, "chronology_anchors"},
		{character.CharacterFactKnowledge{}, "character_fact_knowledge"},
		{reader.PovRoster{}, "pov_roster"},
		{storage.LedgerChangeHistory{}, "ledger_change_history"},
		{volume.VolumeSnapshot{}, "volume_snapshots"},
	} {
		namer, ok := c.model.(interface{ TableName() string })
		if !ok {
			t.Fatalf("%T 缺 TableName()", c.model)
		}
		if got := namer.TableName(); got != c.want {
			t.Errorf("%T 表名 = %q, want %q", c.model, got, c.want)
		}
	}
}
