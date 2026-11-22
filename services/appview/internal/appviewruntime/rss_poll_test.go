package appviewruntime

import (
	"context"
	"errors"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	"testing"
	"time"
)

func TestRSSPollLeavesUnstartedFeedsForNextRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	calls := 0
	err := pollRSSFeeds(ctx, []string{"first", "second"}, func(context.Context, string) (int, error) { calls++; return 0, nil })
	if err != nil || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
func TestRSSPollPreservesPartialErrorsAndCancellation(t *testing.T) {
	source := &thinstore.RSSSourceFailure{Category: "rss_fetch"}
	database := errors.New("database unavailable")
	var calls []string
	err := pollRSSFeeds(t.Context(), []string{"source", "database", "healthy"}, func(_ context.Context, feed string) (int, error) {
		calls = append(calls, feed)
		switch feed {
		case "source":
			return 0, source
		case "database":
			return 0, database
		}
		return 1, nil
	})
	if len(calls) != 3 || !errors.Is(err, source) || !errors.Is(err, database) {
		t.Fatalf("failure=%v calls=%v", err, calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	calls = nil
	err = pollRSSFeeds(ctx, []string{"first", "second"}, func(context.Context, string) (int, error) {
		calls = append(calls, "first")
		cancel()
		return 0, database
	})
	if len(calls) != 1 || !errors.Is(err, database) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v calls=%v", err, calls)
	}
}

// Model elapsed work without sleeping or changing the request deadline itself.
type rssAdmissionContext struct {
	context.Context
	checks int
}

func (c *rssAdmissionContext) Deadline() (time.Time, bool) {
	c.checks++
	if c.checks == 1 {
		return time.Now().Add(time.Minute), true
	}
	return time.Now().Add(time.Second), true
}
func TestRSSPollStopsAdmissionAfterCompletedFeed(t *testing.T) {
	ctx := &rssAdmissionContext{Context: t.Context()}
	var calls []string
	err := pollRSSFeeds(ctx, []string{"first", "deferred"}, func(_ context.Context, feed string) (int, error) { calls = append(calls, feed); return 1, nil })
	if err != nil || len(calls) != 1 || calls[0] != "first" {
		t.Fatalf("calls=%v err=%v", calls, err)
	}
}
