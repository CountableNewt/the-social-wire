package scheduledata

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
)

func TestExistingDatabaseMigrationPreservesOAuthAndLegacyQueue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "scheduling.sqlite")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE oauth_sessions(did TEXT PRIMARY KEY,handle TEXT,token_json TEXT NOT NULL,dpop_key_json TEXT NOT NULL,updated_at TEXT NOT NULL);
		INSERT INTO oauth_sessions VALUES('did:plc:account',NULL,'{"accessToken":"old"}','{"key":"secret"}','2026-01-01T00:00:00Z');
		CREATE TABLE oauth_states(state TEXT PRIMARY KEY,handle TEXT NOT NULL,pkce_verifier TEXT NOT NULL,nonce TEXT NOT NULL,expires_at TEXT NOT NULL);
		INSERT INTO oauth_states VALUES('old-state','test.example','verifier','nonce','2099-01-01T00:00:00Z');
		CREATE TABLE scheduled_jobs(did TEXT NOT NULL,rkey TEXT NOT NULL,scheduled_for TEXT NOT NULL,status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,locked_until TEXT,last_error TEXT,published_uri TEXT,published_cid TEXT,
			updated_at TEXT NOT NULL,PRIMARY KEY(did,rkey));
		INSERT INTO scheduled_jobs VALUES('did:plc:account','old-job','2026-01-01T00:00:00Z','scheduled',0,NULL,NULL,NULL,NULL,'2026-01-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path, "vault key", true)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.OAuthSession(ctx, "did:plc:account")
	if err != nil || session == nil || session.TokenJSON != `{"accessToken":"old"}` || session.DPoPKeyJSON != `{"key":"secret"}` {
		t.Fatalf("legacy session not preserved: %v", err)
	}
	account, err := store.ViewerAccount(ctx, session.DID, session.DID)
	if err != nil || account == nil || account.AccessKind != "owner" || !account.IsDefault {
		t.Fatalf("owner access not backfilled: %+v %v", account, err)
	}
	job, err := store.ScheduleJob(ctx, session.DID, "old-job")
	if err != nil || job == nil || job.PublishRKey != "old-job" || job.RecordType != "app.bsky.feed.post" {
		t.Fatalf("legacy job not preserved: %+v %v", job, err)
	}
	var token, key string
	if err := store.db.Reader().QueryRowContext(ctx, "SELECT token_json,dpop_key_json FROM oauth_sessions WHERE did=?", session.DID).Scan(&token, &key); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, sealedPrefix) || !strings.HasPrefix(key, sealedPrefix) {
		t.Fatal("legacy auth remained plaintext")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(ctx, path, "wrong key", true); err == nil {
		bad.Close()
		t.Fatal("wrong encryption key accepted")
	}
	store, err = Open(ctx, path, "vault key", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := store.ConsumeOAuthState(ctx, "old-state", "2026-01-01T00:00:00Z")
	if err != nil || state == nil || state.PKCEVerifier != "verifier" || state.Purpose != "sign_in" {
		t.Fatalf("legacy state not preserved: %v", err)
	}
	state, err = store.ConsumeOAuthState(ctx, "old-state", "2026-01-01T00:00:00Z")
	if err != nil || state != nil {
		t.Fatalf("state was replayable: %v", err)
	}
}

func TestOAuthSessionRefreshIgnoresSupersededToken(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:", "test key", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := "2026-01-01T00:00:00Z"
	old := OAuthSession{DID: "did:plc:account", TokenJSON: `{"accessToken":"old"}`, DPoPKeyJSON: "key"}
	if err := store.CreateOAuthSession(ctx, old, now); err != nil {
		t.Fatal(err)
	}
	replacement := old
	replacement.TokenJSON = `{"accessToken":"new"}`
	if ok, err := store.ClaimOAuthRefresh(ctx, old.DID, "owner", "token-hash", 1, 10); err != nil || !ok {
		t.Fatalf("claim: %t %v", ok, err)
	}
	if err := store.BeginOAuthRefresh(ctx, old.DID, "owner", "token-hash", 2); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteOAuthRefresh(ctx, replacement, old.TokenJSON, "owner", "token-hash", now, 3); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteOAuthRefresh(ctx, old, old.TokenJSON, "owner", "token-hash", now, 4); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale refresh overwrite: %v", err)
	}

	latest, err := store.OAuthSession(ctx, old.DID)
	if err != nil || latest == nil || latest.TokenJSON != replacement.TokenJSON {
		t.Fatalf("superseded token overwrote session: %v", err)
	}
}

func TestDueClaimsAndEditsShareOneTransactionalQueue(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	stamp := scheduling.ScheduleTimestamp(now)
	job := ScheduledJob{DID: "did:plc:account", RKey: "waiting", ScheduledAt: scheduling.ScheduleTimestamp(now.Add(-time.Minute)), Status: scheduling.StatusScheduled}
	if err := store.UpsertScheduleJob(ctx, job, stamp); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimScheduleEdit(ctx, job.DID, job.RKey, "lease", stamp, scheduling.ScheduleTimestamp(now.Add(time.Minute)))
	if err != nil || !claimed {
		t.Fatalf("edit lease: %v %v", claimed, err)
	}
	jobs, err := store.ClaimDueJobs(ctx, stamp, 1)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("worker claimed a record being edited: %v %+v", err, jobs)
	}
	if err := store.ReleaseScheduleEdit(ctx, job.DID, job.RKey, "lease"); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	claims := make(chan int, 2)
	for range 2 {
		group.Go(func() {
			claimed, err := store.ClaimDueJobs(ctx, stamp, 1)
			if err != nil {
				t.Error(err)
			}
			claims <- len(claimed)
		})
	}
	group.Wait()
	close(claims)
	var count int
	for n := range claims {
		count += n
	}
	if count != 1 {
		t.Fatalf("claimed %d times", count)
	}
	durable, err := store.ScheduleJob(ctx, job.DID, job.RKey)
	if err != nil || durable == nil || durable.Status != scheduling.StatusPublishing || durable.Attempts != 1 {
		t.Fatalf("durable claim: %+v %v", durable, err)
	}
	claimed, err = store.ClaimScheduleEdit(ctx, job.DID, job.RKey, "later", stamp, scheduling.ScheduleTimestamp(now.Add(time.Minute)))
	if err != nil || claimed {
		t.Fatalf("edit acquired an in-flight publish: %v %v", claimed, err)
	}
}
