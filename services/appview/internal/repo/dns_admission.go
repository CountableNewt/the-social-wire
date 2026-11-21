package repo

import "sync"

// DNSResolverAdmission bounds concurrent blocking DNS resolutions. A slot is
// held until the resolver itself returns, even after the caller gave up.
type DNSResolverAdmission struct {
	mu      sync.Mutex
	maximum int
	active  int
}

// NewDNSResolverAdmission returns an admission gate (Swift default 8, minimum 1).
func NewDNSResolverAdmission(maximumConcurrentResolutions int) *DNSResolverAdmission {
	return &DNSResolverAdmission{maximum: max(maximumConcurrentResolutions, 1)}
}

// TryAcquire takes a slot without waiting.
func (a *DNSResolverAdmission) TryAcquire() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active >= a.maximum {
		return false
	}
	a.active++
	return true
}

// Release returns a slot.
func (a *DNSResolverAdmission) Release() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active <= 0 {
		panic("repo: DNS admission released without acquire")
	}
	a.active--
}

// ActiveCount reports the number of in-flight resolutions.
func (a *DNSResolverAdmission) ActiveCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active
}
