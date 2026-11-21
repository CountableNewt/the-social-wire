package operations

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMigrateIsIdempotent(t *testing.T) {
	db, _ := openFileDB(t)
	ctx := context.Background()
	for range 3 {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'operations_change_%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Fatalf("triggers = %d", count)
	}
}

func TestMigrateUpgradesExistingInboxSchema(t *testing.T) {
	db, _ := openFileDB(t)
	ctx := context.Background()
	now := "2026-08-16T12:00:00.000Z"
	if _, err := db.Writer().Exec(`
CREATE TABLE appview_ingestion_inbox (
  environment TEXT NOT NULL, source_generation TEXT NOT NULL, seq INTEGER NOT NULL,
  source_host TEXT NOT NULL, cursor_kind TEXT NOT NULL CHECK (cursor_kind = 'jetstream_v2_seq'),
  event_kind TEXT NOT NULL, repo_did TEXT NOT NULL, collection TEXT, operation TEXT,
  repo_rev TEXT, record_key TEXT, record_cid TEXT, payload TEXT NOT NULL,
  event_time TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'leased', 'retry', 'applied', 'dead_letter')),
  attempt_count INTEGER NOT NULL DEFAULT 0, next_attempt_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  lease_owner TEXT, lease_token TEXT, lease_expires_at TEXT,
  failure_category TEXT, failure_reason TEXT, staged_at TEXT NOT NULL,
  updated_at TEXT NOT NULL, applied_at TEXT, dead_lettered_at TEXT,
  reconciled_at TEXT, expires_at TEXT,
  PRIMARY KEY (environment, source_generation, seq)
);
CREATE INDEX idx_appview_ingestion_inbox_claim
  ON appview_ingestion_inbox (environment, source_generation, status, next_attempt_at, seq)
  WHERE status IN ('pending', 'leased', 'retry');
CREATE INDEX idx_appview_ingestion_inbox_repo_fifo
  ON appview_ingestion_inbox (environment, source_generation, repo_did, seq)
  WHERE status IN ('pending', 'leased', 'retry');
INSERT INTO appview_ingestion_inbox
  (environment, source_generation, seq, source_host, cursor_kind, event_kind,
   repo_did, payload, event_time, staged_at, updated_at, expires_at)
VALUES ('dev', 'v2', 1, 'jetstream.example', 'jetstream_v2_seq', 'identity',
        'did:plc:existing', '{}', ?, ?, ?, ?);`, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	definition := schemaText(t, db.Reader(), "appview_ingestion_inbox")
	if !strings.Contains(definition, "'filtered_scope'") {
		t.Fatalf("schema not upgraded: %s", definition)
	}
	indexes := map[string]bool{}
	rows, err := db.Reader().Query(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'appview_ingestion_inbox'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		indexes[name] = true
	}
	rows.Close()
	if !indexes["idx_appview_ingestion_inbox_claim"] || !indexes["idx_appview_ingestion_inbox_repo_fifo"] {
		t.Fatalf("indexes %v", indexes)
	}
	if _, err := db.Writer().Exec(`
INSERT INTO appview_ingestion_inbox
  (environment, source_generation, seq, source_host, cursor_kind, event_kind,
   repo_did, collection, payload, event_time, status, staged_at, updated_at,
   filtered_scope_policy, filtered_scope_at, expires_at)
VALUES ('dev', 'v2', 2, 'jetstream.example', 'jetstream_v2_seq', 'commit',
        'did:plc:filtered', 'app.example.unknown', '{}', ?, 'filtered_scope',
        ?, ?, ?, ?, ?)`, now, now, now, "publication-author-viewer-v1", now, now); err != nil {
		t.Fatal(err)
	}
	var total, filtered int
	if err := db.Reader().QueryRow(`SELECT COUNT(*), SUM(status = 'filtered_scope') FROM appview_ingestion_inbox WHERE source_generation = 'v2'`).Scan(&total, &filtered); err != nil {
		t.Fatal(err)
	}
	if total != 2 || filtered != 1 {
		t.Fatalf("total %d filtered %d", total, filtered)
	}
}

func TestMigrateUpgradesExistingCheckpointSchema(t *testing.T) {
	db, _ := openFileDB(t)
	now := "2026-08-20T12:00:00.000Z"
	if _, err := db.Writer().Exec(`
CREATE TABLE appview_jetstream_checkpoints (
  environment TEXT NOT NULL, source_generation TEXT NOT NULL, source_host TEXT NOT NULL,
  stream_nsid TEXT NOT NULL, filter_fingerprint TEXT NOT NULL,
  cursor_kind TEXT NOT NULL CHECK (cursor_kind = 'jetstream_v2_seq'),
  last_staged_seq INTEGER, last_staged_event_at TEXT, last_staged_at TEXT,
  last_applied_seq INTEGER, last_applied_event_at TEXT, last_applied_at TEXT,
  last_reconciled_repo_rev TEXT, last_reconciled_at TEXT,
  replay_state TEXT NOT NULL DEFAULT 'idle'
    CHECK (replay_state IN ('idle', 'replaying', 'live', 'paused_budget', 'failed')),
  replay_after_seq INTEGER, replay_sealed_seq INTEGER,
  replay_bytes_downloaded INTEGER NOT NULL DEFAULT 0,
  replay_retry_count INTEGER NOT NULL DEFAULT 0,
  replay_range_resume_count INTEGER NOT NULL DEFAULT 0,
  replay_last_progress_at TEXT, replay_etag TEXT, updated_at TEXT NOT NULL,
  PRIMARY KEY (environment, source_generation)
);
INSERT INTO appview_jetstream_checkpoints
  (environment, source_generation, source_host, stream_nsid, filter_fingerprint,
   cursor_kind, replay_state, updated_at)
VALUES ('dev', 'existing-v2', 'jetstream.example',
        'network.bsky.jetstream.subscribeEvents', 'filter-v1', 'jetstream_v2_seq', 'live', ?);`, now); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	definition := schemaText(t, db.Reader(), "appview_jetstream_checkpoints")
	if !strings.Contains(definition, "'snapshot_complete'") || !strings.Contains(definition, "replay_before_seq") {
		t.Fatalf("schema not upgraded: %s", definition)
	}
	if _, err := db.Writer().Exec(`
INSERT INTO appview_jetstream_checkpoints
  (environment, source_generation, source_host, stream_nsid, filter_fingerprint,
   cursor_kind, last_staged_seq, replay_state, replay_after_seq, replay_before_seq,
   replay_sealed_seq, updated_at)
VALUES ('dev', 'bounded-v2', 'jetstream.example',
        'network.bsky.jetstream.subscribeEvents', 'filter-v1',
        'jetstream_v2_seq', 200, 'snapshot_complete', 100, 200, 200, ?)`, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM appview_jetstream_checkpoints`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("count %d err %v", count, err)
	}
}

func schemaText(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var definition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestMetricRollupsRetainBucketValuesAndDimensions(t *testing.T) {
	store := newTestStore(t, "dev")
	ctx := context.Background()
	now := time.Now()
	for _, value := range []float64{1, 3} {
		if err := store.RecordMetric(ctx, MetricSample{
			Name: "socialwire.ingestion.events_total", Value: value,
			Dimensions: map[string]string{"collection": "site.standard.document", "operation": "create"},
			RecordedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	rollups, err := store.ListMetricRollups(ctx, now.Add(-time.Minute), now.Add(time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rollups) != 1 {
		t.Fatalf("rollups %+v", rollups)
	}
	rollup := rollups[0]
	if rollup.MetricName != "socialwire.ingestion.events_total" ||
		rollup.Dimensions["collection"] != "site.standard.document" ||
		rollup.SampleCount != 2 || rollup.ValueSum != 4 || *rollup.ValueMin != 1 || *rollup.ValueMax != 3 {
		t.Fatalf("rollup %+v", rollup)
	}
	if !rollup.BucketStart.Equal(metricBucket(now)) || rollup.BucketStart.Second() != 0 {
		t.Fatalf("bucket %v", rollup.BucketStart)
	}
	var bucket, expires string
	if err := store.db.Reader().QueryRow(`SELECT bucket_start, expires_at FROM operations_metric_rollups`).Scan(&bucket, &expires); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(bucket, ":00.000Z") || len(bucket) != 24 {
		t.Fatalf("bucket format %q", bucket)
	}
}

func TestTelemetryBatchRejectsCrossEnvironmentSignalsAtomically(t *testing.T) {
	store := newTestStore(t, "dev")
	ctx := context.Background()
	now := time.Now()
	err := store.RecordTelemetryBatch(ctx, []Signal{
		MetricSignal(MetricSample{Name: "socialwire.test.total", Value: 1, Dimensions: map[string]string{"environment": "dev"}, RecordedAt: now}),
		EventSignal(Event{Service: "appview", Environment: "prod", InstanceID: "appview-1", Name: "test", OccurredAt: now}),
	})
	if !errors.Is(err, &EnvironmentMismatchError{Expected: "dev", Actual: "prod"}) {
		t.Fatalf("err %v", err)
	}
	rollups, err := store.ListMetricRollups(ctx, now.Add(-time.Minute), now.Add(time.Minute), 10)
	if err != nil || len(rollups) != 0 {
		t.Fatalf("rollups %v err %v", rollups, err)
	}
}

func TestSpansAndEventsPersistRedacted(t *testing.T) {
	store := newTestStore(t, "dev")
	ctx := context.Background()
	now := time.Now()
	if err := store.RecordTelemetryBatch(ctx, []Signal{
		SpanSignal(TraceSpan{Environment: "dev", TraceID: "trace", Service: "appview", Name: "appview.request",
			StartedAt: now, DurationMs: 12.5, Status: "ok",
			Attributes: map[string]string{"route_template": "/v1/x", "authorization": "secret"},
			ExpiresAt:  now.Add(time.Hour)}),
		EventSignal(Event{Service: "appview", Environment: "dev", InstanceID: "i", Name: "started"}),
	}); err != nil {
		t.Fatal(err)
	}
	var id, attributes string
	if err := store.db.Reader().QueryRow(`SELECT id, attributes FROM operations_trace_spans`).Scan(&id, &attributes); err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || attributes != `{"route_template":"\/v1\/x"}` {
		t.Fatalf("id %q attributes %s", id, attributes)
	}
	var events int
	if err := store.db.Reader().QueryRow(`SELECT COUNT(*) FROM operations_events`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events %d err %v", events, err)
	}
}

func TestLeaderLeasesPreserveFencingTokensAcrossReleaseAndTakeover(t *testing.T) {
	store := newTestStore(t, "dev")
	ctx := context.Background()
	now := time.Now()
	first, err := store.AcquireLeaderLease(ctx, "jetstream-v2-intake", "v2-us-west-1", "worker-a", now.Add(30*time.Second), now)
	if err != nil || first == nil {
		t.Fatalf("first %v err %v", first, err)
	}
	blocked, err := store.AcquireLeaderLease(ctx, "jetstream-v2-intake", "v2-us-west-1", "worker-b", now.Add(30*time.Second), now)
	if err != nil || blocked != nil {
		t.Fatalf("blocked %v err %v", blocked, err)
	}
	again, err := store.AcquireLeaderLease(ctx, "jetstream-v2-intake", "v2-us-west-1", "worker-a", now.Add(40*time.Second), now)
	if err != nil || again.FencingToken != first.FencingToken {
		t.Fatalf("reacquire %v err %v", again, err)
	}
	if err := store.ReleaseLeaderLease(ctx, first.Name, first.OwnerID, first.FencingToken, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	second, err := store.AcquireLeaderLease(ctx, "jetstream-v2-intake", "v2-us-west-1", "worker-b", now.Add(40*time.Second), now.Add(2*time.Second))
	if err != nil || second == nil {
		t.Fatalf("second %v err %v", second, err)
	}
	if second.FencingToken != first.FencingToken+1 {
		t.Fatalf("token %d after %d", second.FencingToken, first.FencingToken)
	}
	if _, err := store.RenewLeaderLease(ctx, first.Name, first.OwnerID, first.FencingToken, now.Add(50*time.Second), now.Add(3*time.Second)); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale renew err %v", err)
	}
	renewed, err := store.RenewLeaderLease(ctx, second.Name, second.OwnerID, second.FencingToken, now.Add(60*time.Second), now.Add(3*time.Second))
	if err != nil || !renewed.ExpiresAt.After(second.ExpiresAt) {
		t.Fatalf("renew %v err %v", renewed, err)
	}
}

func TestLeaderLeaseFenceBlocksReleaseAndStaleOwners(t *testing.T) {
	store := newTestStore(t, "dev")
	ctx := context.Background()
	now := time.Now()
	lease, err := store.AcquireLeaderLease(ctx, "pds-read-state-rebuild-slot-0", "pds-read-state-rebuild-v1", "owner", now.Add(120*time.Second), now)
	if err != nil || lease == nil {
		t.Fatal(err)
	}
	ran := false
	err = store.WithLeaderLeaseFence(ctx, lease.Name, "owner", lease.FencingToken, time.Now(), func(ctx context.Context) error {
		ran = true
		if err := store.ReleaseLeaderLease(ctx, lease.Name, "owner", lease.FencingToken, time.Now()); !errors.Is(err, ErrLeaseConflict) {
			t.Errorf("release inside fence: %v", err)
		}
		if other, err := store.AcquireLeaderLease(ctx, lease.Name, "pds-read-state-rebuild-v1", "intruder", time.Now().Add(time.Minute), time.Now()); err != nil || other != nil {
			t.Errorf("takeover inside fence: %v %v", other, err)
		}
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("fence err %v ran %v", err, ran)
	}
	if err := store.WithLeaderLeaseFence(ctx, lease.Name, "stale", lease.FencingToken, time.Now(), func(context.Context) error {
		t.Error("stale owner entered the fence")
		return nil
	}); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale fence err %v", err)
	}
	if err := store.ReleaseLeaderLease(ctx, lease.Name, "owner", lease.FencingToken, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceStateUpsertKeepsStartedAtAndRejectsOtherEnvironments(t *testing.T) {
	store := newTestStore(t, "dev")
	ctx := context.Background()
	now := time.Now()
	state := ServiceState{Service: "appview", Environment: "dev", InstanceID: "a", Liveness: Healthy,
		Readiness: Healthy, Freshness: Unknown, Completeness: Unknown,
		DependencyState: map[string]string{"service_probe": "ready"}, StartedAt: now.Add(-time.Hour), HeartbeatAt: now}
	if err := store.UpsertServiceState(ctx, state); err != nil {
		t.Fatal(err)
	}
	state.StartedAt = now
	state.Liveness = Degraded
	if err := store.UpsertServiceState(ctx, state); err != nil {
		t.Fatal(err)
	}
	states, err := store.ListServiceStates(ctx)
	if err != nil || len(states) != 1 {
		t.Fatalf("states %v err %v", states, err)
	}
	if states[0].Liveness != Degraded || states[0].StartedAt.After(now.Add(-time.Hour+time.Second)) || states[0].Version != nil {
		t.Fatalf("state %+v", states[0])
	}
	state.Environment = "prod"
	if err := store.UpsertServiceState(ctx, state); !errors.Is(err, &EnvironmentMismatchError{Expected: "dev", Actual: "prod"}) {
		t.Fatalf("err %v", err)
	}
}
