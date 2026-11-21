package atproto

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestListRecordsCursorStates(t *testing.T) {
	cases := []struct {
		json    map[string]any
		current string
		seen    []string
		empty   bool
		want    CursorResult
	}{
		{map[string]any{}, "", nil, false, CursorResult{Outcome: CursorEnd}},
		{map[string]any{"cursor": "page-2"}, "", nil, false, CursorResult{CursorNext, "page-2"}},
		{map[string]any{"cursor": 2.0}, "", nil, false, CursorResult{CursorInvalid, "cursor_not_string"}},
		{map[string]any{"cursor": "  "}, "", nil, false, CursorResult{CursorInvalid, "cursor_empty"}},
		{map[string]any{"cursor": "page-2"}, "page-2", []string{"page-2"}, false, CursorResult{CursorInvalid, "cursor_cycle"}},
		{map[string]any{"cursor": "page-1"}, "page-2", []string{"page-1", "page-2"}, false, CursorResult{CursorInvalid, "cursor_cycle"}},
		{map[string]any{"cursor": "page-2"}, "", nil, true, CursorResult{CursorInvalid, "empty_page_with_cursor"}},
	}
	for i, c := range cases {
		seen := map[string]struct{}{}
		for _, s := range c.seen {
			seen[s] = struct{}{}
		}
		if got := ParseListRecordsCursor(c.json, c.current, seen, c.empty); got != c.want {
			t.Errorf("case %d: got %+v want %+v", i, got, c.want)
		}
	}
}

// chunkProbe yields chunkCount chunks of chunkSize bytes and records how many
// chunks were started.
type chunkProbe struct {
	chunkSize, chunkCount int
	started, offset       int
	closed                bool
}

func (p *chunkProbe) Read(buf []byte) (int, error) {
	if p.offset == 0 {
		if p.started == p.chunkCount {
			return 0, io.EOF
		}
		p.started++
	}
	n := min(len(buf), p.chunkSize-p.offset)
	for i := range n {
		buf[i] = 'a'
	}
	p.offset = (p.offset + n) % p.chunkSize
	return n, nil
}

func (p *chunkProbe) Close() error { p.closed = true; return nil }

func TestDrainAndCloseIsBounded(t *testing.T) {
	probe := &chunkProbe{chunkSize: 40 * 1024, chunkCount: 3}
	if err := DrainAndClose(context.Background(), probe, 64*1024); err != nil {
		t.Fatal(err)
	}
	if probe.started != 2 || !probe.closed {
		t.Fatalf("started %d chunks, closed %v", probe.started, probe.closed)
	}
}

func TestRateLimiterCancellation(t *testing.T) {
	limiter := NewPDSRequestRateLimiter(1)
	if err := limiter.WaitForPermit(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- limiter.WaitForPermit(ctx) }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not observe cancellation")
	}
}

func TestRateLimiterReservesSlots(t *testing.T) {
	limiter := NewPDSRequestRateLimiter(50)
	start := time.Now()
	for range 3 {
		if err := limiter.WaitForPermit(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 35*time.Millisecond {
		t.Fatalf("three permits at 50/s took only %v", elapsed)
	}
}
