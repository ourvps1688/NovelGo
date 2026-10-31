//go:build cgo

package rag

import (
	"container/list"
	"testing"
	"time"
)

// newTestCache 构造一个可直接设置容量 / TTL 的 embedCache，便于确定性测试。
func newTestCache(max int, ttl time.Duration) *embedCache {
	return &embedCache{
		ll:    list.New(),
		items: make(map[string]*list.Element, max),
		ttl:   ttl,
		max:   max,
	}
}

func TestEmbedCache_AddGet(t *testing.T) {
	c := newTestCache(embedCacheMaxEntries, embedCacheTTL)
	v := []float32{0.1, 0.2, 0.3}
	c.add("k1", v)
	got, ok := c.get("k1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(got) != len(v) {
		t.Fatalf("len = %d, want %d", len(got), len(v))
	}
	for i := range v {
		if got[i] != v[i] {
			t.Fatalf("value mismatch at %d: got %v want %v", i, got[i], v[i])
		}
	}
}

func TestEmbedCache_GetReturnsCopy(t *testing.T) {
	c := newTestCache(embedCacheMaxEntries, embedCacheTTL)
	v := []float32{1, 2, 3}
	c.add("k", v)
	got, _ := c.get("k")
	got[0] = 999 // 篡改返回值，不应影响缓存内部存储
	got2, _ := c.get("k")
	if got2[0] != 1 {
		t.Fatalf("cache was mutated via returned slice: got %v", got2[0])
	}
}

func TestEmbedCache_TTLExpiry(t *testing.T) {
	c := newTestCache(embedCacheMaxEntries, -time.Second) // 过期时间已在过去
	c.add("k", []float32{1})
	if _, ok := c.get("k"); ok {
		t.Fatal("expected expired entry to be a miss")
	}
}

func TestEmbedCache_LRUEviction(t *testing.T) {
	c := newTestCache(2, embedCacheTTL)
	c.add("a", []float32{1})
	c.add("b", []float32{2})
	c.add("c", []float32{3}) // 触发淘汰最久未用者 "a"
	if _, ok := c.get("a"); ok {
		t.Fatal("expected 'a' to be evicted")
	}
	if _, ok := c.get("b"); !ok {
		t.Fatal("expected 'b' present")
	}
	if _, ok := c.get("c"); !ok {
		t.Fatal("expected 'c' present")
	}
	// 访问 "b" 使其成为最近使用，再写入 "d" 应淘汰 "c"
	_, _ = c.get("b")
	c.add("d", []float32{4})
	if _, ok := c.get("c"); ok {
		t.Fatal("expected 'c' evicted after LRU touch of 'b'")
	}
	if _, ok := c.get("b"); !ok {
		t.Fatal("expected 'b' still present")
	}
}
