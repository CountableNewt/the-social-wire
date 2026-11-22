package scheduledata

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func leaseStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "leases.sqlite")
	first, err := Open(t.Context(), path, "test-key", true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(t.Context(), path, "test-key", true)
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close(); second.Close() })
	return first, second
}
func TestActorLeaseOwnershipExpiryAndGlobalAdmissionAcrossStores(t *testing.T) {
	a, b := leaseStores(t)
	ctx := t.Context()
	if ok, err := a.ClaimActorLease(ctx, "create", "actor", "first", 10, 100, 1); err != nil || !ok {
		t.Fatalf("first claim: %t %v", ok, err)
	}
	if ok, err := b.ClaimActorLease(ctx, "create", "other", "second", 11, 101, 1); err != nil || ok {
		t.Fatalf("global bound: %t %v", ok, err)
	}
	if err := b.ReleaseActorLease(ctx, "create", "actor", "second"); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckActorLease(ctx, "create", "actor", "first", 12); err != nil {
		t.Fatal("foreign release removed owner", err)
	}
	if ok, err := b.ClaimActorLease(ctx, "create", "actor", "second", 100, 200, 1); err != nil || !ok {
		t.Fatalf("expired takeover: %t %v", ok, err)
	}
	if err := a.RenewActorLease(ctx, "create", "actor", "first", 101, 201); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale renewal: %v", err)
	}
	a.ReleaseActorLease(ctx, "create", "actor", "first")
	if err := b.CheckActorLease(ctx, "create", "actor", "second", 102); err != nil {
		t.Fatal(err)
	}
}
func TestRefreshIntentSurvivesExpiryAndFailedPersistence(t *testing.T) {
	a, b := leaseStores(t)
	ctx := t.Context()
	did := "did:plc:account"
	original := OAuthSession{DID: did, TokenJSON: `{"accessToken":"old","refreshToken":"single-use"}`, DPoPKeyJSON: `{"key":"private"}`}
	if err := a.CreateOAuthSession(ctx, original, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if ok, err := a.ClaimOAuthRefresh(ctx, did, "first", "token-hash", 10, 100); err != nil || !ok {
		t.Fatalf("claim: %t %v", ok, err)
	}
	if err := a.BeginOAuthRefresh(ctx, did, "first", "token-hash", 11); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.ClaimOAuthRefresh(ctx, did, "second", "token-hash", 12, 112); err != nil || ok {
		t.Fatalf("live refresh must wait: %t %v", ok, err)
	}
	replacement := original
	replacement.TokenJSON = `{"accessToken":"new","refreshToken":"new-token"}`
	if err := a.CompleteOAuthRefresh(ctx, replacement, original.TokenJSON, "first", "token-hash", "2026-01-01T00:00:01Z", 100); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired persist accepted: %v", err)
	}
	if ok, err := b.ClaimOAuthRefresh(ctx, did, "second", "token-hash", 101, 201); !errors.Is(err, ErrRefreshUncertain) || ok {
		t.Fatalf("consumed token reused: %t %v", ok, err)
	}
	if ok, err := b.ClaimOAuthRefresh(ctx, did, "second", "fresh-token-hash", 101, 201); err != nil || !ok {
		t.Fatalf("new reconnect token blocked: %t %v", ok, err)
	}
	if err := a.BeginOAuthRefresh(ctx, did, "first", "token-hash", 102); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale begin: %v", err)
	}
}
func TestRefreshCompletionCASPreservesReconnectAndConsumedTokenFence(t *testing.T) {
	a, b := leaseStores(t)
	ctx := context.Background()
	did := "did:plc:account"
	old := OAuthSession{DID: did, TokenJSON: `{"accessToken":"same","refreshToken":"old"}`, DPoPKeyJSON: "old-key"}
	a.CreateOAuthSession(ctx, old, "2026-01-01T00:00:00Z")
	a.ClaimOAuthRefresh(ctx, did, "owner", "old-hash", 10, 100)
	a.BeginOAuthRefresh(ctx, did, "owner", "old-hash", 11)
	reconnect := old
	reconnect.TokenJSON = `{"accessToken":"same","refreshToken":"reconnected"}`
	reconnect.DPoPKeyJSON = "reconnected-key"
	if err := b.CreateOAuthSession(ctx, reconnect, "2026-01-01T00:00:01Z"); err != nil {
		t.Fatal(err)
	}
	replacement := old
	replacement.TokenJSON = `{"accessToken":"new","refreshToken":"rotated"}`
	if err := a.CompleteOAuthRefresh(ctx, replacement, old.TokenJSON, "owner", "old-hash", "2026-01-01T00:00:02Z", 12); err != nil {
		t.Fatal(err)
	}
	got, err := b.OAuthSession(ctx, did)
	if err != nil || got.TokenJSON != reconnect.TokenJSON || got.DPoPKeyJSON != reconnect.DPoPKeyJSON {
		t.Fatalf("reconnect overwritten: %+v %v", got, err)
	}
	if ok, err := b.ClaimOAuthRefresh(ctx, did, "next", "old-hash", 13, 113); !errors.Is(err, ErrRefreshUncertain) || ok {
		t.Fatalf("completed token reused: %t %v", ok, err)
	}
}

func TestStaleCreatorCannotPersistSubmissionOrJob(t *testing.T) {
	a, b := leaseStores(t)
	ctx := t.Context()
	did := "did:plc:creator"
	now := time.Now().UnixMicro()
	if ok, err := a.ClaimActorLease(ctx, "create", did, "stale", now-20, now-10, 32); err != nil || !ok {
		t.Fatalf("historical owner: %t %v", ok, err)
	}
	if ok, err := b.ClaimActorLease(ctx, "create", did, "current", now, now+1_000_000, 32); err != nil || !ok {
		t.Fatalf("takeover: %t %v", ok, err)
	}
	if err := a.RememberScheduleSubmissionOwned(ctx, did, "record", "stale", "2026-01-01T00:00:00Z"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale submission: %v", err)
	}
	if err := a.InsertScheduleJobIfMissingOwned(ctx, ScheduledJob{DID: did, RKey: "record"}, "stale", "2026-01-01T00:00:00Z"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale job: %v", err)
	}
}
