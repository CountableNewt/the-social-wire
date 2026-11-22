package jobs

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func runnerDatabase(t *testing.T, path string) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestIndependentRunnersCannotOverlapAndCanRecoverExpiredLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.sqlite")
	first := runnerDatabase(t, path)
	second := runnerDatabase(t, path)
	a, _ := New(first, "", nil)
	b, _ := New(second, "", nil)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	if claimed, err := a.claim(ctx, "discovery", "old", now, now.Add(time.Minute)); err != nil || !claimed {
		t.Fatalf("first claim %t %v", claimed, err)
	}
	if claimed, err := b.claim(ctx, "discovery", "other", now, now.Add(time.Minute)); err != nil || claimed {
		t.Fatalf("overlapping claim %t %v", claimed, err)
	}
	if claimed, err := b.claim(ctx, "discovery", "new", now.Add(time.Minute), now.Add(2*time.Minute)); err != nil || !claimed {
		t.Fatalf("expired claim %t %v", claimed, err)
	}
	if err := a.release(ctx, "discovery", "old"); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err := second.Reader().QueryRow("SELECT owner FROM http_job_leases WHERE name='discovery'").Scan(&owner); err != nil || owner != "new" {
		t.Fatalf("stale release removed successor: %q %v", owner, err)
	}
	if err := b.release(ctx, "discovery", "new"); err != nil {
		t.Fatal(err)
	}
}

func TestRunBoundsWorkAndReleasesItsLease(t *testing.T) {
	db := runnerDatabase(t, filepath.Join(t.TempDir(), "jobs.sqlite"))
	runner, err := New(db, "", map[string]Action{"discovery": {Timeout: time.Millisecond, Run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), "discovery")
	if err == nil || !result.Claimed || result.Completed {
		t.Fatalf("unbounded result %+v %v", result, err)
	}
	var count int
	if err := db.Reader().QueryRow("SELECT COUNT(*) FROM http_job_leases").Scan(&count); err != nil || count != 0 {
		t.Fatalf("lease leaked: %d %v", count, err)
	}
}

func TestJobConfigurationFailsClosed(t *testing.T) {
	for _, value := range []string{"external", "internal", ""} {
		if _, err := ParseMode(value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ParseMode("serverless"); err == nil {
		t.Fatal("unknown runner mode accepted")
	}
	for _, secret := range []string{"short", strings.Repeat("a", 31), strings.Repeat("a", 32) + "\n", strings.Repeat("a", 513)} {
		if ValidSecret(secret) {
			t.Fatal("invalid secret accepted")
		}
	}
	if !ValidSecret(strings.Repeat("a", 32)) {
		t.Fatal("valid secret rejected")
	}
}
