package quality

// checker_config_test.go —— 题材卡机制化（R2b-3）配置契约：
// Validate 拒收/归一、缺省回落逐字一致、落库覆盖语义（含 GORM 零值跳写坑回归）。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckerConfigValidate 拒收负数与坏清单；归一化（TrimSpace 去空去重、空→[]）。
func TestCheckerConfigValidate(t *testing.T) {
	// 负数拒收。
	bad := []CheckerConfig{
		{BeatDensityMin: -0.5},
		{WindowN: -1},
		{TabooList: `["虐主"`},
		{TabooList: `{"a":1}`},
		{TabooList: `["a", 1]`},
	}
	for i := range bad {
		require.Error(t, bad[i].Validate(), "case %d 应拒收", i)
	}

	// 归一化：去空去重保序。
	cfg := CheckerConfig{TabooList: `[" 虐主 ", "", "破墙", "虐主"]`}
	require.NoError(t, cfg.Validate())
	assert.Equal(t, `["虐主","破墙"]`, cfg.TabooList)

	// 空清单归一 []。
	empty := CheckerConfig{}
	require.NoError(t, empty.Validate())
	assert.Equal(t, "[]", empty.TabooList)
}

// TestCheckerConfigAccessors 缺省口径：HookRequired nil=true、OverdueGrace 负归 0、
// Taboos 坏数据按空（宁缺勿错）。
func TestCheckerConfigAccessors(t *testing.T) {
	cfg := DefaultCheckerConfig(9)
	assert.True(t, cfg.HookRequired(), "nil=缺省 true=现行行为")
	assert.Equal(t, 0, cfg.OverdueGrace())
	assert.Empty(t, cfg.Taboos())

	f := false
	cfg.TailHookRequired = &f
	assert.False(t, cfg.HookRequired())
	cfg.WindowN = -3
	assert.Equal(t, 0, cfg.OverdueGrace(), "负宽限归 0")
	cfg.TabooList = "not-json"
	assert.Empty(t, cfg.Taboos(), "坏数据按空清单（不炸不误报）")
}

// TestGetCheckerConfigDefault 未配置 → 缺省（不报错，逐字一致口径）。
func TestGetCheckerConfigDefault(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.DB.AutoMigrate(&CheckerConfig{}))
	cfg, err := s.GetCheckerConfig(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), cfg.NovelID)
	assert.True(t, cfg.HookRequired())
	assert.Equal(t, 0, cfg.OverdueGrace())
	assert.Empty(t, cfg.Taboos())
}

// TestSaveCheckerConfigRoundTrip 落库覆盖：同书单行覆盖、跨书隔离；
// 显式 false 不被 GORM 零值跳写（指针三态回归）。
func TestSaveCheckerConfigRoundTrip(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.DB.AutoMigrate(&CheckerConfig{}))
	ctx := context.Background()

	f := false
	cfg := &CheckerConfig{NovelID: 7, BeatDensityMin: 1.5, TailHookRequired: &f,
		WindowN: 3, TabooList: `["虐主"]`}
	require.NoError(t, s.SaveCheckerConfig(ctx, cfg))

	got, err := s.GetCheckerConfig(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, 1.5, got.BeatDensityMin)
	assert.False(t, got.HookRequired(), "显式 false 必须落库（零值跳写坑回归）")
	assert.Equal(t, 3, got.OverdueGrace())
	assert.Equal(t, []string{"虐主"}, got.Taboos())

	// 同书覆盖（局部字段整行覆盖语义由工具层拼装，这里验证同书单行不重复）。
	cfg2 := &CheckerConfig{NovelID: 7, BeatDensityMin: 0, TabooList: "[]"}
	require.NoError(t, s.SaveCheckerConfig(ctx, cfg2))
	var count int64
	require.NoError(t, s.DB.Model(&CheckerConfig{}).Where("novel_id = ?", 7).Count(&count).Error)
	assert.Equal(t, int64(1), count, "同书单行覆盖，不重复落行")
	got, err = s.GetCheckerConfig(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, 0.0, got.BeatDensityMin)
	assert.True(t, got.HookRequired(), "nil=缺省 true")

	// 跨书隔离。
	other, err := s.GetCheckerConfig(ctx, 8)
	require.NoError(t, err)
	assert.True(t, other.HookRequired())
	assert.Empty(t, other.Taboos())

	// 契约拒收不落库。
	bad := &CheckerConfig{NovelID: 9, WindowN: -1}
	require.Error(t, s.SaveCheckerConfig(ctx, bad))
	got, err = s.GetCheckerConfig(ctx, 9)
	require.NoError(t, err)
	assert.Equal(t, 0, got.OverdueGrace())
}
