package cache

import (
	"container/list"
	"sync"
	"time"
)

// LRUCache implements Cache with LRU eviction policy
type LRUCache struct {
	mu           sync.RWMutex
	maxSize      int                      // Maximum number of entries
	items        map[string]*list.Element // Key to list element mapping
	evictionList *list.List               // Doubly linked list for LRU
	stats        Stats
}

// lruEntry wraps Entry with list element
type lruEntry struct {
	entry *Entry
}

// NewLRUCache creates a new LRU cache with the given maximum size
func NewLRUCache(maxSize int) *LRUCache {
	return &LRUCache{
		maxSize:      maxSize,
		items:        make(map[string]*list.Element),
		evictionList: list.New(),
		stats:        Stats{},
	}
}

// Get retrieves a value by key
func (c *LRUCache) Get(key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, found := c.items[key]
	if !found {
		c.stats.Misses++
		return nil, ErrKeyNotFound
	}

	entry := elem.Value.(*lruEntry).entry

	// Check expiration
	if entry.IsExpired() {
		c.removeElement(elem)
		c.stats.Misses++
		return nil, ErrKeyNotFound
	}

	// Move to the front (most recently used)
	c.evictionList.MoveToFront(elem)
	entry.UpdateAccess()

	c.stats.Hits++
	return entry.Value, nil
}

// Set stores a key-value pair with optional TTL
func (c *LRUCache) Set(key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}

	// Check if the key already exists
	if elem, found := c.items[key]; found {
		// Update existing entry
		entry := elem.Value.(*lruEntry).entry
		entry.Value = value
		entry.ExpiresAt = expiresAt
		entry.AccessedAt = now
		c.evictionList.MoveToFront(elem)
		return nil
	}

	// Evict if at capacity
	if c.evictionList.Len() >= c.maxSize {
		c.evictOldest()
	}

	// Add a new entry
	entry := &Entry{
		Key:        key,
		Value:      value,
		ExpiresAt:  expiresAt,
		CreatedAt:  now,
		AccessedAt: now,
	}

	elem := c.evictionList.PushFront(&lruEntry{entry: entry})
	c.items[key] = elem

	return nil
}

// Delete removes a key from the cache
func (c *LRUCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, found := c.items[key]
	if !found {
		return false
	}

	c.removeElement(elem)
	return true
}

// Size returns the number of keys in the cache
func (c *LRUCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Clear removes all entries from the cache
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.evictionList.Init()
	c.stats = Stats{}
}

// Stats returns cache statistics
func (c *LRUCache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	stats.TotalKeys = len(c.items)

	// Approximate memory usage
	for _, elem := range c.items {
		entry := elem.Value.(*lruEntry).entry
		stats.MemoryBytes += int64(len(entry.Key) + len(entry.Value))
	}

	return stats
}

// evictOldest removes the least recently used entry
func (c *LRUCache) evictOldest() {
	elem := c.evictionList.Back()
	if elem != nil {
		c.removeElement(elem)
		c.stats.Evictions++
	}
}

// removeElement removes an element from the cache
func (c *LRUCache) removeElement(elem *list.Element) {
	c.evictionList.Remove(elem)
	entry := elem.Value.(*lruEntry).entry
	delete(c.items, entry.Key)
}
