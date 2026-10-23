package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/voice"
	"github.com/novelgo/novelgo/internal/volume"
)

// openSnapDB 建测试库：registry 全部台账模型 + 卷/快照/章节 + 履历/操作日志，
// 并注册 temporal 与 oplog 钩子（还原生产写路径环境，验证回滚不触发回调）。
func openSnapDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	models := []any{
		&volume.Volume{}, &volume.VolumeSnapshot{}, &chapter.Chapter{},
		&storage.LedgerChangeHistory{}, &storage.OperationLogRecord{},
	}
	for _, spec := range registry {
		models = append(models, spec.newRows())
	}
	for _, m := range models {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatalf("migrate %T: %v", m, err)
		}
	}
	if err := storage.RegisterLedgerTemporalHooks(db); err != nil {
		t.Fatalf("register ledger hooks: %v", err)
	}
	if err := storage.RegisterOplogHooks(db); err != nil {
		t.Fatalf("register oplog hooks: %v", err)
	}
	return db
}

func testSnapLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func int64Ptr(v int64) *int64 { return &v }

// seedFixture 造一卷 + 每张台账表一行 + 一章（正文清单），返回卷 id。
// 覆盖 nil 指针列（planted/revealed、as_of、source_chapter 等）以验证 NULL 往返。
func seedFixture(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	ctx := context.Background()

	vol := volume.Volume{NovelID: 1, Name: "第一卷", SortOrder: 1}
	if err := db.WithContext(ctx).Create(&vol).Error; err != nil {
		t.Fatalf("seed volume: %v", err)
	}

	ch := chapter.Chapter{NovelID: 1, VolumeID: int64Ptr(vol.ID), Title: "第一章", SortOrder: 1, WordCount: 2000}
	if err := db.WithContext(ctx).Create(&ch).Error; err != nil {
		t.Fatalf("seed chapter: %v", err)
	}

	char := character.Character{NovelID: 1, Name: "主角"}
	if err := db.WithContext(ctx).Create(&char).Error; err != nil {
		t.Fatalf("seed character: %v", err)
	}
	char2 := character.Character{NovelID: 1, Name: "师父"}
	if err := db.WithContext(ctx).Create(&char2).Error; err != nil {
		t.Fatalf("seed character2: %v", err)
	}
	rel := character.CharacterRelation{
		NovelID: 1, SourceCharacterID: char.ID, TargetCharacterID: char2.ID,
		RelationDescribe: "亦师亦敌", IsCurrent: true,
	}
	if err := db.WithContext(ctx).Create(&rel).Error; err != nil {
		t.Fatalf("seed relation: %v", err)
	}

	loc := location.Location{NovelID: 1, Name: "迷雾森林"}
	if err := db.WithContext(ctx).Create(&loc).Error; err != nil {
		t.Fatalf("seed location: %v", err)
	}

	arc := storyarc.StoryArc{NovelID: 1, Name: "复仇之路", ArcType: "main", Status: "active"}
	if err := db.WithContext(ctx).Create(&arc).Error; err != nil {
		t.Fatalf("seed arc: %v", err)
	}
	node := storyarc.ArcNode{NovelID: 1, StoryArcID: arc.ID, Title: "发现仇人身份", Status: "pending"}
	if err := db.WithContext(ctx).Create(&node).Error; err != nil {
		t.Fatalf("seed arc node: %v", err)
	}

	tl := timeline.TimelineEntry{NovelID: 1, Category: "foreshadowing", Status: "pending", Title: "断刀"}
	if err := db.WithContext(ctx).Create(&tl).Error; err != nil {
		t.Fatalf("seed timeline entry: %v", err)
	}
	anchor := timeline.ChronologyAnchor{NovelID: 1, Label: "新历元年", AnchorType: "era"}
	if err := db.WithContext(ctx).Create(&anchor).Error; err != nil {
		t.Fatalf("seed chronology anchor: %v", err)
	}

	rp := reader.ReaderPerspective{
		NovelID: 1, Type: "suspense", Content: "谁杀了村长",
		PlantedChapterID: int64Ptr(ch.ID),
	}
	if err := db.WithContext(ctx).Create(&rp).Error; err != nil {
		t.Fatalf("seed reader perspective: %v", err)
	}
	fact := character.CharacterFactKnowledge{NovelID: 1, CharID: char.ID, FactID: rp.ID, Knows: true}
	if err := db.WithContext(ctx).Create(&fact).Error; err != nil {
		t.Fatalf("seed fact knowledge: %v", err)
	}
	pov := reader.PovRoster{NovelID: 1, CharID: char.ID, Mode: reader.POVModeLimited}
	if err := db.WithContext(ctx).Create(&pov).Error; err != nil {
		t.Fatalf("seed pov roster: %v", err)
	}
	// Voice 台账（P1 T4.1）：书级十维，无 volume_id / 无 changed_at（不入 temporal）。
	// metrics 留空以覆盖可空列往返。
	vc := voice.Entry{NovelID: 1, Dimension: string(voice.DimRhythm), Content: "先结论后铺垫", Source: string(voice.SourceManual)}
	if err := db.WithContext(ctx).Create(&vc).Error; err != nil {
		t.Fatalf("seed voice dimension: %v", err)
	}
	return vol.ID
}

func countRows(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Count(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// ── 提交 ─────────────────────────────────────────────────────────────

func TestCommit_PayloadCompleteAndDeterministic(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))

	snap, err := svc.Commit(ctx, 1, volID)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if snap.SnapshotVersion != 1 {
		t.Errorf("version: want 1, got %d", snap.SnapshotVersion)
	}
	if snap.DiffRef != "" {
		t.Errorf("first diff_ref: want empty, got %q", snap.DiffRef)
	}
	if snap.CommittedAt.IsZero() {
		t.Error("committed_at 不应为零值")
	}

	var vol volume.Volume
	db.First(&vol, volID)
	if vol.SnapshotVersion != 1 {
		t.Errorf("volumes.snapshot_version: want 1, got %d", vol.SnapshotVersion)
	}

	p, err := parsePayload(snap.Payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if p.SchemaVersion != SchemaVersion || p.NovelID != 1 || p.VolumeID != volID {
		t.Errorf("payload 元数据错误: %+v", p)
	}
	if len(p.Tables) != len(registry) {
		t.Errorf("tables 键数: want %d, got %d", len(registry), len(p.Tables))
	}
	for _, spec := range registry {
		if _, ok := p.Tables[spec.name]; !ok {
			t.Errorf("payload 缺表 %s", spec.name)
		}
	}

	var chars []map[string]any
	dec := json.NewDecoder(bytes.NewReader(p.Tables["characters"]))
	if err := dec.Decode(&chars); err != nil || len(chars) != 2 {
		t.Fatalf("characters 行数: want 2 (err=%v), got %d", err, len(chars))
	}
	if chars[0]["name"] != "主角" {
		t.Errorf("character name: got %v", chars[0]["name"])
	}

	var rps []map[string]any
	json.Unmarshal(p.Tables["reader_perspectives"], &rps)
	if len(rps) != 1 || rps[0]["revealed_chapter_id"] != nil {
		t.Errorf("reader_perspectives 生命周期字段错误: %+v", rps)
	}

	if len(p.ChapterManifest) != 1 || p.ChapterManifest[0].Title != "第一章" || p.ChapterManifest[0].WordCount != 2000 {
		t.Errorf("正文清单错误: %+v", p.ChapterManifest)
	}

	// 确定性：相同库态两次采集逐表逐字节一致
	again, err := capturePayload(ctx, db, 1, volID, 1, snap.CommittedAt)
	if err != nil {
		t.Fatalf("recapture: %v", err)
	}
	for _, spec := range registry {
		if !bytes.Equal(p.Tables[spec.name], again.Tables[spec.name]) {
			t.Errorf("采集不确定：%s 两次 payload 不一致", spec.name)
		}
	}
}

// ── 冻结触发 ─────────────────────────────────────────────────────────

func TestFreeze_CommitsOnceOnTransition(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))

	v1, snap1, err := svc.Freeze(ctx, 1, volID)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if !v1.Frozen || v1.FrozenAt == nil {
		t.Fatalf("冻结态未置位: %+v", v1)
	}
	if snap1 == nil || snap1.SnapshotVersion != 1 {
		t.Fatalf("首冻未提交快照: %+v", snap1)
	}

	// 幂等：重复冻结不重复提交、不动 frozen_at
	v2, snap2, err := svc.Freeze(ctx, 1, volID)
	if err != nil {
		t.Fatalf("refreeze: %v", err)
	}
	if snap2 != nil {
		t.Errorf("重复冻结不应再提交快照: %+v", snap2)
	}
	if !v2.FrozenAt.Equal(*v1.FrozenAt) {
		t.Errorf("重复冻结不应动 frozen_at: %v → %v", v1.FrozenAt, v2.FrozenAt)
	}
	if n := countRows(t, db, "volume_snapshots"); n != 1 {
		t.Errorf("快照行数: want 1, got %d", n)
	}

	// 解冻再冻结 = 新一轮提交，版本递增 + diff_ref 指向基准
	store := volume.NewStore(db, testSnapLogger())
	if _, err := store.SetFrozen(ctx, nil, 1, volID, false); err != nil {
		t.Fatalf("unfreeze: %v", err)
	}
	v3, snap3, err := svc.Freeze(ctx, 1, volID)
	if err != nil {
		t.Fatalf("refreeze after unfreeze: %v", err)
	}
	if !v3.Frozen {
		t.Error("二次冻结未置位")
	}
	if snap3 == nil || snap3.SnapshotVersion != 2 {
		t.Fatalf("二次冻结版本: want 2, got %+v", snap3)
	}
	wantRef := "base:" + strconv.FormatInt(snap1.ID, 10)
	if snap3.DiffRef != wantRef {
		t.Errorf("diff_ref: want %q, got %q", wantRef, snap3.DiffRef)
	}
	if n := countRows(t, db, "volume_snapshots"); n != 2 {
		t.Errorf("快照行数: want 2, got %d", n)
	}
}

// ── diff ─────────────────────────────────────────────────────────────

func TestDiff_DetectsAddedRemovedChanged(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))

	base, err := svc.Commit(ctx, 1, volID)
	if err != nil {
		t.Fatalf("commit v1: %v", err)
	}

	// 三类变更：改角色名 / 增地点 / 删弧线节点
	var char character.Character
	if err := db.Where("novel_id = ? AND name = ?", 1, "主角").First(&char).Error; err != nil {
		t.Fatalf("load char: %v", err)
	}
	char.Name = "李四"
	if err := db.Save(&char).Error; err != nil {
		t.Fatalf("update char: %v", err)
	}
	if err := db.Create(&location.Location{NovelID: 1, Name: "黑铁城堡"}).Error; err != nil {
		t.Fatalf("create location: %v", err)
	}
	var node storyarc.ArcNode
	db.Where("novel_id = ?", 1).First(&node)
	if err := db.Delete(&storyarc.ArcNode{}, node.ID).Error; err != nil {
		t.Fatalf("delete arc node: %v", err)
	}

	target, err := svc.Commit(ctx, 1, volID)
	if err != nil {
		t.Fatalf("commit v2: %v", err)
	}

	d, err := svc.DiffSnapshots(ctx, base.ID, target.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if d.BaseVersion != 1 || d.TargetVersion != 2 {
		t.Errorf("diff 版本: got base=%d target=%d", d.BaseVersion, d.TargetVersion)
	}

	byTable := map[string]TableDelta{}
	for _, td := range d.Tables {
		byTable[td.Table] = td
	}
	if len(d.Tables) != 3 {
		t.Errorf("差异表数: want 3 (characters/locations/arc_nodes), got %d: %+v", len(d.Tables), d.Tables)
	}

	td := byTable["characters"]
	if len(td.Changed) != 1 || td.Changed[0].ID != char.ID {
		t.Fatalf("characters changed: %+v", td.Changed)
	}
	nameDelta, ok := td.Changed[0].Fields["name"]
	if !ok || nameDelta.Old != "主角" || nameDelta.New != "李四" {
		t.Errorf("name delta: %+v", td.Changed[0].Fields["name"])
	}

	td = byTable["locations"]
	if len(td.Added) != 1 || td.Added[0]["name"] != "黑铁城堡" {
		t.Errorf("locations added: %+v", td.Added)
	}

	td = byTable["arc_nodes"]
	if len(td.Removed) != 1 {
		t.Fatalf("arc_nodes removed: %+v", td.Removed)
	}
	removedID, _ := td.Removed[0]["id"].(json.Number).Int64()
	if removedID != node.ID {
		t.Errorf("arc_nodes removed id: want %d, got %d", node.ID, removedID)
	}

	// DiffToPrevious 与 DiffSnapshots 等价（v2 的 diff_ref 指向 v1）
	d2, err := svc.DiffToPrevious(ctx, target.ID)
	if err != nil {
		t.Fatalf("diff to previous: %v", err)
	}
	if !deltaEqual(d, d2) {
		t.Errorf("DiffToPrevious 与 DiffSnapshots 不一致: %+v vs %+v", d, d2)
	}
}

func deltaEqual(a, b *Delta) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

// ── 回滚 ─────────────────────────────────────────────────────────────

func TestRestore_RoundTripPreservesStateAndSilencesHooks(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))

	_, snap, err := svc.Freeze(ctx, 1, volID)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	v1, err := parsePayload(snap.Payload)
	if err != nil {
		t.Fatalf("parse v1: %v", err)
	}

	// 快照后改动：改角色（钩子会戳 changed_at）、增地点、删 POV 名册行
	var char character.Character
	if err := db.Where("novel_id = ? AND name = ?", 1, "主角").First(&char).Error; err != nil {
		t.Fatalf("load char: %v", err)
	}
	oldChangedAt := char.ChangedAt
	char.Name = "改名后"
	if err := db.Save(&char).Error; err != nil {
		t.Fatalf("update char: %v", err)
	}
	var updated character.Character
	db.First(&updated, char.ID)
	if oldChangedAt != nil && updated.ChangedAt != nil && updated.ChangedAt.Equal(*oldChangedAt) {
		t.Error("前置条件失效：Save 未触发 changed_at 变更")
	}
	if err := db.Create(&location.Location{NovelID: 1, Name: "新增地点"}).Error; err != nil {
		t.Fatalf("create location: %v", err)
	}
	var pov reader.PovRoster
	db.Where("novel_id = ?", 1).First(&pov)
	if err := db.Delete(&reader.PovRoster{}, pov.ID).Error; err != nil {
		t.Fatalf("delete pov: %v", err)
	}

	historyBefore := countRows(t, db, "ledger_change_history")
	oplogBefore := countRows(t, db, "operation_log")

	sum, err := svc.Restore(ctx, snap.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// 时间戳保真 + 状态逐字节还原：回滚后重采集 = v1 payload
	now, err := capturePayload(ctx, db, 1, volID, 1, snap.CommittedAt)
	if err != nil {
		t.Fatalf("recapture: %v", err)
	}
	for _, spec := range registry {
		if !bytes.Equal(v1.Tables[spec.name], now.Tables[spec.name]) {
			t.Errorf("回滚不精确：%s 与快照 payload 不一致\n快照: %s\n现状: %s",
				spec.name, v1.Tables[spec.name], now.Tables[spec.name])
		}
	}

	// 防履历污染：回滚本身不产生 ledger_change_history / operation_log
	if historyAfter := countRows(t, db, "ledger_change_history"); historyAfter != historyBefore {
		t.Errorf("回滚污染履历: %d → %d", historyBefore, historyAfter)
	}
	if oplogAfter := countRows(t, db, "operation_log"); oplogAfter != oplogBefore {
		t.Errorf("回滚污染操作日志: %d → %d", oplogBefore, oplogAfter)
	}

	// 摘要计数：locations 现 2 行（旧+新）→ 恢复 1 行；pov_roster 0 → 1
	if s := sum.Tables["locations"]; s.Deleted != 2 || s.Inserted != 1 {
		t.Errorf("locations 摘要: %+v", s)
	}
	if s := sum.Tables["pov_roster"]; s.Deleted != 0 || s.Inserted != 1 {
		t.Errorf("pov_roster 摘要: %+v", s)
	}

	// 显式确认：新增行已丢弃、被删行已回来
	var locCount int64
	db.Model(&location.Location{}).Where("novel_id = ? AND name = ?", 1, "新增地点").Count(&locCount)
	if locCount != 0 {
		t.Error("回滚后仍存在快照后新增的行")
	}
	var povBack reader.PovRoster
	if err := db.First(&povBack, pov.ID).Error; err != nil {
		t.Error("回滚后被删行未恢复")
	}
}

// ── voice_dimensions 专项（P1 T4.1，WBS §0.5）─────────────────────────

// TestVoiceDimensions_FreezeDiffRestore 验证 voice_dimensions 入 registry 后
// freeze→diff→restore 三态链路：改/增行在 diff 可见（维度枚举值为行键语义），
// 回滚丢新增、还原改动，重采集与快照 payload 逐字节一致
// （created_at/updated_at 保真——本表不入 temporal，无 changed_at）。
func TestVoiceDimensions_FreezeDiffRestore(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))

	_, snap1, err := svc.Freeze(ctx, 1, volID)
	if err != nil {
		t.Fatalf("freeze v1: %v", err)
	}
	v1, err := parsePayload(snap1.Payload)
	if err != nil {
		t.Fatalf("parse v1: %v", err)
	}

	// 改：DimRhythm 内容翻转；增：DimTabooWords 新行（metrics 留空覆盖可空列）
	var rhythm voice.Entry
	if err := db.Where("novel_id = ? AND dimension = ?", 1, voice.DimRhythm).First(&rhythm).Error; err != nil {
		t.Fatalf("load rhythm: %v", err)
	}
	rhythm.Content = "先铺垫后结论"
	if err := db.Save(&rhythm).Error; err != nil {
		t.Fatalf("update rhythm: %v", err)
	}
	taboo := voice.Entry{NovelID: 1, Dimension: string(voice.DimTabooWords), Content: "禁用突然/忽然", Source: string(voice.SourceManual)}
	if err := db.Create(&taboo).Error; err != nil {
		t.Fatalf("create taboo: %v", err)
	}

	snap2, err := svc.Commit(ctx, 1, volID)
	if err != nil {
		t.Fatalf("commit v2: %v", err)
	}

	d, err := svc.DiffSnapshots(ctx, snap1.ID, snap2.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	var voiceDelta TableDelta
	for _, td := range d.Tables {
		if td.Table == "voice_dimensions" {
			voiceDelta = td
		}
	}
	// 三态①改：content 字段 delta（updated_at 变化不作断言锚点）
	if len(voiceDelta.Changed) != 1 || voiceDelta.Changed[0].ID != rhythm.ID {
		t.Fatalf("voice changed: %+v", voiceDelta.Changed)
	}
	cd, ok := voiceDelta.Changed[0].Fields["content"]
	if !ok || cd.Old != "先结论后铺垫" || cd.New != "先铺垫后结论" {
		t.Errorf("content delta: %+v", voiceDelta.Changed[0].Fields["content"])
	}
	// 三态②增：整行可读，dimension 枚举值为行键语义
	if len(voiceDelta.Added) != 1 || voiceDelta.Added[0]["dimension"] != string(voice.DimTabooWords) {
		t.Errorf("voice added: %+v", voiceDelta.Added)
	}
	if len(voiceDelta.Removed) != 0 {
		t.Errorf("voice removed: %+v", voiceDelta.Removed)
	}

	// 三态③回滚：丢新增、还原改动（全表替换：现 2 行 → 删 2 插 1）
	sum, err := svc.Restore(ctx, snap1.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if s := sum.Tables["voice_dimensions"]; s.Deleted != 2 || s.Inserted != 1 {
		t.Errorf("voice_dimensions 摘要: %+v", s)
	}

	now, err := capturePayload(ctx, db, 1, volID, 1, snap1.CommittedAt)
	if err != nil {
		t.Fatalf("recapture: %v", err)
	}
	if !bytes.Equal(v1.Tables["voice_dimensions"], now.Tables["voice_dimensions"]) {
		t.Errorf("回滚不精确：voice_dimensions 与快照 payload 不一致\n快照: %s\n现状: %s",
			v1.Tables["voice_dimensions"], now.Tables["voice_dimensions"])
	}

	// 显式确认：新增行已丢弃、改动行内容已还原
	var after voice.Entry
	if err := db.Where("novel_id = ? AND dimension = ?", 1, voice.DimRhythm).First(&after).Error; err != nil {
		t.Fatalf("reload rhythm: %v", err)
	}
	if after.Content != "先结论后铺垫" {
		t.Errorf("rhythm content: want 旧值, got %q", after.Content)
	}
	if n := countRows(t, db, "voice_dimensions"); n != 1 {
		t.Errorf("voice_dimensions 行数: want 1, got %d", n)
	}
}

// ── 快照 vs 当前 ─────────────────────────────────────────────────────

func TestDiffAgainstCurrent(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))

	snap1, err := svc.Commit(ctx, 1, volID)
	if err != nil {
		t.Fatalf("commit v1: %v", err)
	}
	if err := db.Create(&location.Location{NovelID: 1, Name: "黑铁城堡"}).Error; err != nil {
		t.Fatalf("create location: %v", err)
	}

	// 快照 vs 当前：地点 1 增（前向修复差异依据）
	d, err := svc.DiffAgainstCurrent(ctx, snap1.ID)
	if err != nil {
		t.Fatalf("diff against current: %v", err)
	}
	if len(d.Tables) != 1 || d.Tables[0].Table != "locations" || len(d.Tables[0].Added) != 1 {
		t.Errorf("diff vs current: %+v", d.Tables)
	}

	// 提交 v2 后快照 vs 当前 = 无差异
	if _, err := svc.Commit(ctx, 1, volID); err != nil {
		t.Fatalf("commit v2: %v", err)
	}
	d2, err := svc.DiffAgainstCurrent(ctx, snap1.ID)
	if err != nil {
		t.Fatalf("diff against current after v2: %v", err)
	}
	if len(d2.Tables) != 1 {
		t.Errorf("v2 后仍应只差那次新增地点的对比基线是 v1，got %+v", d2.Tables)
	}
	snaps, err := svc.ListByVolume(ctx, 1, volID)
	if err != nil || len(snaps) != 2 {
		t.Fatalf("list: %v, %+v", err, snaps)
	}
	d3, err := svc.DiffAgainstCurrent(ctx, snaps[1].ID)
	if err != nil {
		t.Fatalf("diff current vs v2: %v", err)
	}
	if len(d3.Tables) != 0 {
		t.Errorf("当前状态应与 v2 一致: %+v", d3.Tables)
	}
}

// ── 无基准首版 ───────────────────────────────────────────────────────

func TestDiffToPrevious_FirstVersionEmptyBase(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))

	snap, err := svc.Commit(ctx, 1, volID)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	d, err := svc.DiffToPrevious(ctx, snap.ID)
	if err != nil {
		t.Fatalf("diff to previous: %v", err)
	}
	// 空基线=全量新增：fixture 台账 12 行（characters 2 + 其余 10 表各 1，含 voice_dimensions；正文清单不入 diff）
	var total int
	for _, td := range d.Tables {
		total += len(td.Added)
	}
	if total != 12 {
		t.Errorf("空基线新增行数: want 12, got %d (%+v)", total, d.Tables)
	}
}

// ── P2 T4.1 双快照语义与风格回滚（RestoreStyle）────────────────────────────

// TestRestoreStyle_ThreeStates 验证 WBS p2 T4.1 三态：
// ①「冻结双层钉住」= 单条快照记录同携事实层与风格层（§0.4 不拆两条记录），
//
//	且 freeze 后书级 style/事实层演进都不改冻结快照（快照不可变性）；
//
// ② RestoreStyle 只还原 voice_dimensions，事实表逐行不变（防误伤断言），
//
//	且不触发 temporal/oplog 回调（raw SQL 纪律，履历零新增）；
//
// ③ 全量 Restore 行为与既有逐字一致（回归：事实层演进被丢弃，整体回滚语义不变）。
func TestRestoreStyle_ThreeStates(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))

	// ── 双层钉住：单条快照记录同携事实层与风格层。
	_, snap, err := svc.Freeze(ctx, 1, volID)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if n := countRows(t, db, "volume_snapshots"); n != 1 {
		t.Fatalf("快照记录数: want 1（双层钉住单记录）, got %d", n)
	}
	base, err := parsePayload(snap.Payload)
	if err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if len(base.Tables["characters"]) == 0 || len(base.Tables[styleTableName]) == 0 {
		t.Fatalf("快照应同携事实层与风格层: characters=%dB voice=%dB",
			len(base.Tables["characters"]), len(base.Tables[styleTableName]))
	}

	// ── ① 演进：书级 style 改一行 + 增一行；事实层增一行——冻结快照必须纹丝不动。
	var rhythm voice.Entry
	if err := db.Where("novel_id = ? AND dimension = ?", 1, voice.DimRhythm).First(&rhythm).Error; err != nil {
		t.Fatalf("load rhythm: %v", err)
	}
	rhythm.Content = "演进后的文风"
	if err := db.Save(&rhythm).Error; err != nil {
		t.Fatalf("update rhythm: %v", err)
	}
	taboo := voice.Entry{NovelID: 1, Dimension: string(voice.DimTabooWords), Content: "禁用突然", Source: string(voice.SourceManual)}
	if err := db.Create(&taboo).Error; err != nil {
		t.Fatalf("create taboo: %v", err)
	}
	newChar := character.Character{NovelID: 1, Name: "快照后新角色"}
	if err := db.Create(&newChar).Error; err != nil {
		t.Fatalf("create character: %v", err)
	}
	var got volume.VolumeSnapshot
	if err := db.First(&got, snap.ID).Error; err != nil {
		t.Fatalf("reload snapshot: %v", err)
	}
	if got.Payload != snap.Payload {
		t.Errorf("冻结快照被改写（不可变性破坏）")
	}

	// ── ② RestoreStyle：只动风格表，事实表逐行不变，履历零新增。
	before, err := capturePayload(ctx, db, 1, volID, 1, snap.CommittedAt)
	if err != nil {
		t.Fatalf("capture before: %v", err)
	}
	var histBefore, oplogBefore int64
	db.Model(&storage.LedgerChangeHistory{}).Count(&histBefore)
	db.Model(&storage.OperationLogRecord{}).Count(&oplogBefore)

	sum, err := svc.RestoreStyle(ctx, snap.ID)
	if err != nil {
		t.Fatalf("restore style: %v", err)
	}
	if len(sum.Tables) != 1 {
		t.Errorf("RestoreStyle 应只动一张表: %+v", sum.Tables)
	}
	if s := sum.Tables[styleTableName]; s.Deleted != 2 || s.Inserted != 1 {
		t.Errorf("voice_dimensions 摘要: want 删2插1, got %+v", s)
	}

	after, err := capturePayload(ctx, db, 1, volID, 1, snap.CommittedAt)
	if err != nil {
		t.Fatalf("capture after: %v", err)
	}
	for _, spec := range registry {
		if spec.name == styleTableName {
			continue
		}
		if !bytes.Equal(before.Tables[spec.name], after.Tables[spec.name]) {
			t.Errorf("事实表 %s 被风格回滚误伤\n前: %s\n后: %s",
				spec.name, before.Tables[spec.name], after.Tables[spec.name])
		}
	}
	if !bytes.Equal(after.Tables[styleTableName], base.Tables[styleTableName]) {
		t.Errorf("风格表未还原到快照态\n快照: %s\n现状: %s",
			base.Tables[styleTableName], after.Tables[styleTableName])
	}
	var histAfter, oplogAfter int64
	db.Model(&storage.LedgerChangeHistory{}).Count(&histAfter)
	db.Model(&storage.OperationLogRecord{}).Count(&oplogAfter)
	if histAfter != histBefore || oplogAfter != oplogBefore {
		t.Errorf("风格回滚不得触发 temporal/oplog 回调: history %d→%d, oplog %d→%d",
			histBefore, histAfter, oplogBefore, oplogAfter)
	}
	// 显式确认：快照后新增的事实行仍在（风格回滚绝不触碰事实层）。
	var kept int64
	if err := db.Model(&character.Character{}).Where("id = ?", newChar.ID).Count(&kept).Error; err != nil {
		t.Fatalf("count new char: %v", err)
	}
	if kept != 1 {
		t.Errorf("快照后新增事实行应保留（RestoreStyle 只动风格）")
	}

	// ── ③ 全量 Restore 回归：整体回滚语义逐字不变（事实层演进随回滚丢弃）。
	if _, err := svc.Restore(ctx, snap.ID); err != nil {
		t.Fatalf("full restore: %v", err)
	}
	full, err := capturePayload(ctx, db, 1, volID, 1, snap.CommittedAt)
	if err != nil {
		t.Fatalf("capture full: %v", err)
	}
	for _, spec := range registry {
		if !bytes.Equal(full.Tables[spec.name], base.Tables[spec.name]) {
			t.Errorf("全量 Restore 后 %s 不等于快照态\n快照: %s\n现状: %s",
				spec.name, base.Tables[spec.name], full.Tables[spec.name])
		}
	}
}
