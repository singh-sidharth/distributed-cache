package cache

import "time"

// Entry represents a single cache entry with TTL support
type Entry struct {
	Key        string
	Value      []byte
	ExpiresAt  time.Time // Zero value means no expiration
	CreatedAt  time.Time
	AccessedAt time.Time
}

// IsExpired checks if the entry has expired
func (e *Entry) IsExpired() bool {
	if e.ExpiresAt.IsZero() {
		return false // No expiration set
	}
	return time.Now().After(e.ExpiresAt)
}

// UpdateAccess updates the last accessed timestamp
func (e *Entry) UpdateAccess() {
	e.AccessedAt = time.Now()
}
