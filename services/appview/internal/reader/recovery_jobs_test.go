package reader

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestServerlessReadStateRecoveryPersistsDemandWithoutDetachedNetworkWork(t *testing.T) {
	db := testReaderDB(t)
	getter := &countingRecordGetter{}
	s := &service{db: db, durableRecovery: true, generationFetcher: readstate.NewLiveRecordFetcher(getter)}
	cid := "current"
	status := readstate.Status{ManifestCID: &cid, ProjectionReady: false}
	for range 2 {
		s.reconcileForRequest(context.Background(), testViewer, status)
	}
	if getter.calls.Load() != 0 {
		t.Fatal("request detached a PDS fetch")
	}
	var count int
	if err := db.Reader().QueryRow("SELECT count(*) FROM pds_recovery_jobs WHERE viewer_did=? AND status='pending'", testViewer).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable request dedupe: %d %v", count, err)
	}
}

func TestRecoveryClaimsSurviveRestartAndFenceOldOwner(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reader.sqlite")
	first, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := thinstore.Migrate(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, first); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := enqueueRecovery(ctx, first, testViewer, now); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	old, err := claimRecovery(ctx, a, now)
	if err != nil || old == nil {
		t.Fatalf("restarted claim: %v %v", old, err)
	}
	if duplicate, err := claimRecovery(ctx, b, now.Add(time.Second)); err != nil || duplicate != nil {
		t.Fatalf("overlapping claim: %v %v", duplicate, err)
	}
	next, err := claimRecovery(ctx, b, now.Add(46*time.Second))
	if err != nil || next == nil || next.owner == old.owner {
		t.Fatalf("expired recovery: %v %v", next, err)
	}
	if err := finishRecovery(ctx, a, old, nil, now.Add(47*time.Second)); err == nil {
		t.Fatal("stale owner finished replacement work")
	}
	if err := finishRecovery(ctx, b, next, nil, now.Add(47*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := enqueueRecovery(ctx, a, testViewer, now.Add(48*time.Second)); err != nil {
		t.Fatal(err)
	}
	if claim, err := claimRecovery(ctx, b, now.Add(48*time.Second)); err != nil || claim != nil {
		t.Fatalf("ignored success cooldown: %v %v", claim, err)
	}
	if err := enqueueRecovery(ctx, a, testViewer, now.Add(108*time.Second)); err != nil {
		t.Fatal(err)
	}
	if claim, err := claimRecovery(ctx, b, now.Add(108*time.Second)); err != nil || claim == nil {
		t.Fatalf("request did not requeue after cooldown: %v %v", claim, err)
	}
}

func TestRecoveryFailuresBackOffAndRemainDurable(t *testing.T) {
	db := testReaderDB(t)
	ctx := context.Background()
	now := time.Now()
	if err := enqueueRecovery(ctx, db, testViewer, now); err != nil {
		t.Fatal(err)
	}
	claim, err := claimRecovery(ctx, db, now)
	if err != nil || claim == nil {
		t.Fatal(err)
	}
	if err := finishRecovery(ctx, db, claim, errors.New("offline"), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if early, err := claimRecovery(ctx, db, now.Add(2*time.Second)); err != nil || early != nil {
		t.Fatalf("retry backoff: %v %v", early, err)
	}
	if retry, err := claimRecovery(ctx, db, now.Add(32*time.Second)); err != nil || retry == nil {
		t.Fatalf("durable retry: %v %v", retry, err)
	}
}

func TestRecoveryCooldownCannotBypassActiveCapacity(t *testing.T) {
	db := testReaderDB(t)
	ctx := context.Background()
	now := time.Now()
	if _, err := db.Writer().ExecContext(ctx, `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<1000)
 INSERT INTO pds_recovery_jobs(viewer_did,status,available_at_us) SELECT 'did:plc:queued'||i,'pending',0 FROM n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().ExecContext(ctx, "INSERT INTO pds_recovery_jobs(viewer_did,status,available_at_us) VALUES(?,'cooldown',0)", testViewer); err != nil {
		t.Fatal(err)
	}
	if err := enqueueRecovery(ctx, db, testViewer, now); err == nil {
		t.Fatal("expired cooldown bypassed active queue capacity")
	}
	var status string
	if err := db.Reader().QueryRowContext(ctx, "SELECT status FROM pds_recovery_jobs WHERE viewer_did=?", testViewer).Scan(&status); err != nil || status != "cooldown" {
		t.Fatalf("cooldown changed at capacity: %s %v", status, err)
	}
}
