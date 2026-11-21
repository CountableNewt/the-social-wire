// Package analytics stores privacy-preserving Social Wire observations and public creator metrics.
package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"golang.org/x/sync/singleflight"
)

type window struct {
	until time.Time
	count int
}

type routes struct {
	store     *Store
	provider  Provider
	mu        sync.Mutex
	limits    map[string]window
	validated map[string]time.Time
	flights   singleflight.Group
	active    chan struct{}
}

// Register registers creator analytics on the authenticated route group.
func Register(group *httpx.Group, store *Store, repo *atproto.RepoClient) {
	handler := &routes{
		store: store, provider: Provider{repo: repo}, limits: make(map[string]window),
		validated: make(map[string]time.Time), active: make(chan struct{}, 1000),
	}
	group.Get("/v1/analytics/bluesky", handler.handle)
	group.Get("/v1/analytics/publications", handler.handle)
	group.Get("/v1/analytics/summary", handler.handle)
	group.Post("/v1/analytics/refresh", handler.handle)
	group.Post("/v1/analytics/events", handler.handle)
}

func (a *routes) allow(key string, at time.Time, limit int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry := a.limits[key]
	if !at.Before(entry.until) {
		if len(a.limits) >= 10_000 {
			for key, window := range a.limits {
				if !at.Before(window.until) {
					delete(a.limits, key)
				}
			}
			if len(a.limits) >= 10_000 {
				return false
			}
		}
		entry = window{until: at.Add(time.Minute)}
	}
	if entry.count >= limit {
		return false
	}
	entry.count++
	a.limits[key] = entry
	return true
}

func (a *routes) validate(ctx context.Context, uri string) error {
	a.mu.Lock()
	cached := time.Now().Before(a.validated[uri])
	a.mu.Unlock()
	if cached {
		return nil
	}
	result := a.flights.DoChan(uri, func() (any, error) {
		select {
		case a.active <- struct{}{}:
			defer func() { <-a.active }()
		default:
			return nil, errors.New("content verification overloaded")
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if err := a.provider.Validate(ctx, uri); err != nil {
			return nil, err
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		now := time.Now()
		if len(a.validated) >= 10_000 {
			for key, until := range a.validated {
				if !now.Before(until) {
					delete(a.validated, key)
				}
			}
		}
		if len(a.validated) < 10_000 {
			a.validated[uri] = now.Add(5 * time.Minute)
		}
		return nil, nil
	})
	select {
	case response := <-result:
		return response.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *routes) handle(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	author := reqctx.ViewerDID(ctx)
	if author == "" {
		return fail(w, http.StatusUnauthorized, "Authentication Required")
	}
	at := time.Now()
	path := r.URL.Path
	maximum := 120
	switch path {
	case "/v1/analytics/refresh":
		maximum = 1
	case "/v1/analytics/bluesky", "/v1/analytics/publications":
		maximum = 10
	}
	if !a.allow(author+"\x00"+path, at, maximum) {
		return fail(w, http.StatusTooManyRequests, "Analytics Request Limit Reached")
	}
	if path == "/v1/analytics/events" {
		return a.observe(ctx, w, r, author, at)
	}
	if path == "/v1/analytics/publications" {
		summary, err := a.provider.publications(ctx, author, at)
		if err != nil {
			return fail(w, http.StatusBadGateway, "Publication Analytics Provider Unavailable")
		}
		return httpx.JSON(w, http.StatusOK, summary)
	}
	query := r.URL.Query()
	days := 30
	if query.Has("days") {
		days, _ = strconv.Atoi(query.Get("days"))
	}
	if path == "/v1/analytics/bluesky" {
		if !slices.Contains([]int{7, 30, 90}, days) {
			return fail(w, http.StatusBadRequest, "Invalid Analytics Range")
		}
		articles, err := a.store.Articles(ctx, author)
		if err != nil {
			return fail(w, http.StatusServiceUnavailable, "Analytics Unavailable")
		}
		return httpx.JSON(w, http.StatusOK, a.provider.bluesky(ctx, author, days, articles, at))
	}
	limit := 50
	if query.Has("limit") {
		limit, _ = strconv.Atoi(query.Get("limit"))
	}
	if days < 1 || days > 90 || limit < 1 || limit > 100 {
		return fail(w, http.StatusBadRequest, "Invalid Analytics Range")
	}
	cursor := query.Get("cursor")
	if cursor != "" {
		owner, _, _, err := Subject(cursor)
		if err != nil || owner != author {
			return fail(w, http.StatusBadRequest, "Invalid Analytics Cursor")
		}
	}
	if path == "/v1/analytics/refresh" {
		items, complete, err := a.provider.Inventory(ctx, author, at)
		if err != nil {
			return fail(w, http.StatusBadGateway, "Analytics Provider Unavailable")
		}
		if err := a.store.Save(ctx, author, items, complete, at); err != nil {
			return fail(w, http.StatusServiceUnavailable, "Analytics Unavailable")
		}
	}
	summary, err := a.store.Summary(ctx, author, days, limit, cursor, at)
	if err != nil {
		return fail(w, http.StatusServiceUnavailable, "Analytics Unavailable")
	}
	return httpx.JSON(w, http.StatusOK, summary)
}

func (a *routes) observe(ctx context.Context, w http.ResponseWriter, r *http.Request, viewer string, at time.Time) error {
	controller := http.NewResponseController(w)
	if deadline, ok := ctx.Deadline(); ok {
		_ = controller.SetReadDeadline(deadline)
		defer controller.SetReadDeadline(time.Time{})
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	var fields map[string]json.RawMessage
	var event Event
	if err != nil || json.Unmarshal(data, &fields) != nil || len(fields) != 3 ||
		fields["subjectUri"] == nil || fields["event"] == nil || fields["surface"] == nil ||
		json.Unmarshal(data, &event) != nil ||
		!slices.Contains([]string{"impression", "view", "outbound_click"}, event.Event) ||
		!slices.Contains([]string{"social-feed", "article-feed", "post-viewer", "article-reader", "messages"}, event.Surface) {
		return fail(w, http.StatusBadRequest, "Invalid Analytics Observation")
	}
	author, _, _, err := Subject(event.SubjectURI)
	if err != nil {
		return fail(w, http.StatusBadRequest, "Invalid Subject")
	}
	if author != viewer {
		if err := a.validate(ctx, event.SubjectURI); err != nil {
			return fail(w, http.StatusUnprocessableEntity, "Canonical Content Unavailable")
		}
		if err := a.store.Record(ctx, viewer, author, event, at); err != nil {
			if errors.Is(err, ErrRateLimit) {
				return fail(w, http.StatusTooManyRequests, "Analytics Observation Limit Reached")
			}
			return fail(w, http.StatusServiceUnavailable, "Analytics Unavailable")
		}
	}
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func fail(w http.ResponseWriter, status int, message string) error {
	return httpx.JSON(w, status, map[string]string{"error": message})
}
