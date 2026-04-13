package cachex

import (
	"container/list"
	"sync"
	"time"

	"github.com/Xhofe/go-cache"
)

type BoundedTTLCache[V any] struct {
	store cache.ICache[V]

	mu      sync.Mutex
	lru     *list.List
	entries map[string]*list.Element

	maxEntries int
}

type lruEntry struct {
	key string
}

func NewBoundedTTLCache[V any](maxEntries, shards int) *BoundedTTLCache[V] {
	if shards <= 0 {
		shards = 1
	}
	if maxEntries < 0 {
		maxEntries = 0
	}
	c := &BoundedTTLCache[V]{
		lru:        list.New(),
		entries:    make(map[string]*list.Element),
		maxEntries: maxEntries,
	}
	c.store = cache.NewMemCache[V](
		cache.WithShards[V](shards),
		cache.WithClearInterval[V](time.Minute),
		cache.WithExpiredCallback[V](func(key string, _ V) error {
			c.removeIndex(key)
			return nil
		}),
	)
	return c
}

func (c *BoundedTTLCache[V]) Get(key string) (V, bool) {
	v, ok := c.store.Get(key)
	if ok {
		c.touch(key)
	}
	return v, ok
}

func (c *BoundedTTLCache[V]) GetDel(key string) (V, bool) {
	v, ok := c.store.GetDel(key)
	if ok {
		c.removeIndex(key)
	}
	return v, ok
}

func (c *BoundedTTLCache[V]) Set(key string, value V, ttl time.Duration) bool {
	if key == "" || ttl <= 0 {
		c.Del(key)
		return false
	}
	if ok := c.store.Set(key, value, cache.WithEx[V](ttl)); !ok {
		return false
	}
	c.touch(key)
	c.evictIfNeeded()
	return true
}

func (c *BoundedTTLCache[V]) Del(keys ...string) int {
	count := c.store.Del(keys...)
	for _, key := range keys {
		c.removeIndex(key)
	}
	return count
}

func (c *BoundedTTLCache[V]) Clear() {
	c.store.Clear()
	c.mu.Lock()
	c.lru.Init()
	c.entries = make(map[string]*list.Element)
	c.mu.Unlock()
}

func (c *BoundedTTLCache[V]) TrimTo(maxEntries int) int {
	if c == nil {
		return 0
	}
	if maxEntries < 0 {
		maxEntries = 0
	}
	removed := 0
	for {
		c.mu.Lock()
		if len(c.entries) <= maxEntries {
			c.mu.Unlock()
			return removed
		}
		ele := c.lru.Back()
		if ele == nil {
			c.mu.Unlock()
			return removed
		}
		entry, _ := ele.Value.(lruEntry)
		c.lru.Remove(ele)
		delete(c.entries, entry.key)
		c.mu.Unlock()
		c.store.Del(entry.key)
		removed++
	}
}

func (c *BoundedTTLCache[V]) Exists(keys ...string) bool {
	return c.store.Exists(keys...)
}

func (c *BoundedTTLCache[V]) Expire(key string, ttl time.Duration) bool {
	if ttl <= 0 {
		c.Del(key)
		return false
	}
	return c.store.Expire(key, ttl)
}

func (c *BoundedTTLCache[V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *BoundedTTLCache[V]) touch(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ele, ok := c.entries[key]; ok {
		c.lru.MoveToFront(ele)
		return
	}
	ele := c.lru.PushFront(lruEntry{key: key})
	c.entries[key] = ele
}

func (c *BoundedTTLCache[V]) removeIndex(key string) {
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ele, ok := c.entries[key]
	if !ok {
		return
	}
	c.lru.Remove(ele)
	delete(c.entries, key)
}

func (c *BoundedTTLCache[V]) evictIfNeeded() {
	if c.maxEntries <= 0 {
		return
	}
	for {
		c.mu.Lock()
		if len(c.entries) <= c.maxEntries {
			c.mu.Unlock()
			return
		}
		ele := c.lru.Back()
		if ele == nil {
			c.mu.Unlock()
			return
		}
		entry, _ := ele.Value.(lruEntry)
		c.lru.Remove(ele)
		delete(c.entries, entry.key)
		c.mu.Unlock()
		c.store.Del(entry.key)
	}
}
