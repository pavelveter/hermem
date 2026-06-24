package algo

import (
	"sync"
)

// EmbeddingCache is a simple LRU cache for embeddings keyed by text.
type EmbeddingCache struct {
	mu       sync.RWMutex
	capacity int
	entries  map[string]*cacheEntry
	head     *cacheEntry
	tail     *cacheEntry
}

type cacheEntry struct {
	key   string
	value []float32
	prev  *cacheEntry
	next  *cacheEntry
}

func NewEmbeddingCache(capacity int) *EmbeddingCache {
	return &EmbeddingCache{capacity: capacity, entries: make(map[string]*cacheEntry)}
}

func (c *EmbeddingCache) Get(key string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.moveToFront(e)
	v := make([]float32, len(e.value))
	copy(v, e.value)
	return v, true
}

func (c *EmbeddingCache) Set(key string, value []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		e.value = append(e.value[:0], value...)
		c.moveToFront(e)
		return
	}
	e := &cacheEntry{key: key, value: append([]float32{}, value...)}
	c.entries[key] = e
	if c.head == nil {
		c.head = e
		c.tail = e
	} else {
		e.next = c.head
		c.head.prev = e
		c.head = e
	}
	for len(c.entries) > c.capacity {
		c.evict()
	}
}

func (c *EmbeddingCache) moveToFront(e *cacheEntry) {
	if c.head == e {
		return
	}
	if e.prev != nil {
		e.prev.next = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev = nil
	e.next = c.head
	c.head.prev = e
	c.head = e
}

func (c *EmbeddingCache) evict() {
	if c.tail == nil {
		return
	}
	oldTail := c.tail
	delete(c.entries, oldTail.key)
	if oldTail.prev != nil {
		oldTail.prev.next = nil
	} else {
		// Single-entry cache: detaching the only entry must also
		// release the head pointer; otherwise we'd keep a live
		// reference to oldTail via c.head.
		c.head = nil
	}
	c.tail = oldTail.prev
	// Drop dangling refs on the evicted entry so the GC can reclaim it
	// and a future caller who still holds a pointer can't walk back into
	// the live list.
	oldTail.prev = nil
	oldTail.next = nil
}
