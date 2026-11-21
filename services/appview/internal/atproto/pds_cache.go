package atproto

import (
	"sync"
	"time"
)

// PDSCacheState classifies a PDSResolutionCache lookup.
type PDSCacheState int

// Lookup states (PDSResolutionCacheLookup).
const (
	PDSCacheMiss PDSCacheState = iota
	PDSCacheFresh
	PDSCacheStale
)

// PDSResolutionCache is the process-wide DID → PDS endpoint cache
// (InMemoryPDSResolutionCache): resolved entries stay fresh 30 minutes and
// expire after 6 hours; unresolved (negative) entries stay fresh 60 seconds
// and expire after 5 minutes. Leases let one caller refresh a DID while
// concurrent callers serve stale data or wait briefly for the winner.
type PDSResolutionCache struct {
	mu      sync.Mutex
	entries map[string]pdsCacheEntry
	leases  map[string]pdsLease
	nextID  uint64
	now     func() time.Time
}

type pdsCacheEntry struct {
	endpoint     string
	freshUntil   time.Time
	hardExpireAt time.Time
}

type pdsLease struct {
	id        uint64
	expiresAt time.Time
}

// PDSLease is a held refresh lease; release it with ReleaseLease.
type PDSLease struct {
	did string
	id  uint64
}

// NewPDSResolutionCache returns an empty cache.
func NewPDSResolutionCache() *PDSResolutionCache {
	return &PDSResolutionCache{
		entries: map[string]pdsCacheEntry{},
		leases:  map[string]pdsLease{},
		now:     time.Now,
	}
}

// SharedPDSResolutionCache is the process-wide default cache
// (PDSResolutionCacheRegistry.shared with no Redis installed).
var SharedPDSResolutionCache = NewPDSResolutionCache()

// Lookup returns the cache state and endpoint; a negative entry has endpoint "".
func (c *PDSResolutionCache) Lookup(did string) (PDSCacheState, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, found := c.entries[did]
	if !found {
		return PDSCacheMiss, ""
	}
	now := c.now()
	if !now.Before(entry.hardExpireAt) {
		delete(c.entries, did)
		return PDSCacheMiss, ""
	}
	if now.Before(entry.freshUntil) {
		return PDSCacheFresh, entry.endpoint
	}
	return PDSCacheStale, entry.endpoint
}

// StoreResolved caches a positive resolution.
func (c *PDSResolutionCache) StoreResolved(did, endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.entries[did] = pdsCacheEntry{endpoint: endpoint,
		freshUntil: now.Add(30 * time.Minute), hardExpireAt: now.Add(6 * time.Hour)}
}

// StoreUnresolved caches a negative resolution.
func (c *PDSResolutionCache) StoreUnresolved(did string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.entries[did] = pdsCacheEntry{freshUntil: now.Add(time.Minute), hardExpireAt: now.Add(5 * time.Minute)}
}

// AcquireLease grants a refresh lease for did unless an unexpired one is held.
func (c *PDSResolutionCache) AcquireLease(did string, ttl time.Duration) (PDSLease, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if lease, held := c.leases[did]; held && lease.expiresAt.After(now) {
		return PDSLease{}, false
	}
	c.nextID++
	c.leases[did] = pdsLease{id: c.nextID, expiresAt: now.Add(ttl)}
	return PDSLease{did: did, id: c.nextID}, true
}

// ReleaseLease drops lease if it is still the current holder.
func (c *PDSResolutionCache) ReleaseLease(lease PDSLease) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current, held := c.leases[lease.did]; held && current.id == lease.id {
		delete(c.leases, lease.did)
	}
}
