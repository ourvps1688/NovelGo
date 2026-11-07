//go:build cgo

package character

// #103 第三层：生死态派生的精度与写入契约。
// 核心回归：旧设计把 alive 当人工维护的独立列，正文已死而 alive=1 时巡检/修复/复验
// 反复空转；现在每章写后由系统从正文单向派生，误标由作者人工入口纠正。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/storage"
)

func aliveDeriveChars() []Character {
	yes := true
	return []Character{
		{ID: 1, NovelID: 1, Name: "赵彦", Aliases: `["彦哥"]`, Alive: &yes},
		{ID: 2, NovelID: 1, Name: "疤脸", Alive: &yes},
		{ID: 3, NovelID: 1, Name: "林澈", Alive: &yes},
	}
}

func aliveOf(t *testing.T, db *gorm.DB, id int64) *bool {
	t.Helper()
	var ch Character
	require.NoError(t, db.First(&ch, id).Error)
	return ch.Alive
}

func TestScanDeathMentions_Precision(t *testing.T) {
	chars := aliveDeriveChars()
	cases := []struct {
		name     string
		text     string
		wantName string
	}{
		// 高精度正例：完成态死亡谓词紧邻角色名/别名。
		{"完成态气绝", "赵彦当场气绝，众人哗然。", "赵彦"},
		{"被斩", "疤脸被斩于城门下。", "疤脸"},
		{"终于死了", "林澈终于死了。", "林澈"},
		{"标点加已死", "赵彦，已死。", "赵彦"},
		{"已故", "赵彦已故多年。", "赵彦"},
		{"别名", "彦哥咽了气。", "彦哥"},
		{"再也没能醒来", "赵彦再也没能醒来。", "赵彦"},
		{"跨句否定不误伤", "他没死。赵彦死了。", "赵彦"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanDeathMentions(tc.text, chars)
			require.Len(t, got, 1, "文本 %q 应召回 1 条", tc.text)
			require.Equal(t, tc.wantName, got[0].Name)
			require.NotEmpty(t, got[0].Excerpt, "定位证据须带原文摘录")
		})
	}
}

func TestScanDeathMentions_RejectsFalsePositives(t *testing.T) {
	chars := aliveDeriveChars()
	falses := []string{
		"赵彦差点死了。",
		"赵彦险些被斩。",
		"赵彦装死骗过众人。",
		"赵彦的父亲死了。", // 「的」他人所有格
		"赵彦死了吗？",
		"赵彦死了？",
		"赵彦死了心。",  // 比喻
		"赵彦死去活来。", // 成语
		"赵彦死里逃生。",
		"赵彦死心塌地。",
		"如果赵彦死了，计划就完了。",
		"传闻赵彦死了。",
		"赵彦梦见自己死了。",
		"赵彦没有死。",
		"赵彦并未身亡。", // 同句否定窗口
		"赵彦倒下后又爬了起来。",
		"赵彦的剑被人斩断。",
	}
	for _, text := range falses {
		t.Run(text, func(t *testing.T) {
			require.Empty(t, ScanDeathMentions(text, chars), "假阳不得派生：%q", text)
		})
	}
}

func TestScanDeathMentions_ShortNameAndLongNamePriority(t *testing.T) {
	yes := true
	require.Empty(t, ScanDeathMentions("林中死了人。", []Character{
		{ID: 9, NovelID: 1, Name: "林", Alive: &yes},
	}), "单字名误匹配率过高，保守跳过，交给 statesync 复核")

	chars := append(aliveDeriveChars(), Character{ID: 4, NovelID: 1, Name: "赵彦之父", Alive: &yes})
	got := ScanDeathMentions("赵彦之父死了。", chars)
	require.Len(t, got, 1)
	require.Equal(t, int64(4), got[0].CharacterID, "长名优先，不得归到短名「赵彦」")
}

func TestDeriveAliveFromChapter_OneWayIdempotentCrossNovel(t *testing.T) {
	db := openCharDB(t)
	ctx := context.Background()
	yes := true
	for _, ch := range aliveDeriveChars() {
		require.NoError(t, db.Create(&ch).Error)
	}
	// 跨书同名角色不得被本书正文波及。
	require.NoError(t, db.Create(&Character{ID: 99, NovelID: 2, Name: "赵彦", Alive: &yes}).Error)

	got, err := DeriveAliveFromChapter(ctx, db, 1, "赵彦当场气绝，林澈吓得后退。")
	require.NoError(t, err)
	require.Len(t, got, 1, "只应标死赵彦；林澈只是受惊")
	require.Equal(t, int64(1), got[0].CharacterID)
	require.False(t, *aliveOf(t, db, 1))
	require.True(t, *aliveOf(t, db, 3), "未明写死亡的角色不得被波及")
	require.True(t, *aliveOf(t, db, 99), "派生必须按 novel_id 隔离")

	// 幂等：重复跑不产生新写入、不返回重复名单。
	got2, err := DeriveAliveFromChapter(ctx, db, 1, "赵彦当场气绝，林澈吓得后退。")
	require.NoError(t, err)
	require.Empty(t, got2)
	require.False(t, *aliveOf(t, db, 1))

	// 单向：派生绝不把已标死角色复活（复活是作者人工入口的 retcon 权限）。
	no := false
	require.NoError(t, db.Create(&Character{ID: 100, NovelID: 1, Name: "疤脸旧识", Alive: &no}).Error)
	_, err = DeriveAliveFromChapter(ctx, db, 1, "疤脸旧识早已不在人世，赵彦当场气绝。")
	require.NoError(t, err)
	require.False(t, *aliveOf(t, db, 100))
}

func TestDeriveAliveFromChapter_RespectsAuthorOverride(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&Character{}, &storage.LedgerChangeHistory{}, &storage.OperationLogRecord{},
	))
	require.NoError(t, storage.RegisterLedgerTemporalHooks(db))
	autoCtx := storage.WithLedgerSource(context.Background(), storage.HistorySourceAuto)
	yes := true
	require.NoError(t, db.Create(&Character{NovelID: 1, Name: "赵彦", Alive: &yes}).Error)

	got, err := DeriveAliveFromChapter(autoCtx, db, 1, "赵彦当场气绝。")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.False(t, *aliveOf(t, db, 1))

	// 作者在人工入口纠正为在世（app 层 PUT：ctx 不带 auto source → author）。
	var ch Character
	require.NoError(t, db.First(&ch, 1).Error)
	alive := true
	ch.Alive = &alive
	require.NoError(t, db.WithContext(context.Background()).Save(&ch).Error)
	require.True(t, *aliveOf(t, db, 1))

	// 重跑同一章同步：人工裁决优先，不得被同一句正文再次标死。
	got2, err := DeriveAliveFromChapter(autoCtx, db, 1, "赵彦当场气绝。")
	require.NoError(t, err)
	require.Empty(t, got2)
	require.True(t, *aliveOf(t, db, 1))
}

func TestDeriveAliveFromChapter_ChangeAudited(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&Character{}, &storage.LedgerChangeHistory{}, &storage.OperationLogRecord{},
	))
	require.NoError(t, storage.RegisterLedgerTemporalHooks(db))
	ctx := storage.WithLedgerSource(context.Background(), storage.HistorySourceAuto)
	yes := true
	require.NoError(t, db.WithContext(ctx).Create(&Character{NovelID: 1, Name: "赵彦", Alive: &yes}).Error)

	got, err := DeriveAliveFromChapter(ctx, db, 1, "赵彦当场气绝。")
	require.NoError(t, err)
	require.Len(t, got, 1)

	var rows []storage.LedgerChangeHistory
	require.NoError(t, db.Where("entry_table = ? AND field = ?", "characters", "alive").Find(&rows).Error)
	require.NotEmpty(t, rows, "派生写入必须经 GORM 回调进台账履历，供作者回溯")
	require.Equal(t, "true", rows[len(rows)-1].Old)
	require.Equal(t, "false", rows[len(rows)-1].New)
}
