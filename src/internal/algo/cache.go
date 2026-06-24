package algo

import (
	"strings"
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
	delete(c.entries, c.tail.key)
	if c.tail.prev != nil {
		c.tail.prev.next = nil
	}
	c.tail = c.tail.prev
}

// IsContradiction checks if two texts are contradictory via simple negation heuristics.
func IsContradiction(a, b string) bool {
	negWords := []string{"not", "don't", "doesn't", "isn't", "aren't", "won't", "can't", "never", "no", "hate", "dislike"}
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	for _, n := range negWords {
		if strings.Contains(al, n) != strings.Contains(bl, n) {
			return true
		}
	}
	return false
}
