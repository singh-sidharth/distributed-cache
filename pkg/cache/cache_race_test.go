package cache

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLRUCacheRace_ConcurrentReadWrite tests true concurrent access to shared keys
func TestLRUCacheRace_ConcurrentReadWrite(t *testing.T) {
	cache := NewLRUCache(100)
	const (
		numWriters    = 5
		numReaders    = 5
		numOperations = 1000
		numKeys       = 50 // Shared keys to create contention
	)

	var wg sync.WaitGroup
	errChan := make(chan error, numWriters+numReaders)

	// Launch writers - all writing to the same key pool
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key%d", j%numKeys)
				value := fmt.Sprintf("worker%d-op%d", workerID, j)

				if err := cache.Set(key, []byte(value), 0); err != nil {
					errChan <- fmt.Errorf("writer %d: Set failed: %w", workerID, err)
					return
				}
			}
		}(i)
	}

	// Launch readers - all reading from the same key pool
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key%d", j%numKeys)

				_, err := cache.Get(key)
				if err != nil && !errors.Is(err, ErrKeyNotFound) {
					errChan <- fmt.Errorf("reader %d: unexpected error: %w", readerID, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errorList
	var errorList []error
	for err := range errChan {
		errorList = append(errorList, err)
	}
	require.Empty(t, errorList, "Concurrent operations produced errorList: %v", errorList)

	// Verify cache integrity
	assert.Greater(t, cache.Size(), 0, "Cache should contain items")
	assert.LessOrEqual(t, cache.Size(), 100, "Cache should respect max size")

	// Verify cache still works correctly
	testErr := cache.Set("post-test-key", []byte("test-value"), 0)
	require.NoError(t, testErr, "Cache should work after concurrent operations")

	val, testErr := cache.Get("post-test-key")
	require.NoError(t, testErr, "Should retrieve value after concurrent operations")
	assert.Equal(t, []byte("test-value"), val, "Retrieved value should match")
}

// TestLRUCacheRace_MixedOperations tests Set/Get/Delete on the same keys concurrently
func TestLRUCacheRace_MixedOperations(t *testing.T) {
	cache := NewLRUCache(50)
	const (
		numWriters    = 4
		numReaders    = 4
		numDeleters   = 4
		numOperations = 500
		numKeys       = 30 // Low number = high contention
	)

	var wg sync.WaitGroup
	var opsCompleted atomic.Uint64
	errChan := make(chan error, numWriters+numReaders+numDeleters)

	// Writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key%d", j%numKeys)
				value := fmt.Sprintf("value-%d-%d", workerID, j)

				if err := cache.Set(key, []byte(value), 0); err != nil {
					errChan <- fmt.Errorf("writer %d: Set failed: %w", workerID, err)
					return
				}
				opsCompleted.Add(1)
			}
		}(i)
	}

	// Readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key%d", j%numKeys)

				_, err := cache.Get(key)
				if err != nil && !errors.Is(err, ErrKeyNotFound) {
					errChan <- fmt.Errorf("reader %d: unexpected error: %w", readerID, err)
					return
				}
				opsCompleted.Add(1)
			}
		}(i)
	}

	// Deleters
	for i := 0; i < numDeleters; i++ {
		wg.Add(1)
		go func(deleterID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key%d", j%numKeys)
				cache.Delete(key) // Returns bool, not error
				opsCompleted.Add(1)
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errorMessages
	var errorMessages []error
	for err := range errChan {
		errorMessages = append(errorMessages, err)
	}
	require.Empty(t, errorMessages, "Mixed operations produced errorMessages: %v", errorMessages)

	// Verify all operations completed
	expectedOps := uint64((numWriters + numReaders + numDeleters) * numOperations)
	assert.Equal(t, expectedOps, opsCompleted.Load(), "All operations should complete")

	// Verify cache still works
	err := cache.Set("verification", []byte("test"), 0)
	require.NoError(t, err, "Cache should work after mixed concurrent operations")

	val, err := cache.Get("verification")
	require.NoError(t, err, "Should retrieve verification value")
	assert.Equal(t, []byte("test"), val, "Verification value should match")
}

// TestLRUCacheRace_HighContentionSingleKey tests extreme contention on one key
func TestLRUCacheRace_HighContentionSingleKey(t *testing.T) {
	cache := NewLRUCache(100)
	const (
		numGoroutines = 20
		numOperations = 500
		hotKey        = "hot-key" // All goroutines fight over this one key
	)

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				value := fmt.Sprintf("worker%d-op%d", workerID, j)

				// Write to the hot key
				if err := cache.Set(hotKey, []byte(value), 0); err != nil {
					errChan <- fmt.Errorf("worker %d: Set failed: %w", workerID, err)
					return
				}

				// Read from the hot key
				_, err := cache.Get(hotKey)
				if err != nil && !errors.Is(err, ErrKeyNotFound) {
					errChan <- fmt.Errorf("worker %d: Get failed: %w", workerID, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errorsList
	var errorsList []error
	for err := range errChan {
		errorsList = append(errorsList, err)
	}
	require.Empty(t, errorsList, "High contention test produced errorsList: %v", errorsList)

	// Verify the key exists and is readable
	val, err := cache.Get(hotKey)
	require.NoError(t, err, "Hot key should exist after concurrent access")
	assert.NotNil(t, val, "Hot key should have a value")
}

// TestLRUCacheRace_ConcurrentEviction tests eviction under concurrent loads
func TestLRUCacheRace_ConcurrentEviction(t *testing.T) {
	const maxSize = 10
	cache := NewLRUCache(maxSize)
	const (
		numGoroutines = 8
		numOperations = 500
	)

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				// Each worker uses unique keys to force eviction
				key := fmt.Sprintf("key-%d-%d", workerID, j)

				if err := cache.Set(key, []byte("value"), 0); err != nil {
					errChan <- fmt.Errorf("worker %d: Set failed: %w", workerID, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errorList
	var errorList []error
	for err := range errChan {
		errorList = append(errorList, err)
	}
	require.Empty(t, errorList, "Eviction test produced errorList: %v", errorList)

	// Verify cache constraints
	size := cache.Size()
	assert.LessOrEqual(t, size, maxSize, "Cache size (%d) should not exceed max size (%d)", size, maxSize)

	// Verify eviction occurred
	stats := cache.Stats()
	totalWrites := numGoroutines * numOperations
	assert.Greater(t, stats.Evictions, uint64(0), "Should have performed evictions")
	// With concurrent eviction, we expect:
	// - Evictions should be at least (totalWrites - maxSize) because we wrote more items than capacity
	// - The actual number might be >= this due to concurrent operations
	minExpectedEvictions := uint64(totalWrites - maxSize)
	assert.GreaterOrEqual(t, stats.Evictions, minExpectedEvictions, "Should have evicted excess items")
	assert.Equal(t, size, stats.TotalKeys, "Stats should reflect actual size")
}

// TestLRUCacheRace_StatsAccuracy tests that stats' counters remain accurate
func TestLRUCacheRace_StatsAccuracy(t *testing.T) {
	cache := NewLRUCache(100)
	const (
		numGoroutines = 10
		numOperations = 100
		numKeys       = 50
	)

	// Pre-populate half the keys
	for i := 0; i < numKeys/2; i++ {
		key := fmt.Sprintf("key%d", i)
		err := cache.Set(key, []byte("value"), 0)
		require.NoError(t, err, "Pre-population should not fail")
	}

	var wg sync.WaitGroup
	var expectedHits atomic.Uint64
	var expectedMisses atomic.Uint64
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				var key string
				if j%2 == 0 {
					// Access existing key (should hit)
					key = fmt.Sprintf("key%d", j%(numKeys/2))
				} else {
					// Access non-existent key (should miss)
					key = fmt.Sprintf("missing-%d-%d", workerID, j)
				}

				_, err := cache.Get(key)
				if err == nil {
					expectedHits.Add(1)
				} else if errors.Is(err, ErrKeyNotFound) {
					expectedMisses.Add(1)
				} else {
					errChan <- fmt.Errorf("worker %d: unexpected error: %w", workerID, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errorList
	var errorList []error
	for err := range errChan {
		errorList = append(errorList, err)
	}
	require.Empty(t, errorList, "Stats accuracy test produced errorList: %v", errorList)

	// Verify stats
	stats := cache.Stats()
	assert.Equal(t, expectedHits.Load(), stats.Hits,
		"Hit count mismatch: expected %d, got %d", expectedHits.Load(), stats.Hits)
	assert.Equal(t, expectedMisses.Load(), stats.Misses,
		"Miss count mismatch: expected %d, got %d", expectedMisses.Load(), stats.Misses)
}

// TestLRUCacheRace_TTLExpiration tests TTL behavior under concurrent access
func TestLRUCacheRace_TTLExpiration(t *testing.T) {
	cache := NewLRUCache(100)
	const (
		numWriters = 4
		numReaders = 4
		numKeys    = 50
		ttl        = 50 * time.Millisecond
	)

	var wg sync.WaitGroup
	errChan := make(chan error, numWriters+numReaders)

	// Writers setting keys with short TTL
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key%d", j%numKeys)
				value := fmt.Sprintf("value-%d-%d", workerID, j)

				if err := cache.Set(key, []byte(value), ttl); err != nil {
					errChan <- fmt.Errorf("writer %d: Set failed: %w", workerID, err)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Readers accessing keys (may hit or miss due to TTL)
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key%d", j%numKeys)

				_, err := cache.Get(key)
				if err != nil && !errors.Is(err, ErrKeyNotFound) {
					errChan <- fmt.Errorf("reader %d: unexpected error: %w", readerID, err)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errorList
	var errorList []error
	for err := range errChan {
		errorList = append(errorList, err)
	}
	require.Empty(t, errorList, "TTL test produced errorList: %v", errorList)

	// Wait for all TTLs to expire
	time.Sleep(100 * time.Millisecond)

	// All keys should be expired
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key%d", i)
		_, err := cache.Get(key)
		assert.ErrorIs(t, err, ErrKeyNotFound, "Key %s should be expired", key)
	}
}

// TestLRUCacheRace_LRUOrdering tests LRU ordering under concurrency
func TestLRUCacheRace_LRUOrdering(t *testing.T) {
	const maxSize = 5
	cache := NewLRUCache(maxSize)

	// Pre-populate cache
	for i := 0; i < maxSize; i++ {
		key := fmt.Sprintf("key%d", i)
		err := cache.Set(key, []byte("value"), 0)
		require.NoError(t, err, "Pre-population should not fail")
	}

	var wg sync.WaitGroup
	const numGoroutines = 4
	errChan := make(chan error, numGoroutines)

	// Multiple goroutines repeatedly accessing key0 to keep it "hot"
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, err := cache.Get("key0")
				if err != nil && !errors.Is(err, ErrKeyNotFound) {
					errChan <- fmt.Errorf("worker %d: unexpected error: %w", workerID, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errorList
	var errorList []error
	for err := range errChan {
		errorList = append(errorList, err)
	}
	require.Empty(t, errorList, "LRU ordering test produced errorList: %v", errorList)

	// Add a new key to trigger eviction
	err := cache.Set("key-new", []byte("new-value"), 0)
	require.NoError(t, err, "Adding new key should not fail")

	// key0 should still exist (frequently accessed)
	_, err = cache.Get("key0")
	assert.NoError(t, err, "key0 should still exist (frequently accessed)")

	// key1 should be evicted (LRU - never accessed)
	_, err = cache.Get("key1")
	assert.ErrorIs(t, err, ErrKeyNotFound, "key1 should be evicted (LRU)")
}

// TestLRUCacheRace_StressTest is a comprehensive stress test for race detector
func TestLRUCacheRace_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	cache := NewLRUCache(100)
	const (
		numGoroutines = 20
		duration      = 2 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	var opsCompleted atomic.Uint64
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(workerID)))

			for {
				select {
				case <-ctx.Done():
					return
				default:
					key := fmt.Sprintf("key%d", rng.Intn(50))

					switch rng.Intn(5) {
					case 0: // Set
						if err := cache.Set(key, []byte("value"), 0); err != nil {
							errChan <- fmt.Errorf("worker %d: Set failed: %w", workerID, err)
							return
						}
					case 1: // Get
						_, err := cache.Get(key)
						if err != nil && !errors.Is(err, ErrKeyNotFound) {
							errChan <- fmt.Errorf("worker %d: Get failed: %w", workerID, err)
							return
						}
					case 2: // Delete
						cache.Delete(key)
					case 3: // Stats
						_ = cache.Stats()
					case 4: // Size
						_ = cache.Size()
					}
					opsCompleted.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for errorList
	var errorList []error
	for err := range errChan {
		errorList = append(errorList, err)
	}
	require.Empty(t, errorList, "Stress test produced errorList: %v", errorList)

	t.Logf("Completed %d operations in %v", opsCompleted.Load(), duration)

	// Verify cache is still functional
	err := cache.Set("final-key", []byte("final-value"), 0)
	require.NoError(t, err, "Cache should work after stress test")

	val, err := cache.Get("final-key")
	require.NoError(t, err, "Should retrieve final value")
	assert.Equal(t, []byte("final-value"), val, "Final value should match")
}
