package oauthclient

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/scheduledata"
)

type durableTestSessions struct{ store *scheduledata.Store }

func (s durableTestSessions) LoadOAuthSession(ctx context.Context, did string) (*Session, error) {
	value, err := s.store.OAuthSession(ctx, did)
	if err != nil || value == nil {
		return nil, err
	}
	return &Session{DID: value.DID, Handle: value.Handle, TokenJSON: value.TokenJSON, DPoPKeyJSON: value.DPoPKeyJSON}, nil
}
func (s durableTestSessions) ClaimOAuthRefresh(ctx context.Context, did, owner, hash string, now, until int64) (bool, error) {
	ok, err := s.store.ClaimOAuthRefresh(ctx, did, owner, hash, now, until)
	if errors.Is(err, scheduledata.ErrRefreshUncertain) {
		return false, ErrNotConfigured
	}
	return ok, err
}
func (s durableTestSessions) BeginOAuthRefresh(ctx context.Context, did, owner, hash string, now int64) error {
	return s.store.BeginOAuthRefresh(ctx, did, owner, hash, now)
}
func (s durableTestSessions) ReleaseOAuthRefresh(ctx context.Context, did, owner string) error {
	return s.store.ReleaseActorLease(ctx, "oauth-refresh", did, owner)
}
func (s durableTestSessions) CompleteOAuthRefresh(ctx context.Context, v Session, previous, owner, hash string, now int64) error {
	return s.store.CompleteOAuthRefresh(ctx, scheduledata.OAuthSession{DID: v.DID, Handle: v.Handle, TokenJSON: v.TokenJSON, DPoPKeyJSON: v.DPoPKeyJSON}, previous, owner, hash, "2026-01-01T00:00:00Z", now)
}

func TestSeparateClientsAndDatabaseHandlesRotateOneRefreshTokenOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scheduler.sqlite")
	a, err := scheduledata.Open(t.Context(), path, "test-key", true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := scheduledata.Open(t.Context(), path, "test-key", true)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	record := `{"accessToken":"old-token","tokenType":"DPoP","refreshToken":"single-use","scope":"atproto","sub":"` + actor + `","pdsEndpoint":"` + pdsOrigin + `","tokenEndpoint":"` + serverOrigin + `/token"}`
	if err := a.CreateOAuthSession(t.Context(), scheduledata.OAuthSession{DID: actor, TokenJSON: record, DPoPKeyJSON: legacyKey}, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	original, err := load(t.Context(), actor, durableTestSessions{a})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	started, release := make(chan struct{}, 1), make(chan struct{})
	transport := testTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return responseWith(200, `{"access_token":"new-token","refresh_token":"rotated","token_type":"DPoP","sub":"`+actor+`"}`, ""), nil
	})
	first, second := New(clientOrigin, &http.Client{Transport: transport}), New(clientOrigin, &http.Client{Transport: transport})
	results := make(chan error, 2)
	go func() { _, err := first.refreshSession(t.Context(), original, durableTestSessions{a}); results <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	go func() { _, err := second.refreshSession(t.Context(), original, durableTestSessions{b}); results <- err }()
	close(release)
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("refresh did not settle")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("token used %d times", calls.Load())
	}
}

func TestUnknownTokenExchangeOutcomeCannotRetryAfterLeaseRelease(t *testing.T) {
	store := &testStore{session: Session{DID: actor, TokenJSON: `{"accessToken":"old","tokenType":"DPoP","refreshToken":"single-use","scope":"atproto","sub":"` + actor + `","pdsEndpoint":"` + pdsOrigin + `","tokenEndpoint":"` + serverOrigin + `/token"}`, DPoPKeyJSON: legacyKey}}
	original, err := load(t.Context(), actor, store)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	transport := testTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("response lost after issuer consumed refresh token")
	})
	for range 2 {
		client := New(clientOrigin, &http.Client{Transport: transport})
		if _, err := client.refreshSession(t.Context(), original, store); err == nil {
			t.Fatal("unknown refresh was accepted")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("uncertain token used %d times", calls.Load())
	}
}
