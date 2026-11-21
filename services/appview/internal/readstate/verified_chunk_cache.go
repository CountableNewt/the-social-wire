package readstate

import (
	"sync"
	"time"
)

// VerifiedChunkCache is ReadStateVerifiedChunkCache: a disposable cache of
// immutable typed chunks already verified against their CID. Rebuilds still
// traverse and validate the complete referenced generation.
type VerifiedChunkCache struct {
	mu             sync.Mutex
	maximumBytes   int
	maximumEntries int
	ttl            time.Duration
	entries        map[Reference]chunkEntry
	bytes          int
}

type chunkEntry struct {
	chunk     Chunk
	size      int
	expiresAt time.Time
}

// NewVerifiedChunkCache builds a bounded cache.
func NewVerifiedChunkCache(maximumBytes, maximumEntries int, ttl time.Duration) *VerifiedChunkCache {
	return &VerifiedChunkCache{maximumBytes: max(0, maximumBytes), maximumEntries: max(0, maximumEntries),
		ttl: max(0, ttl), entries: map[Reference]chunkEntry{}}
}

// Value returns the cached chunk when unexpired.
func (c *VerifiedChunkCache) Value(reference Reference, now time.Time) (Chunk, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[reference]
	if !ok {
		return Chunk{}, false
	}
	if !entry.expiresAt.After(now) {
		c.remove(reference)
		return Chunk{}, false
	}
	return entry.chunk, true
}

// InsertVerified caches a chunk whose raw JSON already passed VerifyRecordCID.
func (c *VerifiedChunkCache) InsertVerified(chunk Chunk, reference Reference, now time.Time) error {
	size, err := EncodedByteCount(chunk)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if size > c.maximumBytes || c.maximumEntries <= 0 || c.ttl <= 0 {
		return nil
	}
	c.remove(reference)
	for key, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			c.remove(key)
		}
	}
	for c.bytes+size > c.maximumBytes || len(c.entries) >= c.maximumEntries {
		oldest, found := oldestKey(c.entries, func(entry chunkEntry) time.Time { return entry.expiresAt })
		if !found {
			break
		}
		c.remove(oldest)
	}
	c.entries[reference] = chunkEntry{chunk: chunk, size: size, expiresAt: now.Add(c.ttl)}
	c.bytes += size
	return nil
}

func (c *VerifiedChunkCache) remove(reference Reference) {
	if entry, ok := c.entries[reference]; ok {
		delete(c.entries, reference)
		c.bytes -= entry.size
	}
}
