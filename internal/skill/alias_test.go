package skill

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAliasConfig_LoadSaveRoundtrip 验证写出后再读回内容一致（原子写、JSON 往返）。
func TestAliasConfig_LoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()

	cfg := &AliasConfig{
		Version: 1,
		Groups: []AuthorAliasGroup{
			{Canonical: "刘慈欣", Aliases: []string{"Liu Cixin", "刘慈欣 "}},
			{Canonical: "鲁迅", Aliases: []string{"周树人"}},
		},
	}
	require.NoError(t, SaveAliasConfig(dir, cfg))

	loaded, err := LoadAliasConfig(dir)
	require.NoError(t, err)
	require.Len(t, loaded.Groups, 2)
	assert.Equal(t, "刘慈欣", loaded.Groups[0].Canonical)
	assert.ElementsMatch(t, []string{"Liu Cixin", "刘慈欣 "}, loaded.Groups[0].Aliases)
	assert.Equal(t, "鲁迅", loaded.Groups[1].Canonical)
}

// TestAliasConfig_LoadMissing 文件不存在应返回空配置而非报错。
func TestAliasConfig_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadAliasConfig(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.Groups)
	assert.Equal(t, 1, cfg.Version)
}

// TestAliasConfig_ToMap 验证归一后的扁平映射：别名→规范名、规范名→自身。
func TestAliasConfig_ToMap(t *testing.T) {
	cfg := &AliasConfig{
		Version: 1,
		Groups: []AuthorAliasGroup{
			{Canonical: "刘慈欣", Aliases: []string{"Liu Cixin", "LIU   CIXIN"}},
		},
	}
	m := cfg.ToMap()
	// 规范名自身映射到自己（归一后）。
	assert.Equal(t, "刘慈欣", m["刘慈欣"])
	// 别名归一（大小写/空白）后映射到规范名。
	assert.Equal(t, "刘慈欣", m["liu cixin"])
	// 不同别名指向同一规范名。
	assert.Equal(t, "刘慈欣", m["liu cixin"])
}

// TestAliasConfig_Resolve 验证任意署名解析为规范名；未命中返回自身归一形式。
func TestAliasConfig_Resolve(t *testing.T) {
	cfg := &AliasConfig{
		Version: 1,
		Groups: []AuthorAliasGroup{
			{Canonical: "刘慈欣", Aliases: []string{"Liu Cixin"}},
		},
	}
	// 英文别名 → 规范名（大小写/空白无关）。
	assert.Equal(t, "刘慈欣", cfg.Resolve("Liu Cixin"))
	assert.Equal(t, "刘慈欣", cfg.Resolve("liu   cixin"))
	// 规范名自身保持不变。
	assert.Equal(t, "刘慈欣", cfg.Resolve("刘慈欣"))
	// 非命中别名返回自身归一形式。
	assert.Equal(t, "someone else", cfg.Resolve("Someone Else"))
}
