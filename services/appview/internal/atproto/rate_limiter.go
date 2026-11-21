package atproto

import (
	"context"
	"sync"
	"time"
)

// PDSRequestRateLimiter spaces requests evenly at requestsPerSecond.
type PDSRequestRateLimiter struct {
	mu           sync.Mutex
	interval     time.Duration
	nextPermitAt time.Time
	now          func() time.Time
}

// NewPDSRequestRateLimiter returns a limiter for at least one request per second.
func NewPDSRequestRateLimiter(requestsPerSecond int) *PDSRequestRateLimiter {
	return &PDSRequestRateLimiter{
		interval: time.Duration(float64(time.Second) / float64(max(1, requestsPerSecond))),
		now:      time.Now,
	}
}

// WaitForPermit reserves the next slot before sleeping, so concurrent waiters
// never share a timestamp and burst above the rate, then sleeps until it.
func (l *PDSRequestRateLimiter) WaitForPermit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	now := l.now()
	permitAt := l.nextPermitAt
	if permitAt.Before(now) {
		permitAt = now
	}
	l.nextPermitAt = permitAt.Add(l.interval)
	l.mu.Unlock()
	if !permitAt.After(now) {
		return nil
	}
	timer := time.NewTimer(permitAt.Sub(now))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
