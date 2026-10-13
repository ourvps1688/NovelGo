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

	res := ReconcileAggregates(context.Background(), sources, []Skill{stale}, dir)

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

	res := ReconcileAggregates(context.Background(), nil, []Skill{orphan}, dir)

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

	res := ReconcileAggregates(context.Background(), sources, []Skill{*fresh}, dir)

	assert.Equal(t, 0, len(res.Rebuilt), "摘要未变不应重建")
	assert.Equal(t, 0, len(res.Deleted))
	assert.Equal(t, 1, res.UpToDate)
}

func TestReconcileAggregates_IgnoresNonAggregated(t *testing.T) {
	dir := t.TempDir()
	// 普通（非聚合）skill 不应被处理。
	plain := Skill{Name: "plain", Kind: "mental", SourceAuthor: "Liu", Content: "x"}
	res := ReconcileAggregates(context.Background(), nil, []Skill{plain}, dir)
	assert.Equal(t, 0, res.Checked)
	assert.Equal(t, 0, len(res.Rebuilt))
	assert.Equal(t, 0, len(res.Deleted))
}
