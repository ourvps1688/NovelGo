//go:build cgo

package rag

import (
	"container/list"
	"sync"
	"time"
)

// embedCache 是「查询向量」（query-mode embedding）的短 TTL + 容量上限缓存。
//
// 为什么只缓存查询向量：
//   - 查询（带 BGE 指令前缀的 Embed / EmbedBatchQuery）在 Agent 多轮对话中会反复出现，
//     命中缓存即可跳过 ONNX 推理，直接降 CPU / 延迟——这是 P0-2 的核心收益。
//   - 文档向量（IndexChunks 的 EmbedBatch）每个 chunk 唯一、一次性，缓存纯属内存开销，故绕过。
//
// 安全性：embedding 是「文本 + 模型」的纯函数，TTL 内不会因「数据变化」而失效，
// 因此短 TTL 仅用于给内存封顶，不引入语义陈旧风险。命中返回副本，防止调用方误改缓存切片。
const (
	embedCacheTTL        = 5 * time.Minute
	embedCacheMaxEntries = 2048
)

type embedCacheEntry struct {
	key string
	vec []float32
	exp time.Time
}

// embedCache 是非线程安全结构；所有访问由 OnnxEmbedder 的调用方加锁（此处自带互斥锁）。
type embedCache struct {
	mu    sync.Mutex
	ll    *list.List // LRU：队首 = 最近使用
	items map[string]*list.Element
	ttl   time.Duration
	max   int
}

func newEmbedCache() *embedCache {
	return &embedCache{
		ll:    list.New(),
		items: make(map[string]*list.Element, embedCacheMaxEntries),
		ttl:   embedCacheTTL,
		max:   embedCacheMaxEntries,
	}
}

// get 命中且未过期时返回向量副本；否则（未命中 / 已过期）返回 (nil, false)。
func (c *embedCache) get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	ent := el.Value.(*embedCacheEntry)
	if time.Now().After(ent.exp) {
		c.removeLocked(el)
		return nil, false
	}
	c.ll.MoveToFront(el)
	v := make([]float32, len(ent.vec))
	copy(v, ent.vec)
	return v, true
}

// add 写入或刷新一条缓存。已存在则覆盖向量并续期；达到容量上限时淘汰最久未用者。
func (c *embedCache) add(key string, vec []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		ent := el.Value.(*embedCacheEntry)
		ent.vec = append(ent.vec[:0], vec...)
		ent.exp = time.Now().Add(c.ttl)
		c.ll.MoveToFront(el)
		return
	}
	if c.ll.Len() >= c.max {
		if oldest := c.ll.Back(); oldest != nil {
			c.removeLocked(oldest)
		}
	}
	cp := make([]float32, len(vec))
	copy(cp, vec)
	ent := &embedCacheEntry{key: key, vec: cp, exp: time.Now().Add(c.ttl)}
	el := c.ll.PushFront(ent)
	c.items[key] = el
}

func (c *embedCache) removeLocked(el *list.Element) {
	c.ll.Remove(el)
	delete(c.items, el.Value.(*embedCacheEntry).key)
}
