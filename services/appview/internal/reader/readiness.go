package reader

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

const (
	readStateStatusPath      = "/xrpc/app.thesocialwire.appview.getReadStateStatus"
	readStateRefreshInterval = 60 * time.Second
	readStateRetryInterval   = 30 * time.Second
	readStateBackoffLimit    = 1000
)

// reconcileState coalesces background PDS manifest checks per viewer.
type reconcileState struct {
	mu           sync.Mutex
	running      map[string]struct{}
	retryAfter   map[string]time.Time
	refreshAfter map[string]time.Time
}

// readsProjection reports whether a route serves the viewer's read-state
// projection. Export stays available while the projection rebuilds.
func readsProjection(path string) bool {
	switch path {
	case "/xrpc/app.thesocialwire.appview.exportReadState":
		return false
	case "/v1/publications/sidebar", "/v1/publications/refresh",
		"/xrpc/app.thesocialwire.publication.getSidebar", "/xrpc/app.thesocialwire.publication.refreshSidebar":
		return true
	}
	return strings.HasPrefix(path, "/v1/appview/") || strings.HasPrefix(path, "/xrpc/app.thesocialwire.appview.")
}

// requireReadState runs after authentication and before cached bootstrap/feed
// responses begin. Status still answers while the projection is not ready.
func (s *service) requireReadState(next httpx.Handler) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		auth := reqctx.AuthFrom(r.Context())
		if auth == nil || !readsProjection(r.URL.Path) {
			return next(w, r)
		}
		status, err := pdsStatus(r.Context(), s.db.Reader(), auth.DID)
		if err == nil {
			s.reconcileForRequest(r.Context(), auth.DID, status)
		}
		if (err == nil && status.ProjectionReady) || r.URL.Path == readStateStatusPath {
			return next(w, r)
		}
		return feedError(w, r, http.StatusServiceUnavailable, "ReadStateNotReady", "Read state is syncing. Please retry shortly.", true)
	}
}

// reconcile verifies a PDS-authoritative viewer's current manifest in the
// background: promptly while the projection is not ready, and at most once a
// minute while it is, so PDS writes whose confirmation never arrived converge.
func (s *service) reconcile(viewer string, status readstate.Status) {
	if status.ManifestCID == nil || s.generationFetcher == nil || !s.claimReconcile(viewer, status.ProjectionReady, time.Now()) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		record, err := s.generationFetcher.Fetch(ctx, viewer, readstate.ManifestCollection, "self", nil)
		if err == nil && record.CID == "" {
			err = readstate.ErrIncompleteGeneration
		}
		if err == nil && (!status.ProjectionReady || record.CID != *status.ManifestCID) {
			_, err = s.confirmGeneration(ctx, viewer, record.CID, nil)
		}
		s.finishReconcile(viewer, err)
	}()
}

func (s *service) claimReconcile(viewer string, ready bool, now time.Time) bool {
	state := &s.reconciles
	state.mu.Lock()
	defer state.mu.Unlock()
	if _, running := state.running[viewer]; running {
		return false
	}
	if now.Before(state.retryAfter[viewer]) || (ready && now.Before(state.refreshAfter[viewer])) {
		return false
	}
	select {
	case s.recoverySlots <- struct{}{}:
	default:
		return false
	}
	if state.running == nil {
		state.running = map[string]struct{}{}
	}
	state.running[viewer] = struct{}{}
	return true
}

func (s *service) finishReconcile(viewer string, err error) {
	state := &s.reconciles
	state.mu.Lock()
	defer state.mu.Unlock()
	delete(state.running, viewer)
	<-s.recoverySlots
	now := time.Now()
	if err != nil {
		state.retryAfter = withBackoff(state.retryAfter, viewer, now, now.Add(readStateRetryInterval))
		return
	}
	delete(state.retryAfter, viewer)
	state.refreshAfter = withBackoff(state.refreshAfter, viewer, now, now.Add(readStateRefreshInterval))
}

// withBackoff records a viewer's next attempt and keeps the map bounded.
func withBackoff(backoff map[string]time.Time, viewer string, now, next time.Time) map[string]time.Time {
	if backoff == nil {
		backoff = map[string]time.Time{}
	}
	if len(backoff) >= readStateBackoffLimit {
		for key, at := range backoff {
			if !at.After(now) {
				delete(backoff, key)
			}
		}
		for key := range backoff {
			if len(backoff) < readStateBackoffLimit {
				break
			}
			delete(backoff, key)
		}
	}
	backoff[viewer] = next
	return backoff
}

// Serverless requests only enqueue durable work; no detached network request is
// needed to repair a public PDS generation after this response has completed.
func (s *service) reconcileForRequest(ctx context.Context, viewer string, status readstate.Status) {
	if !s.durableRecovery {
		s.reconcile(viewer, status)
		return
	}
	if status.ManifestCID == nil || s.generationFetcher == nil {
		return
	}
	if err := enqueueRecovery(ctx, s.db, viewer, time.Now()); err != nil {
		slog.Default().Warn("Read-state recovery enqueue deferred")
	}
}
