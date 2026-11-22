package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func testDiscoveryQueue(t *testing.T) (*discoveryQueue, *sqlitedb.DB) {
	t.Helper()
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := MigrateDiscoveryQueue(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	return &discoveryQueue{db}, db
}

func testDiscoveryScope(key string) DiscoveryScope {
	return normalizeDiscoveryScope(DiscoveryScope{Author: "did:plc:author", Keys: []string{"https://example.test/" + key}})
}

func TestDiscoveryQueuePersistsAcrossRestartAndDeduplicatesNormalizedScopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.sqlite")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateDiscoveryQueue(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	queue := &discoveryQueue{db}
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	scope := testDiscoveryScope("publication")
	if err := queue.enqueue(t.Context(), scope, now); err != nil {
		t.Fatal(err)
	}
	if err := queue.enqueue(t.Context(), scope, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Reader().QueryRow("SELECT count(*) FROM public_discovery_jobs").Scan(&count); err != nil || count != 1 {
		t.Fatalf("dedupe: %d %v", count, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	queue = &discoveryQueue{db}
	if err := MigrateDiscoveryQueue(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	claim, err := queue.claim(t.Context(), now.Add(2*time.Second))
	if err != nil || claim == nil || discoveryID(claim.scope) != discoveryID(scope) {
		t.Fatalf("restart lost request: %+v %v", claim, err)
	}
	if retried, err := queue.finish(t.Context(), *claim, false, now.Add(3*time.Second)); err != nil || retried {
		t.Fatalf("ack: %t %v", retried, err)
	}
	if claim, err := queue.claim(t.Context(), now.Add(4*time.Second)); err != nil || claim != nil {
		t.Fatalf("acknowledged request remains: %+v %v", claim, err)
	}
}

func TestDiscoveryClaimsFenceExpiredWorkersAndRecoverCrashes(t *testing.T) {
	queue, _ := testDiscoveryQueue(t)
	now := time.Now()
	if err := queue.enqueue(t.Context(), testDiscoveryScope("one"), now); err != nil {
		t.Fatal(err)
	}
	first, err := queue.claim(t.Context(), now)
	if err != nil || first == nil {
		t.Fatalf("first claim: %+v %v", first, err)
	}
	if other, err := queue.claim(t.Context(), now.Add(time.Second)); err != nil || other != nil {
		t.Fatalf("live lease double claim: %+v %v", other, err)
	}
	expired := now.Add(discoveryLease)
	if _, err := queue.finish(t.Context(), *first, false, expired); !errors.Is(err, ErrDiscoveryLeaseLost) {
		t.Fatalf("expired ack accepted: %v", err)
	}
	second, err := queue.claim(t.Context(), expired)
	if err != nil || second == nil || second.owner == first.owner || second.attempts != 2 {
		t.Fatalf("crash recovery: %+v %v", second, err)
	}
	if _, err := queue.finish(t.Context(), *first, true, expired.Add(time.Second)); !errors.Is(err, ErrDiscoveryLeaseLost) {
		t.Fatalf("stale retry replaced successor: %v", err)
	}
	if _, err := queue.finish(t.Context(), *second, false, expired.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryRetryBackoffAndAttemptLimit(t *testing.T) {
	queue, db := testDiscoveryQueue(t)
	now := time.Now()
	scope := testDiscoveryScope("failed")
	if err := queue.enqueue(t.Context(), scope, now); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= discoveryMaxAttempts; attempt++ {
		claim, err := queue.claim(t.Context(), now)
		if err != nil || claim == nil || claim.attempts != attempt {
			t.Fatalf("attempt %d: %+v %v", attempt, claim, err)
		}
		retried, err := queue.finish(t.Context(), *claim, true, now)
		if err != nil || retried != (attempt < discoveryMaxAttempts) {
			t.Fatalf("finish attempt %d: %t %v", attempt, retried, err)
		}
		if immediate, err := queue.claim(t.Context(), now); err != nil || immediate != nil {
			t.Fatalf("backoff skipped: %+v %v", immediate, err)
		}
		now = now.Add(time.Second * time.Duration(1<<attempt))
	}
	var status string
	if err := db.Reader().QueryRow("SELECT status FROM public_discovery_jobs").Scan(&status); err != nil || status != "failed" {
		t.Fatalf("retry not terminal: %q %v", status, err)
	}
	if err := queue.enqueue(t.Context(), scope, now); err != nil {
		t.Fatal(err)
	}
	if claim, err := queue.claim(t.Context(), now); err != nil || claim != nil {
		t.Fatalf("failed cooldown bypassed: %+v %v", claim, err)
	}
	now = now.Add(time.Hour)
	if err := queue.enqueue(t.Context(), scope, now); err != nil {
		t.Fatal(err)
	}
	claim, err := queue.claim(t.Context(), now)
	if err != nil || claim == nil || claim.attempts != 1 {
		t.Fatalf("explicit later refresh failed: %+v %v", claim, err)
	}
}

func TestDiscoveryQueueBoundsPendingWorkWithoutDroppingDuplicates(t *testing.T) {
	queue, _ := testDiscoveryQueue(t)
	now := time.Now()
	for i := range discoveryQueueLimit {
		if err := queue.enqueue(t.Context(), testDiscoveryScope(fmt.Sprint(i)), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.enqueue(t.Context(), testDiscoveryScope("0"), now); err != nil {
		t.Fatalf("full queue rejected duplicate: %v", err)
	}
	if err := queue.enqueue(t.Context(), testDiscoveryScope("overflow"), now); !errors.Is(err, ErrDiscoveryQueueFull) {
		t.Fatalf("queue accepted overflow: %v", err)
	}
}

func TestDiscoveryClaimIsExclusiveAcrossDatabaseHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite")
	firstDB, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer firstDB.Close()
	secondDB, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer secondDB.Close()
	if err := MigrateDiscoveryQueue(t.Context(), firstDB); err != nil {
		t.Fatal(err)
	}
	first, second := &discoveryQueue{firstDB}, &discoveryQueue{secondDB}
	now := time.Now()
	if err := first.enqueue(t.Context(), testDiscoveryScope("single"), now); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	claims := make(chan *discoveryClaim, 2)
	failures := make(chan error, 2)
	for _, queue := range []*discoveryQueue{first, second} {
		wg.Go(func() { claim, err := queue.claim(t.Context(), now); claims <- claim; failures <- err })
	}
	wg.Wait()
	close(claims)
	close(failures)
	count := 0
	for claim := range claims {
		if claim != nil {
			count++
		}
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("one job received %d claims", count)
	}
}

func TestDiscoveryRunOnceConsumesDurableWorkAndPropagatesFailure(t *testing.T) {
	_, db := testDiscoveryQueue(t)
	if err := thinstore.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	store := thinstore.SQLiteIngestStore{DB: db}
	provider := providerFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"records":[]}`))}, nil
	})
	service := NewMicrocosm(provider, store, db)
	if err := service.Schedule(t.Context(), testDiscoveryScope("empty")); err != nil {
		t.Fatal(err)
	}
	result, err := service.RunOnce(t.Context())
	if err != nil || !result.Claimed || result.Indexed != 0 || result.Retried {
		t.Fatalf("successful run: %+v %v", result, err)
	}
	service.client = providerFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New("provider unavailable") })
	if err := service.Schedule(t.Context(), testDiscoveryScope("retry")); err != nil {
		t.Fatal(err)
	}
	result, err = service.RunOnce(t.Context())
	if err == nil || !result.Claimed || !result.Retried {
		t.Fatalf("failed run: %+v %v", result, err)
	}
}

func TestDiscoveryScheduleRejectsMissingDurableStoreAndIgnoresInvalidScope(t *testing.T) {
	service := NewMicrocosm(nil, nil, nil)
	if err := service.Schedule(context.Background(), DiscoveryScope{}); err != nil {
		t.Fatal(err)
	}
	if err := service.Schedule(context.Background(), testDiscoveryScope("valid")); err == nil {
		t.Fatal("missing durable store silently dropped scope")
	}
}

func TestDiscoveryMalformedPersistedScopeDoesNotPoisonQueue(t *testing.T) {
	queue, db := testDiscoveryQueue(t)
	now := time.Now()
	bad := testDiscoveryScope("bad")
	if err := queue.enqueue(t.Context(), bad, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec("UPDATE public_discovery_jobs SET scope_json='not-json' WHERE id=?", queueID(bad)); err != nil {
		t.Fatal(err)
	}
	if err := queue.enqueue(t.Context(), testDiscoveryScope("next"), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if claim, err := queue.claim(t.Context(), now); err == nil || claim != nil {
		t.Fatalf("malformed input was not quarantined: %+v %v", claim, err)
	}
	claim, err := queue.claim(t.Context(), now.Add(time.Second))
	if err != nil || claim == nil || claim.id == queueID(bad) {
		t.Fatalf("bad row blocked next scope: %+v %v", claim, err)
	}
}

func TestDiscoveryCrashOnFinalAttemptBecomesTerminal(t *testing.T) {
	queue, db := testDiscoveryQueue(t)
	now := time.Now()
	if err := queue.enqueue(t.Context(), testDiscoveryScope("crash"), now); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= discoveryMaxAttempts; attempt++ {
		claim, err := queue.claim(t.Context(), now)
		if err != nil || claim == nil || claim.attempts != attempt {
			t.Fatalf("claim %d: %+v %v", attempt, claim, err)
		}
		now = now.Add(discoveryLease)
	}
	if claim, err := queue.claim(t.Context(), now); err != nil || claim != nil {
		t.Fatalf("exhausted crashed job reclaimed: %+v %v", claim, err)
	}
	var status string
	if err := db.Reader().QueryRow("SELECT status FROM public_discovery_jobs").Scan(&status); err != nil || status != "failed" {
		t.Fatalf("crashed final attempt not terminal: %q %v", status, err)
	}
}
