package ingest

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func historyTestDB(t *testing.T, path string) *sqlitedb.DB {
	t.Helper()
	var db *sqlitedb.DB
	var err error
	if path == "" {
		db, err = sqlitedb.OpenMemory()
	} else {
		db, err = sqlitedb.Open(path)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := MigrateAuthorHistoryQueue(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAuthorHistoryCheckpointSurvivesRestartAndDeduplicatesDemand(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "history.sqlite")
	db := historyTestDB(t, path)
	q := historyQueue{db}
	now := time.Now()
	const author = "did:plc:author"
	for range 2 {
		if err := q.enqueue(ctx, author, now); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.Reader().QueryRow("SELECT count(*) FROM author_history_jobs").Scan(&count); err != nil || count != 1 {
		t.Fatalf("dedupe: %d %v", count, err)
	}
	claim, err := q.claim(ctx, now)
	if err != nil || claim == nil {
		t.Fatalf("claim: %v %v", claim, err)
	}
	if err := q.checkpoint(ctx, claim, 0, "next-page", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.finish(ctx, *claim, false, false, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := historyQueue{historyTestDB(t, path)}
	resumed, err := restarted.claim(ctx, now.Add(3*time.Second))
	if err != nil || resumed == nil || resumed.cursor != "next-page" || resumed.collection != 0 {
		t.Fatalf("resume: %#v %v", resumed, err)
	}
	if err := restarted.checkpoint(ctx, resumed, 1, "", now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.finish(ctx, *resumed, false, false, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	next, err := restarted.claim(ctx, now.Add(6*time.Second))
	if err != nil || next == nil || next.collection != 1 || next.cursor != "" {
		t.Fatalf("collection checkpoint: %#v %v", next, err)
	}
}

func TestAuthorHistoryClaimsAreExclusiveAndStaleOwnersCannotAdvance(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "shared.sqlite")
	a := historyQueue{historyTestDB(t, path)}
	b := historyQueue{historyTestDB(t, path)}
	now := time.Now()
	a.enqueue(ctx, "did:plc:author", now)
	first, err := a.claim(ctx, now)
	if err != nil || first == nil {
		t.Fatalf("first: %v", err)
	}
	if other, err := b.claim(ctx, now.Add(time.Second)); err != nil || other != nil {
		t.Fatalf("overlapping claim: %#v %v", other, err)
	}
	second, err := b.claim(ctx, now.Add(historyLease))
	if err != nil || second == nil || second.owner == first.owner {
		t.Fatalf("recovery: %#v %v", second, err)
	}
	if err := a.checkpoint(ctx, first, 0, "stale", now.Add(historyLease+time.Second)); !errors.Is(err, ErrAuthorHistoryLeaseLost) {
		t.Fatalf("stale checkpoint: %v", err)
	}
	if _, err := a.finish(ctx, *first, true, false, now.Add(historyLease+time.Second)); !errors.Is(err, ErrAuthorHistoryLeaseLost) {
		t.Fatalf("stale completion: %v", err)
	}
	if err := b.checkpoint(ctx, second, 0, "owned", now.Add(historyLease+time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorHistoryYieldsAtPageBoundAndResumesInsteadOfRescanningHead(t *testing.T) {
	ctx := t.Context()
	q := historyQueue{historyTestDB(t, "")}
	now := time.Now()
	q.enqueue(ctx, "did:plc:author", now)
	claim, err := q.claim(ctx, now)
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	calls := []string{}
	load := func(_ context.Context, collection, cursor string) ([]authorRecord, string, error) {
		calls = append(calls, collection+":"+cursor)
		if collection == documentCollections[1] {
			return []authorRecord{}, "", nil
		}
		n := 0
		if cursor != "" {
			n, _ = strconv.Atoi(cursor)
		}
		if n == 27 {
			return []authorRecord{}, "", nil
		}
		return make([]authorRecord, 10), strconv.Itoa(n + 1), nil
	}
	index := func(_ context.Context, _ string, records []authorRecord) (int, error) { return len(records), nil }
	pages, indexed, done, err := walkHistoryClaim(ctx, q, claim, load, index)
	if err != nil || done || pages != 25 || indexed != 250 || claim.cursor != "25" {
		t.Fatalf("bounded walk: pages=%d indexed=%d done=%t cursor=%s err=%v", pages, indexed, done, claim.cursor, err)
	}
	q.finish(ctx, *claim, false, false, time.Now())
	resumed, err := q.claim(ctx, time.Now())
	if err != nil || resumed == nil {
		t.Fatal(err)
	}
	calls = nil
	_, _, done, err = walkHistoryClaim(ctx, q, resumed, load, index)
	if err != nil || !done || len(calls) == 0 || calls[0] != documentCollections[0]+":25" {
		t.Fatalf("resume restarted head: %v done=%t err=%v", calls, done, err)
	}
}

func TestAuthorHistoryCursorCycleFailsWithoutSkippingOrLosingCheckpoint(t *testing.T) {
	ctx := t.Context()
	q := historyQueue{historyTestDB(t, "")}
	now := time.Now()
	q.enqueue(ctx, "did:plc:author", now)
	claim, _ := q.claim(ctx, now)
	if err := q.checkpoint(ctx, claim, 0, "a", now); err != nil {
		t.Fatal(err)
	}
	if err := q.checkpoint(ctx, claim, 0, "b", now); err != nil {
		t.Fatal(err)
	}
	if err := q.checkpoint(ctx, claim, 0, "a", now); !errors.Is(err, errHistoryCursorLoop) {
		t.Fatalf("cycle: %v", err)
	}
	var cursor string
	q.db.Reader().QueryRow("SELECT cursor FROM author_history_jobs").Scan(&cursor)
	if cursor != "b" {
		t.Fatalf("failed checkpoint advanced: %q", cursor)
	}
}

func TestAuthorHistoryBackoffAndCompletedCooldownPersist(t *testing.T) {
	ctx := t.Context()
	q := historyQueue{historyTestDB(t, "")}
	now := time.Now()
	author := "did:plc:author"
	q.enqueue(ctx, author, now)
	claim, _ := q.claim(ctx, now)
	retry, err := q.finish(ctx, *claim, false, true, now)
	if err != nil || !retry {
		t.Fatalf("failure: %t %v", retry, err)
	}
	if claim, err := q.claim(ctx, now.Add(59*time.Second)); err != nil || claim != nil {
		t.Fatalf("retry too soon: %v %v", claim, err)
	}
	resumed, err := q.claim(ctx, now.Add(time.Minute))
	if err != nil || resumed == nil {
		t.Fatal(err)
	}
	if _, err := q.finish(ctx, *resumed, true, false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	q.enqueue(ctx, author, now.Add(2*time.Minute))
	if claim, err := q.claim(ctx, now.Add(2*time.Minute)); err != nil || claim != nil {
		t.Fatalf("cooldown lost: %v %v", claim, err)
	}
	q.enqueue(ctx, author, now.Add(7*time.Hour))
	if claim, err := q.claim(ctx, now.Add(7*time.Hour)); err != nil || claim == nil || claim.collection != 0 || claim.cursor != "" {
		t.Fatalf("new refresh cycle: %#v %v", claim, err)
	}
}

func TestAuthorHistoryFailedPageDoesNotAdvanceCursor(t *testing.T) {
	ctx := t.Context()
	q := historyQueue{historyTestDB(t, "")}
	now := time.Now()
	if err := q.enqueue(ctx, "did:plc:author", now); err != nil {
		t.Fatal(err)
	}
	claim, err := q.claim(ctx, now)
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	failure := errors.New("partial indexing failed")
	load := func(context.Context, string, string) ([]authorRecord, string, error) {
		return make([]authorRecord, 10), "after-page", nil
	}
	index := func(context.Context, string, []authorRecord) (int, error) { return 3, failure }
	pages, indexed, done, err := walkHistoryClaim(ctx, q, claim, load, index)
	if !errors.Is(err, failure) || done || pages != 0 || indexed != 3 {
		t.Fatalf("failed page: %d %d %t %v", pages, indexed, done, err)
	}
	var cursor string
	if err := q.db.Reader().QueryRow("SELECT cursor FROM author_history_jobs").Scan(&cursor); err != nil || cursor != "" {
		t.Fatalf("partial page skipped: %q %v", cursor, err)
	}
}
