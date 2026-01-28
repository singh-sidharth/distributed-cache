package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRUCache_BasicOperations(t *testing.T) {
	cache := NewLRUCache(3)

	// Test Set and Get
	err := cache.Set("key1", []byte("value1"), 0)
	require.NoError(t, err, "Set should not return error")

	val, err := cache.Get("key1")
	require.NoError(t, err, "Get should not return error for existing key")
	assert.Equal(t, []byte("value1"), val, "Retrieved value should match")

	// Test Size
	assert.Equal(t, 1, cache.Size(), "Cache size should be 1")
}

func TestLRUCache_KeyNotFound(t *testing.T) {
	cache := NewLRUCache(3)

	_, err := cache.Get("nonexistent")
	assert.ErrorIs(t, err, ErrKeyNotFound, "Should return ErrKeyNotFound for non-existent key")
}

func TestLRUCache_TTL(t *testing.T) {
	cache := NewLRUCache(3)

	// Set with short TTL
	err := cache.Set("key1", []byte("value1"), 100*time.Millisecond)
	require.NoError(t, err, "Set with TTL should not return error")

	// Should exist immediately
	val, err := cache.Get("key1")
	require.NoError(t, err, "Get should succeed before TTL expires")
	assert.Equal(t, []byte("value1"), val, "Value should match before expiration")

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	_, err = cache.Get("key1")
	assert.ErrorIs(t, err, ErrKeyNotFound, "Should return ErrKeyNotFound after TTL expires")

	// Verify key was removed from the cache
	assert.Equal(t, 0, cache.Size(), "Cache should be empty after TTL expiration")
}

func TestLRUCache_Eviction(t *testing.T) {
	cache := NewLRUCache(2) // Max 2 items

	err := cache.Set("key1", []byte("value1"), 0)
	require.NoError(t, err, "First Set should not return error")

	err = cache.Set("key2", []byte("value2"), 0)
	require.NoError(t, err, "Second Set should not return error")

	err = cache.Set("key3", []byte("value3"), 0) // Should evict key1
	require.NoError(t, err, "Third Set should not return error")

	// key1 should be evicted
	_, err = cache.Get("key1")
	assert.ErrorIs(t, err, ErrKeyNotFound, "key1 should be evicted (LRU)")

	// key2 and key3 should exist
	val2, err := cache.Get("key2")
	require.NoError(t, err, "key2 should still exist")
	assert.Equal(t, []byte("value2"), val2, "key2 value should match")

	val3, err := cache.Get("key3")
	require.NoError(t, err, "key3 should exist")
	assert.Equal(t, []byte("value3"), val3, "key3 value should match")

	// Verify eviction stat
	stats := cache.Stats()
	assert.Equal(t, uint64(1), stats.Evictions, "Should have 1 eviction")
}

func TestLRUCache_LRUOrder(t *testing.T) {
	cache := NewLRUCache(2)

	err := cache.Set("key1", []byte("value1"), 0)
	require.NoError(t, err, "Set key1 should not return error")

	err = cache.Set("key2", []byte("value2"), 0)
	require.NoError(t, err, "Set key2 should not return error")

	// Access key1 (makes it most recently used)
	val, err := cache.Get("key1")
	require.NoError(t, err, "Get key1 should not return error")
	assert.Equal(t, []byte("value1"), val, "key1 value should match")

	// Add key3 (should evict key2, not key1)
	err = cache.Set("key3", []byte("value3"), 0)
	require.NoError(t, err, "Set key3 should not return error")

	// key2 should be evicted
	_, err = cache.Get("key2")
	assert.ErrorIs(t, err, ErrKeyNotFound, "key2 should be evicted (was LRU)")

	// key1 should still exist
	val1, err := cache.Get("key1")
	require.NoError(t, err, "key1 should still exist (was accessed)")
	assert.Equal(t, []byte("value1"), val1, "key1 value should match")

	// key3 should exist
	val3, err := cache.Get("key3")
	require.NoError(t, err, "key3 should exist")
	assert.Equal(t, []byte("value3"), val3, "key3 value should match")
}

func TestLRUCache_Delete(t *testing.T) {
	cache := NewLRUCache(3)

	err := cache.Set("key1", []byte("value1"), 0)
	require.NoError(t, err, "Set should not return error")

	// Delete existing key
	deleted := cache.Delete("key1")
	assert.True(t, deleted, "Delete should return true for existing key")

	// Key should not exist
	_, err = cache.Get("key1")
	assert.ErrorIs(t, err, ErrKeyNotFound, "Deleted key should not be found")

	// Delete non-existent key
	deleted = cache.Delete("key1")
	assert.False(t, deleted, "Delete should return false for non-existent key")

	// Verify size
	assert.Equal(t, 0, cache.Size(), "Cache should be empty after deletion")
}

func TestLRUCache_Clear(t *testing.T) {
	cache := NewLRUCache(3)

	err := cache.Set("key1", []byte("value1"), 0)
	require.NoError(t, err, "Set key1 should not return error")

	err = cache.Set("key2", []byte("value2"), 0)
	require.NoError(t, err, "Set key2 should not return error")

	assert.Equal(t, 2, cache.Size(), "Cache should have 2 items before clear")

	cache.Clear()

	assert.Equal(t, 0, cache.Size(), "Cache should be empty after clear")

	_, err = cache.Get("key1")
	assert.ErrorIs(t, err, ErrKeyNotFound, "key1 should not exist after clear")

	_, err = cache.Get("key2")
	assert.ErrorIs(t, err, ErrKeyNotFound, "key2 should not exist after clear")

	// Verify stats are reset
	stats := cache.Stats()
	assert.Equal(t, uint64(0), stats.Hits, "Hits should be reset to 0")
	assert.Equal(t, uint64(2), stats.Misses, "Misses should be reset to 2")
	assert.Equal(t, uint64(0), stats.Evictions, "Evictions should be reset to 0")
}

func TestLRUCache_Stats(t *testing.T) {
	cache := NewLRUCache(3)

	err := cache.Set("key1", []byte("value1"), 0)
	require.NoError(t, err, "Set should not return error")

	val, err := cache.Get("key1") // Hit
	require.NoError(t, err, "Get should not return error")
	assert.Equal(t, []byte("value1"), val, "Value should match")

	_, err = cache.Get("key2") // Miss
	assert.ErrorIs(t, err, ErrKeyNotFound, "Get non-existent key should return error")

	stats := cache.Stats()
	assert.Equal(t, uint64(1), stats.Hits, "Should have 1 hit")
	assert.Equal(t, uint64(1), stats.Misses, "Should have 1 miss")
	assert.Equal(t, 1, stats.TotalKeys, "Should have 1 key")
	assert.Greater(t, stats.MemoryBytes, int64(0), "Memory usage should be > 0")
}

func TestLRUCache_UpdateExistingKey(t *testing.T) {
	cache := NewLRUCache(3)

	// Set initial value
	err := cache.Set("key1", []byte("value1"), 0)
	require.NoError(t, err, "Initial Set should not return error")

	// Update with a new value
	err = cache.Set("key1", []byte("value2"), 0)
	require.NoError(t, err, "Update Set should not return error")

	// Verify updated value
	val, err := cache.Get("key1")
	require.NoError(t, err, "Get should not return error")
	assert.Equal(t, []byte("value2"), val, "Value should be updated")

	// Verify size hasn't changed
	assert.Equal(t, 1, cache.Size(), "Size should still be 1")
}

func TestLRUCache_EmptyValue(t *testing.T) {
	cache := NewLRUCache(3)

	// Test with an empty byte slice
	err := cache.Set("empty", []byte{}, 0)
	require.NoError(t, err, "Set with empty value should not return error")

	val, err := cache.Get("empty")
	require.NoError(t, err, "Get should not return error for empty value")
	assert.Equal(t, []byte{}, val, "Empty value should be retrievable")
}

func TestLRUCache_NilValue(t *testing.T) {
	cache := NewLRUCache(3)

	// Test with nil value
	err := cache.Set("nil", nil, 0)
	require.NoError(t, err, "Set with nil value should not return error")

	val, err := cache.Get("nil")
	require.NoError(t, err, "Get should not return error for nil value")
	assert.Nil(t, val, "Nil value should be retrievable")
}
