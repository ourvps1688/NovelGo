package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDerived 将派生 skill 序列化落盘，便于断言重建结果。
func writeDerived(t *testing.T, dir string, sk Skill) {
	t.Helper()
	_, err := WriteSkill(dir, &sk)
	require.NoError(t, err)
}

func TestReconcileAggregates_RebuildsStale(t *testing.T) {
	dir := t.TempDir()

	s1 := &Skill{Name: "a", Kind: "mental", SourceAuthor: "Liu", Content: "x: 1"}
	s2 := &Skill{Name: "b", Kind: "mental", SourceAuthor: "Liu", Content: "y: 2"}
	sources := []MergeSource{{Skill: s1}, {Skill: s2}}

	// 已存在派生档，但哈希陈旧（源已变化或尚未记录哈希）。
	stale := Skill{
		Name:                 "liu-mental",
		Kind:                 "mental",
		Author:               "aggregate",
		SourceAuthor:         "liu",
		Aggregated:           true,
		AggregatedFrom:       []string{"a", "b"},
		AggregatedAt:         "2026-01-01T00:00:00Z",
		AggregatedSourceHash: "stale-hash",
		Content:              "old body",
	}
	writeDerived(t, dir, stale)

	res := ReconcileAggregates(context.Background(), sources, []Skill{stale}, dir, nil)

	assert.Contains(t, res.Rebuilt, "liu-mental")
	assert.Equal(t, 1, res.Checked)
	assert.Equal(t, 0, len(res.Deleted))

	// 落盘文件被刷新：哈希更新、正文为新合并结果。
	reloaded, err := ParseFile(filepath.Join(dir, "liu-mental.md"))
	require.NoError(t, err)
	assert.NotEqual(t, "stale-hash", reloaded.AggregatedSourceHash)
	assert.NotEqual(t, "old body", reloaded.Content)
	assert.Equal(t, "liu", reloaded.SourceAuthor)
}

func TestReconcileAggregates_DeletesOrphan(t *testing.T) {
	dir := t.TempDir()

	// 派生档存在，但源已全部消失 → 应删除孤儿档。
	orphan := Skill{
		Name:                 "liu-mental",
		Kind:                 "mental",
		Author:               "aggregate",
		SourceAuthor:         "liu",
		Aggregated:           true,
		AggregatedSourceHash: "whatever",
		Content:              "old body",
	}
	writeDerived(t, dir, orphan)

	res := ReconcileAggregates(context.Background(), nil, []Skill{orphan}, dir, nil)

	assert.Contains(t, res.Deleted, "liu-mental")
	assert.Equal(t, 0, len(res.Rebuilt))
	_, statErr := os.Stat(filepath.Join(dir, "liu-mental.md"))
	assert.True(t, os.IsNotExist(statErr), "孤儿派生档应被删除")
}

func TestReconcileAggregates_UpToDateSkipped(t *testing.T) {
	dir := t.TempDir()

	s1 := &Skill{Name: "a", Kind: "mental", SourceAuthor: "Liu", Content: "x: 1"}
	s2 := &Skill{Name: "b", Kind: "mental", SourceAuthor: "Liu", Content: "y: 2"}
	sources := []MergeSource{{Skill: s1}, {Skill: s2}}

	// 用同样的源先生成一版「最新」派生档，作为 existing（哈希正确）。
	fresh, err := MergeMentalSkills(context.Background(), sources, MergeOptions{
		CanonicalAuthor: "liu",
		OutputDir:       dir,
		AggregatedBy:    "aggregate",
	})
	require.NoError(t, err)

	res := ReconcileAggregates(context.Background(), sources, []Skill{*fresh}, dir, nil)

	assert.Equal(t, 0, len(res.Rebuilt), "摘要未变不应重建")
	assert.Equal(t, 0, len(res.Deleted))
	assert.Equal(t, 1, res.UpToDate)
}

func TestReconcileAggregates_IgnoresNonAggregated(t *testing.T) {
	dir := t.TempDir()
	// 普通（非聚合）skill 不应被处理。
	plain := Skill{Name: "plain", Kind: "mental", SourceAuthor: "Liu", Content: "x"}
	res := ReconcileAggregates(context.Background(), nil, []Skill{plain}, dir, nil)
	assert.Equal(t, 0, res.Checked)
	assert.Equal(t, 0, len(res.Rebuilt))
	assert.Equal(t, 0, len(res.Deleted))
}

// TestReconcileAggregates_AliasGrouping 验证：同一作者的两种署名（英文 "Liu Cixin"
// 与中文 "刘慈欣"）经别名映射归并为同一规范名后，被当作同一组处理。
func TestReconcileAggregates_AliasGrouping(t *testing.T) {
	dir := t.TempDir()

	aliasMap := (&AliasConfig{Version: 1, Groups: []AuthorAliasGroup{
		{Canonical: "刘慈欣", Aliases: []string{"Liu Cixin"}},
	}}).ToMap()

	// 源：一份署名 "Liu Cixin"，另一份署名 "刘慈欣"（实为同一人）。
	s1 := &Skill{Name: "a", Kind: "mental", SourceAuthor: "Liu Cixin", Content: "x: 1"}
	s2 := &Skill{Name: "b", Kind: "mental", SourceAuthor: "刘慈欣", Content: "y: 2"}
	sources := []MergeSource{{Skill: s1}, {Skill: s2}}

	// 已存在的派生档，其 SourceAuthor 按其中一种署名归一（"liu cixin"）。
	existing := Skill{
		Name:                 "liu-cixin-mental",
		Kind:                 "mental",
		Author:               "aggregate",
		SourceAuthor:         "liu cixin",
		Aggregated:           true,
		AggregatedFrom:       []string{"a", "b"},
		AggregatedSourceHash: "stale",
		Content:              "old",
	}
	writeDerived(t, dir, existing)

	res := ReconcileAggregates(context.Background(), sources, []Skill{existing}, dir, aliasMap)
	assert.Contains(t, res.Rebuilt, "liu-cixin-mental")

	// 别名归一改变了规范名，旧名文件应被清理，避免遗留孤儿档。
	_, statErr := os.Stat(filepath.Join(dir, "liu-cixin-mental.md"))
	assert.True(t, os.IsNotExist(statErr), "旧规范名文件应随别名归一被移除")

	// 目录中仅剩唯一派生档，其 SourceAuthor 归并为规范名 "刘慈欣"，
	// 证明两种署名被分到同一组、而非各自独立。
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var mdFiles []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			mdFiles = append(mdFiles, e.Name())
		}
	}
	require.Len(t, mdFiles, 1, "应只剩唯一派生档")
	reloaded, err := ParseFile(filepath.Join(dir, mdFiles[0]))
	require.NoError(t, err)
	assert.Equal(t, "刘慈欣", reloaded.SourceAuthor, "别名应把 Liu Cixin 与 刘慈欣 归并为同一规范名")
}
