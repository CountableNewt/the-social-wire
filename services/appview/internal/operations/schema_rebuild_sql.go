package operations

// The rebuild migrations below are copied from SQLiteOperationsStore's
// migrateJetstreamCheckpointSnapshotCompleteIfNeeded and
// migrateIngestionInboxFilteredScopeIfNeeded.

// checkpointRebuildSQL recreates appview_jetstream_checkpoints with the
// snapshot_complete state; %s is the replay_before_seq source expression.
const checkpointRebuildSQL = `
ALTER TABLE appview_jetstream_checkpoints
  RENAME TO appview_jetstream_checkpoints_before_snapshot_complete;
CREATE TABLE appview_jetstream_checkpoints (
  environment TEXT NOT NULL, source_generation TEXT NOT NULL, source_host TEXT NOT NULL,
  stream_nsid TEXT NOT NULL, filter_fingerprint TEXT NOT NULL,
  cursor_kind TEXT NOT NULL CHECK (cursor_kind = 'jetstream_v2_seq'),
  last_staged_seq INTEGER, last_staged_event_at TEXT, last_staged_at TEXT,
  last_applied_seq INTEGER, last_applied_event_at TEXT, last_applied_at TEXT,
  last_reconciled_repo_rev TEXT, last_reconciled_at TEXT,
  replay_state TEXT NOT NULL DEFAULT 'idle'
    CHECK (replay_state IN (
      'idle', 'replaying', 'live', 'paused_budget', 'failed', 'snapshot_complete'
    )),
  replay_after_seq INTEGER, replay_before_seq INTEGER, replay_sealed_seq INTEGER,
  replay_bytes_downloaded INTEGER NOT NULL DEFAULT 0,
  replay_retry_count INTEGER NOT NULL DEFAULT 0,
  replay_range_resume_count INTEGER NOT NULL DEFAULT 0,
  replay_last_progress_at TEXT, replay_etag TEXT, updated_at TEXT NOT NULL,
  CHECK (last_staged_seq IS NULL OR last_staged_seq >= 0),
  CHECK (last_applied_seq IS NULL OR last_applied_seq >= 0),
  CHECK (last_applied_seq IS NULL OR last_staged_seq IS NULL OR last_applied_seq <= last_staged_seq),
  CHECK (replay_before_seq IS NULL OR replay_before_seq >= 0),
  CHECK (replay_before_seq IS NULL OR (
    replay_after_seq IS NOT NULL AND replay_after_seq < replay_before_seq
  )),
  CHECK (replay_state != 'snapshot_complete' OR (
    replay_after_seq IS NOT NULL AND replay_before_seq IS NOT NULL
    AND replay_after_seq < replay_before_seq
    AND replay_sealed_seq = replay_before_seq
    AND (last_staged_seq IS NULL OR last_staged_seq <= replay_before_seq)
  )),
  PRIMARY KEY (environment, source_generation)
);
INSERT INTO appview_jetstream_checkpoints
  (environment, source_generation, source_host, stream_nsid, filter_fingerprint,
   cursor_kind, last_staged_seq, last_staged_event_at, last_staged_at,
   last_applied_seq, last_applied_event_at, last_applied_at,
   last_reconciled_repo_rev, last_reconciled_at, replay_state, replay_after_seq,
   replay_before_seq, replay_sealed_seq, replay_bytes_downloaded, replay_retry_count,
   replay_range_resume_count, replay_last_progress_at, replay_etag, updated_at)
SELECT environment, source_generation, source_host, stream_nsid, filter_fingerprint,
       cursor_kind, last_staged_seq, last_staged_event_at, last_staged_at,
       last_applied_seq, last_applied_event_at, last_applied_at,
       last_reconciled_repo_rev, last_reconciled_at, replay_state, replay_after_seq,
       %s, replay_sealed_seq, replay_bytes_downloaded,
       replay_retry_count, replay_range_resume_count, replay_last_progress_at,
       replay_etag, updated_at
FROM appview_jetstream_checkpoints_before_snapshot_complete;
DROP TABLE appview_jetstream_checkpoints_before_snapshot_complete;
`

// inboxRebuildCreateSQL recreates appview_ingestion_inbox with filtered_scope.
const inboxRebuildCreateSQL = `
ALTER TABLE appview_ingestion_inbox RENAME TO appview_ingestion_inbox_before_filtered_scope;
CREATE TABLE appview_ingestion_inbox (
  environment TEXT NOT NULL, source_generation TEXT NOT NULL, seq INTEGER NOT NULL,
  source_host TEXT NOT NULL, cursor_kind TEXT NOT NULL CHECK (cursor_kind = 'jetstream_v2_seq'),
  event_kind TEXT NOT NULL, repo_did TEXT NOT NULL, collection TEXT, operation TEXT,
  repo_rev TEXT, record_key TEXT, record_cid TEXT, payload TEXT NOT NULL,
  event_time TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'leased', 'retry', 'applied', 'dead_letter', 'filtered_scope')),
  attempt_count INTEGER NOT NULL DEFAULT 0, next_attempt_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  lease_owner TEXT, lease_token TEXT, lease_expires_at TEXT,
  failure_category TEXT, failure_reason TEXT, staged_at TEXT NOT NULL,
  updated_at TEXT NOT NULL, applied_at TEXT, dead_lettered_at TEXT,
  reconciled_at TEXT, filtered_scope_policy TEXT, filtered_scope_at TEXT, expires_at TEXT,
  CHECK (seq >= 0), CHECK (attempt_count >= 0),
  CHECK ((status = 'leased' AND lease_owner IS NOT NULL AND lease_token IS NOT NULL
    AND lease_expires_at IS NOT NULL) OR status != 'leased'),
  CHECK (status != 'applied' OR applied_at IS NOT NULL),
  CHECK (status != 'dead_letter' OR dead_lettered_at IS NOT NULL),
  CHECK ((status = 'filtered_scope' AND filtered_scope_policy IS NOT NULL
    AND length(filtered_scope_policy) BETWEEN 1 AND 128
    AND filtered_scope_at IS NOT NULL AND applied_at IS NULL AND reconciled_at IS NULL)
    OR (status != 'filtered_scope' AND filtered_scope_policy IS NULL AND filtered_scope_at IS NULL)),
  PRIMARY KEY (environment, source_generation, seq)
);
`

// inboxCopyWithFilteredSQL copies rows that already carry filtered-scope evidence.
const inboxCopyWithFilteredSQL = `
INSERT INTO appview_ingestion_inbox
  (environment, source_generation, seq, source_host, cursor_kind, event_kind,
   repo_did, collection, operation, repo_rev, record_key, record_cid, payload,
   event_time, status, attempt_count, next_attempt_at, lease_owner, lease_token,
   lease_expires_at, failure_category, failure_reason, staged_at, updated_at,
   applied_at, dead_lettered_at, reconciled_at, filtered_scope_policy,
   filtered_scope_at, expires_at)
SELECT environment, source_generation, seq, source_host, cursor_kind, event_kind,
       repo_did, collection, operation, repo_rev, record_key, record_cid, payload,
       event_time, status, attempt_count, next_attempt_at, lease_owner, lease_token,
       lease_expires_at, failure_category, failure_reason, staged_at, updated_at,
       applied_at, dead_lettered_at, reconciled_at, filtered_scope_policy,
       filtered_scope_at, expires_at
FROM appview_ingestion_inbox_before_filtered_scope;
`

// inboxCopyLegacySQL copies rows from an inbox without filtered-scope columns.
const inboxCopyLegacySQL = `
INSERT INTO appview_ingestion_inbox
  (environment, source_generation, seq, source_host, cursor_kind, event_kind,
   repo_did, collection, operation, repo_rev, record_key, record_cid, payload,
   event_time, status, attempt_count, next_attempt_at, lease_owner, lease_token,
   lease_expires_at, failure_category, failure_reason, staged_at, updated_at,
   applied_at, dead_lettered_at, reconciled_at, expires_at)
SELECT environment, source_generation, seq, source_host, cursor_kind, event_kind,
       repo_did, collection, operation, repo_rev, record_key, record_cid, payload,
       event_time, status, attempt_count, next_attempt_at, lease_owner, lease_token,
       lease_expires_at, failure_category, failure_reason, staged_at, updated_at,
       applied_at, dead_lettered_at, reconciled_at, expires_at
FROM appview_ingestion_inbox_before_filtered_scope;
`

// inboxRebuildFinishSQL drops the old inbox and restores its indexes.
const inboxRebuildFinishSQL = `
DROP TABLE appview_ingestion_inbox_before_filtered_scope;
CREATE INDEX IF NOT EXISTS idx_appview_ingestion_inbox_claim
  ON appview_ingestion_inbox
    (environment, source_generation, status, next_attempt_at, seq)
  WHERE status IN ('pending', 'leased', 'retry');
CREATE INDEX IF NOT EXISTS idx_appview_ingestion_inbox_repo_fifo
  ON appview_ingestion_inbox (environment, source_generation, repo_did, seq)
  WHERE status IN ('pending', 'leased', 'retry');
`

// lifecycleBackfillSQL is the unconditional retention normalization migrate runs.
const lifecycleBackfillSQL = `
UPDATE operations_commands
SET status = 'queued', claimed_by = NULL, lease_expires_at = NULL,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), version = version + 1
WHERE status = 'running' AND (claimed_by IS NULL OR lease_expires_at IS NULL);
UPDATE operations_commands
SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', COALESCE(completed_at, updated_at), '+365 days')
WHERE status IN ('completed', 'failed');
UPDATE appview_ingestion_gaps
SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', updated_at, '+365 days')
WHERE status IN ('resolved', 'ignored');
UPDATE appview_backfill_jobs
SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', COALESCE(completed_at, updated_at), '+365 days')
WHERE status IN ('completed', 'failed', 'cancelled');
UPDATE operations_alerts
SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', updated_at, '+365 days')
WHERE status = 'resolved';
UPDATE operations_audit_events
SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', occurred_at, '+365 days');
`
