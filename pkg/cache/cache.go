package cache

import (
	"errors"
	"time"
)

var (
	ErrKeyNotFound = errors.New("key not found")
	// ErrCacheFull = errors.New("cache is full")
)

// Cache defines the interface for cache operations
type Cache interface {
	// Get retrieves a value by key
	// Returns ErrKeyNotFound if the key doesn't exist or is expired
	Get(key string) ([]byte, error)

	// Set stores a key-value pair with optional TTL
	//  of 0 means no expiration
	Set(key string, value []byte, ttl time.Duration) error

	// Delete removes a key from the cache
	// Returns true if the key existed, false otherwise
	Delete(key string) bool

	// Size returns the number of keys in the cache
	Size() int

	// Clear removes all entries from the cache
	Clear()

	// Stats returns cache statistics
	Stats() Stats
}

// Stats contains cache statistics
type Stats struct {
	Hits        uint64 // Number of cache hits
	Misses      uint64 // Number of cache misses
	Evictions   uint64 // Number of evictions
	TotalKeys   int    // Current number of keys
	MemoryBytes int64  // Approximate memory usage
}
