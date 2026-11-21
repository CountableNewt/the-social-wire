package readstate

import (
	"sync"
	"time"
)

// VerifiedRecordCache is PDSReadStateVerifiedRecordCache: one byte budget for
// immutable v1/v2 records, keyed by the full viewer URI/CID.
type VerifiedRecordCache struct {
	mu             sync.Mutex
	maximumBytes   int
	maximumEntries int
	ttl            time.Duration
	entries        map[Reference]verifiedEntry
	bytes          int
}

type verifiedEntry struct {
	json      []byte
	size      int
	expiresAt time.Time
}

// NewVerifiedRecordCache uses Swift's defaults when given 8 MiB, 512, 1h.
func NewVerifiedRecordCache(maximumBytes, maximumEntries int, ttl time.Duration) *VerifiedRecordCache {
	return &VerifiedRecordCache{maximumBytes: max(0, maximumBytes), maximumEntries: max(0, maximumEntries),
		ttl: max(0, ttl), entries: map[Reference]verifiedEntry{}}
}

// NewDefaultVerifiedRecordCache is the 8 MiB / 512 entry / one hour cache.
func NewDefaultVerifiedRecordCache() *VerifiedRecordCache {
	return NewVerifiedRecordCache(8*1024*1024, 512, time.Hour)
}

// Value returns the cached record bytes when unexpired.
func (c *VerifiedRecordCache) Value(reference Reference, now time.Time) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[reference]
	if !ok {
		return nil, false
	}
	if !entry.expiresAt.After(now) {
		c.remove(reference)
		return nil, false
	}
	return entry.json, true
}

// Insert verifies the record against its CID before caching it.
func (c *VerifiedRecordCache) Insert(data []byte, reference Reference, now time.Time) error {
	if len(data) > MaximumRecordBytes {
		return ErrSizeLimit
	}
	if err := VerifyRecordCID(data, reference.CID); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(data) > c.maximumBytes || c.maximumEntries <= 0 || c.ttl <= 0 {
		return nil
	}
	c.remove(reference)
	for key, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			c.remove(key)
		}
	}
	for c.bytes+len(data) > c.maximumBytes || len(c.entries) >= c.maximumEntries {
		oldest, found := oldestKey(c.entries, func(entry verifiedEntry) time.Time { return entry.expiresAt })
		if !found {
			break
		}
		c.remove(oldest)
	}
	stored := append([]byte(nil), data...)
	c.entries[reference] = verifiedEntry{json: stored, size: len(stored), expiresAt: now.Add(c.ttl)}
	c.bytes += len(stored)
	return nil
}

func (c *VerifiedRecordCache) remove(reference Reference) {
	if entry, ok := c.entries[reference]; ok {
		delete(c.entries, reference)
		c.bytes -= entry.size
	}
}

func oldestKey[V any](entries map[Reference]V, expiry func(V) time.Time) (Reference, bool) {
	var oldest Reference
	var oldestAt time.Time
	found := false
	for key, entry := range entries {
		at := expiry(entry)
		if !found || at.Before(oldestAt) {
			oldest, oldestAt, found = key, at, true
		}
	}
	return oldest, found
}
