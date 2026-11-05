package quality

// platform_profile_test.go —— 平台风格 profile（P0-1）契约：
// 注册表取名/别名归一、CheckerConfig 投影（Profile/EffectiveBeatDensityMin）、
// Validate 拒未注册名、落库列进 DoUpdates 覆盖。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQimaoProfilePreset 七猫 preset 量化口径锁定（真实编辑反馈口径，
// docs/quality-signability-upgrade-plan-2026-11-05.md §7）。
func TestQimaoProfilePreset(t *testing.T) {
	p := QimaoProfile()
	assert.Equal(t, "qimao", p.Name)
	assert.Equal(t, "七猫", p.Title)
	assert.Equal(t, 1000, p.ChapterLenMin, "章长 1000–1500（计划文档口径，勿回 1800）")
	assert.Equal(t, 1500, p.ChapterLenMax)
	assert.Equal(t, 500, p.PremiseWindow, "前 500 字爆点/核心看点实体化")
	assert.Equal(t, 800, p.CheatWindow)
	assert.Equal(t, 200, p.ProtagonistWindow)
	assert.Equal(t, 3, p.OpeningChapterWindow, "黄金三章")
	assert.Equal(t, 200, p.HookEarlyWindow)
	assert.Equal(t, 1, p.HookEarlyMin)
	assert.Equal(t, 500, p.HookWindow)
	assert.Equal(t, 2, p.HookMin)
	assert.Equal(t, 1.0, p.BeatDensityMin)
	assert.Equal(t, 300, p.EmotionSegMax, "情绪段上限（P0-2 消费）")
}

// TestPlatformProfileByName 注册表取名：注册名/别名/大小写命中；空/未注册 → nil。
func TestPlatformProfileByName(t *testing.T) {
	for _, name := range []string{"qimao", "七猫", " QIMAO ", "qimao"} {
		p := PlatformProfileByName(name)
		require.NotNil(t, p, "name=%q 应命中", name)
		assert.Equal(t, "qimao", p.Name)
	}
	assert.Nil(t, PlatformProfileByName(""))
	assert.Nil(t, PlatformProfileByName("   "))
	assert.Nil(t, PlatformProfileByName("fanqie"), "未注册平台不臆造口径（勿注册）")
}

// TestCheckerConfigProfileProjection Profile/EffectiveBeatDensityMin 投影口径。
func TestCheckerConfigProfileProjection(t *testing.T) {
	// 缺省：无 profile、密度 0。
	cfg := DefaultCheckerConfig(1)
	assert.Nil(t, cfg.Profile())
	assert.Equal(t, 0.0, cfg.EffectiveBeatDensityMin())

	// 未注册名 → nil（宁缺勿错，不武装任何检查）。
	bad := &CheckerConfig{PlatformProfile: "fanqie"}
	assert.Nil(t, bad.Profile())

	// profile 生效：密度下限回落 profile 值。
	cfg2 := &CheckerConfig{PlatformProfile: "qimao"}
	p := cfg2.Profile()
	require.NotNil(t, p)
	assert.Equal(t, 1.0, cfg2.EffectiveBeatDensityMin(), "列 0 → 回落 profile 值")

	// 显式列值 >0 优先于 profile。
	cfg3 := &CheckerConfig{PlatformProfile: "qimao", BeatDensityMin: 2.5}
	assert.Equal(t, 2.5, cfg3.EffectiveBeatDensityMin(), "显式列值优先")

	// 无 profile 且列 0 → 0（不限）。
	cfg4 := &CheckerConfig{}
	assert.Equal(t, 0.0, cfg4.EffectiveBeatDensityMin())
}

// TestCheckerConfigValidatePlatformProfile Validate：未注册名拒收、别名归一注册名、空=清除合法。
func TestCheckerConfigValidatePlatformProfile(t *testing.T) {
	bad := &CheckerConfig{PlatformProfile: "fanqie", TabooList: "[]",
		PremiseList: "[]", CheatList: "[]", ProtagonistList: "[]"}
	require.Error(t, bad.Validate(), "未注册名拒收")

	alias := &CheckerConfig{PlatformProfile: "七猫", TabooList: "[]",
		PremiseList: "[]", CheatList: "[]", ProtagonistList: "[]"}
	require.NoError(t, alias.Validate())
	assert.Equal(t, "qimao", alias.PlatformProfile, "别名归一为注册名")

	empty := &CheckerConfig{PlatformProfile: "  ", TabooList: "[]",
		PremiseList: "[]", CheatList: "[]", ProtagonistList: "[]"}
	require.NoError(t, empty.Validate(), "空=清除合法")
	assert.Equal(t, "", empty.PlatformProfile)
}

// TestSaveCheckerConfigPlatformProfile 落库：platform_profile 进 DoUpdates 覆盖
// （设置→清除不残留旧值），未注册名不落库。
func TestSaveCheckerConfigPlatformProfile(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.DB.AutoMigrate(&CheckerConfig{}))
	ctx := context.Background()

	cfg := &CheckerConfig{NovelID: 7, PlatformProfile: "七猫",
		TabooList: "[]", PremiseList: "[]", CheatList: "[]", ProtagonistList: "[]"}
	require.NoError(t, s.SaveCheckerConfig(ctx, cfg))
	got, err := s.GetCheckerConfig(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "qimao", got.PlatformProfile, "别名归一落库")
	require.NotNil(t, got.Profile())

	// 清除：空串覆盖，不残留。
	cfg2 := &CheckerConfig{NovelID: 7, PlatformProfile: "",
		TabooList: "[]", PremiseList: "[]", CheatList: "[]", ProtagonistList: "[]"}
	require.NoError(t, s.SaveCheckerConfig(ctx, cfg2))
	got, err = s.GetCheckerConfig(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "", got.PlatformProfile, "空串覆盖清除，不残留旧值")
	assert.Nil(t, got.Profile())

	// 未注册名拒收不落库。
	bad := &CheckerConfig{NovelID: 8, PlatformProfile: "fanqie",
		TabooList: "[]", PremiseList: "[]", CheatList: "[]", ProtagonistList: "[]"}
	require.Error(t, s.SaveCheckerConfig(ctx, bad))
	got, err = s.GetCheckerConfig(ctx, 8)
	require.NoError(t, err)
	assert.Equal(t, "", got.PlatformProfile, "拒收不落库")
}
