package main

import "sync"

// EmbeddingCache is a simple in-memory LRU cache for entity embeddings.
// Reduces repeated DB reads during batch retrieval by caching recently
// accessed embeddings in the vector index layer.
type EmbeddingCache struct {
	mu       sync.Mutex
	entries  map[string]*cacheEntry
	head     *cacheEntry
	tail     *cacheEntry
	capacity int
}

type cacheEntry struct {
	key  string
	vec  []float32
	prev *cacheEntry
	next *cacheEntry
}

// NewEmbeddingCache creates an LRU cache with the given capacity.
// Capacity 0 disables caching (all operations are no-ops).
func NewEmbeddingCache(capacity int) *EmbeddingCache {
	return &EmbeddingCache{
		entries:  make(map[string]*cacheEntry, capacity),
		capacity: capacity,
	}
}

// Get returns the cached embedding for key, or nil if not present.
func (c *EmbeddingCache) Get(key string) []float32 {
	if c.capacity == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		c.moveToFront(e)
		return e.vec
	}
	return nil
}

// Put stores an embedding in the cache. If the key already exists,
// the entry is updated and moved to the front.
func (c *EmbeddingCache) Put(key string, vec []float32) {
	if c.capacity == 0 || vec == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		e.vec = vec
		c.moveToFront(e)
		return
	}
	e := &cacheEntry{key: key, vec: vec}
	c.entries[key] = e
	c.addToFront(e)
	if len(c.entries) > c.capacity {
		c.removeTail()
	}
}

// Invalidate removes an entry from the cache (e.g., on entity update).
func (c *EmbeddingCache) Invalidate(key string) {
	if c.capacity == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		c.remove(e)
	}
}

// Size returns the current number of cached entries.
func (c *EmbeddingCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *EmbeddingCache) moveToFront(e *cacheEntry) {
	if c.head == e {
		return
	}
	c.remove(e)
	c.addToFront(e)
}

func (c *EmbeddingCache) addToFront(e *cacheEntry) {
	e.next = c.head
	e.prev = nil
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *EmbeddingCache) remove(e *cacheEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
}

func (c *EmbeddingCache) removeTail() {
	if c.tail == nil {
		return
	}
	delete(c.entries, c.tail.key)
	c.remove(c.tail)
}
