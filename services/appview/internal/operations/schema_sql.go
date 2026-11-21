package operations

// schemaTables is SQLiteOperationsStore's Schema.sqlite, verbatim.
const schemaTables = `
CREATE TABLE IF NOT EXISTS operations_service_state (
  service TEXT NOT NULL, environment TEXT NOT NULL, instance_id TEXT NOT NULL,
  liveness TEXT NOT NULL CHECK (liveness IN ('healthy', 'degraded', 'unhealthy', 'unknown')),
  readiness TEXT NOT NULL CHECK (readiness IN ('healthy', 'degraded', 'unhealthy', 'unknown')),
  freshness TEXT NOT NULL CHECK (freshness IN ('healthy', 'degraded', 'unhealthy', 'unknown')),
  completeness TEXT NOT NULL CHECK (completeness IN ('healthy', 'degraded', 'unhealthy', 'unknown')),
  dependency_state TEXT NOT NULL DEFAULT '{}', version TEXT, started_at TEXT NOT NULL, heartbeat_at TEXT NOT NULL,
  PRIMARY KEY (service, environment, instance_id)
);
CREATE TABLE IF NOT EXISTS operations_trace_spans (
  environment TEXT NOT NULL, id TEXT NOT NULL, trace_id TEXT NOT NULL, parent_span_id TEXT, service TEXT NOT NULL,
  name TEXT NOT NULL, started_at TEXT NOT NULL, duration_ms REAL NOT NULL, status TEXT NOT NULL,
  attributes TEXT NOT NULL DEFAULT '{}', expires_at TEXT NOT NULL,
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS operations_metric_rollups (
  environment TEXT NOT NULL, bucket_start TEXT NOT NULL, metric_name TEXT NOT NULL, dimensions_hash TEXT NOT NULL,
  dimensions TEXT NOT NULL DEFAULT '{}', sample_count INTEGER NOT NULL DEFAULT 0,
  value_sum REAL NOT NULL DEFAULT 0, value_min REAL, value_max REAL,
  histogram_buckets TEXT NOT NULL DEFAULT '{}', expires_at TEXT NOT NULL,
  PRIMARY KEY (environment, bucket_start, metric_name, dimensions_hash)
);
CREATE TABLE IF NOT EXISTS operations_events (
  id TEXT NOT NULL, service TEXT NOT NULL, environment TEXT NOT NULL, instance_id TEXT NOT NULL,
  event_name TEXT NOT NULL, occurred_at TEXT NOT NULL, request_id TEXT, trace_id TEXT,
  attributes TEXT NOT NULL DEFAULT '{}', expires_at TEXT NOT NULL,
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS operations_audit_events (
  environment TEXT NOT NULL, id TEXT NOT NULL, operator_did TEXT NOT NULL, action TEXT NOT NULL,
  target_type TEXT NOT NULL, target_id TEXT, idempotency_key TEXT, request_id TEXT,
  expected_version INTEGER, note TEXT,
  before_state TEXT NOT NULL DEFAULT '{}', after_state TEXT NOT NULL DEFAULT '{}',
  outcome TEXT NOT NULL DEFAULT 'recorded', occurred_at TEXT NOT NULL, expires_at TEXT NOT NULL,
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS operations_idempotency_records (
  environment TEXT NOT NULL, idempotency_key TEXT NOT NULL, action TEXT NOT NULL,
  target_type TEXT NOT NULL, target_id TEXT, outcome TEXT NOT NULL,
  request_fingerprint TEXT, result_payload TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL, expires_at TEXT NOT NULL,
  PRIMARY KEY (environment, idempotency_key)
);
CREATE TABLE IF NOT EXISTS operations_change_event_watermarks (
  environment TEXT PRIMARY KEY, latest_cursor INTEGER NOT NULL DEFAULT 0,
  earliest_available_cursor INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS operations_change_events (
  environment TEXT NOT NULL, cursor INTEGER NOT NULL, event_type TEXT NOT NULL,
  entity_type TEXT NOT NULL, entity_id TEXT, payload TEXT NOT NULL DEFAULT '{}',
  occurred_at TEXT NOT NULL, expires_at TEXT NOT NULL,
  PRIMARY KEY (environment, cursor)
);
CREATE TABLE IF NOT EXISTS appview_ingestion_stream_state (
  environment TEXT NOT NULL, source TEXT NOT NULL,
  connection_state TEXT NOT NULL DEFAULT 'unknown'
    CHECK (connection_state IN ('connected', 'disconnected', 'reconnecting', 'unknown')),
  connected_at TEXT,
  last_disconnect_at TEXT, last_disconnect_reason TEXT, last_received_cursor INTEGER,
  last_received_event_at TEXT, last_received_at TEXT, last_committed_cursor INTEGER,
  last_committed_event_at TEXT, last_committed_at TEXT, queue_depth INTEGER NOT NULL DEFAULT 0,
  queue_capacity INTEGER, queue_overflow_total INTEGER, queue_observed_at TEXT,
  transport_heartbeat_at TEXT, last_indexed_mutation_at TEXT,
  projection_watermark TEXT, validation_watermark TEXT,
  heartbeat_at TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK (queue_depth >= 0), CHECK (queue_capacity IS NULL OR queue_capacity > 0),
  CHECK (queue_overflow_total IS NULL OR queue_overflow_total >= 0),
  PRIMARY KEY (environment, source)
);
CREATE TABLE IF NOT EXISTS appview_jetstream_checkpoints (
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
CREATE TABLE IF NOT EXISTS appview_ingestion_inbox (
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
CREATE TABLE IF NOT EXISTS appview_ingestion_incidents (
  environment TEXT NOT NULL, id TEXT NOT NULL, source_generation TEXT, source_host TEXT,
  source TEXT NOT NULL,
  cursor_kind TEXT NOT NULL, start_cursor INTEGER, end_cursor INTEGER, category TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'recovering', 'verification_required', 'resolved', 'ignored')),
  occurrence_count INTEGER NOT NULL DEFAULT 1, first_detected_at TEXT NOT NULL,
  last_detected_at TEXT NOT NULL, last_error TEXT, replay_state TEXT,
  replay_bytes_downloaded INTEGER NOT NULL DEFAULT 0,
  replay_retry_count INTEGER NOT NULL DEFAULT 0,
  replay_range_resume_count INTEGER NOT NULL DEFAULT 0,
  replay_sealed_seq INTEGER, recovered_through_cursor INTEGER,
  verification_evidence TEXT NOT NULL DEFAULT '{}', resolved_at TEXT,
  updated_at TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 0,
  CHECK (start_cursor IS NULL OR start_cursor >= 0),
  CHECK (end_cursor IS NULL OR end_cursor >= 0),
  CHECK (start_cursor IS NULL OR end_cursor IS NULL OR start_cursor <= end_cursor),
  CHECK (occurrence_count > 0), CHECK (version >= 0),
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS appview_ingestion_replay_usage (
  environment TEXT NOT NULL, source_generation TEXT NOT NULL,
  bucket_started_at TEXT NOT NULL, bytes_downloaded INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL, CHECK (bytes_downloaded >= 0),
  PRIMARY KEY (environment, source_generation, bucket_started_at)
);
CREATE TABLE IF NOT EXISTS appview_ingestion_incident_gaps (
  environment TEXT NOT NULL, incident_id TEXT NOT NULL, gap_id TEXT NOT NULL,
  linked_at TEXT NOT NULL,
  PRIMARY KEY (environment, incident_id, gap_id)
);
CREATE TABLE IF NOT EXISTS appview_ingestion_leases (
  environment TEXT NOT NULL, lease_name TEXT NOT NULL, source_generation TEXT NOT NULL,
  owner_id TEXT NOT NULL, fencing_token INTEGER NOT NULL, acquired_at TEXT NOT NULL,
  lease_expires_at TEXT NOT NULL, released_at TEXT, updated_at TEXT NOT NULL,
  CHECK (fencing_token > 0), PRIMARY KEY (environment, lease_name)
);
CREATE TABLE IF NOT EXISTS operations_role_leases (
  environment TEXT NOT NULL, role TEXT NOT NULL, owner_id TEXT NOT NULL,
  fencing_token INTEGER NOT NULL, acquired_at TEXT NOT NULL,
  lease_expires_at TEXT NOT NULL, released_at TEXT, updated_at TEXT NOT NULL,
  CHECK (length(environment) BETWEEN 1 AND 32),
  CHECK (length(role) BETWEEN 1 AND 128),
  CHECK (length(owner_id) BETWEEN 1 AND 255),
  CHECK (fencing_token > 0), CHECK (lease_expires_at > acquired_at),
  PRIMARY KEY (environment, role)
);
CREATE INDEX IF NOT EXISTS operations_role_leases_expiry_idx
  ON operations_role_leases (environment, lease_expires_at, role);
CREATE TABLE IF NOT EXISTS appview_ingestion_reconciliation_requests (
  environment TEXT NOT NULL, id TEXT NOT NULL, source_generation TEXT NOT NULL,
  repo_did TEXT NOT NULL, reason TEXT NOT NULL, trigger_seq INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'leased', 'completed', 'failed')),
  attempt_count INTEGER NOT NULL DEFAULT 0, next_attempt_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  lease_owner TEXT, lease_token TEXT, lease_expires_at TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT,
  CHECK (trigger_seq >= 0), CHECK (attempt_count >= 0),
  PRIMARY KEY (environment, id),
  UNIQUE (environment, source_generation, repo_did, trigger_seq, reason)
);
CREATE TABLE IF NOT EXISTS appview_jetstream_endpoints (
  environment TEXT NOT NULL, id TEXT NOT NULL, display_name TEXT NOT NULL, host TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('active', 'standby')),
  connection_state TEXT NOT NULL DEFAULT 'unknown'
    CHECK (connection_state IN ('connected', 'disconnected', 'reconnecting', 'unknown')),
  last_connected_at TEXT,
  last_disconnected_at TEXT, last_error TEXT, connection_attempts INTEGER NOT NULL DEFAULT 0,
  failover_count INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK (connection_attempts >= 0), CHECK (failover_count >= 0),
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS operations_commands (
  environment TEXT NOT NULL, id TEXT NOT NULL,
  action TEXT NOT NULL CHECK (action = 'reconnect_jetstream'),
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed')),
  requested_by_did TEXT NOT NULL, audit_note TEXT, claimed_by TEXT,
  lease_expires_at TEXT, failure_reason TEXT, created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL, completed_at TEXT,
  expires_at TEXT,
  version INTEGER NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK (status != 'running' OR (claimed_by IS NOT NULL AND lease_expires_at IS NOT NULL)),
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS appview_ingestion_gaps (
  environment TEXT NOT NULL, id TEXT NOT NULL, source TEXT NOT NULL, start_cursor INTEGER, end_cursor INTEGER,
  start_time TEXT, end_time TEXT, reason TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('suspected', 'confirmed', 'backfill_queued',
    'backfilling', 'verification_required', 'resolved', 'ignored')),
  collections TEXT NOT NULL DEFAULT '[]', detected_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  backfill_job_id TEXT, discovered_count INTEGER NOT NULL DEFAULT 0,
  processed_count INTEGER NOT NULL DEFAULT 0, failed_count INTEGER NOT NULL DEFAULT 0,
  reconciled_count INTEGER NOT NULL DEFAULT 0, expires_at TEXT,
  version INTEGER NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK (start_cursor IS NULL OR end_cursor IS NULL OR start_cursor < end_cursor),
  CHECK (discovered_count >= 0 AND processed_count >= 0 AND failed_count >= 0
    AND reconciled_count >= 0 AND failed_count <= processed_count
    AND reconciled_count <= processed_count),
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS appview_backfill_jobs (
  environment TEXT NOT NULL, id TEXT NOT NULL, gap_id TEXT,
  source_mode TEXT NOT NULL CHECK (source_mode IN
    ('tap_verified_resync', 'jetstream_replay', 'pds_reconciliation')),
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'paused', 'completed', 'failed', 'cancelled')),
  start_cursor INTEGER, end_cursor INTEGER, checkpoint_cursor INTEGER,
  collections TEXT NOT NULL DEFAULT '[]', author_dids TEXT NOT NULL DEFAULT '[]',
  author_results TEXT NOT NULL DEFAULT '[]',
  batch_size INTEGER NOT NULL, rate_limit INTEGER NOT NULL, max_concurrency INTEGER NOT NULL,
  estimated_count INTEGER NOT NULL DEFAULT 0, processed_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0, reconciled_count INTEGER NOT NULL DEFAULT 0,
  requested_by_did TEXT NOT NULL, audit_note TEXT, failure_reason TEXT, idempotency_key TEXT,
  verification_status TEXT NOT NULL DEFAULT 'required'
    CHECK (verification_status IN ('pending', 'required', 'verified', 'failed')),
  verification_reason TEXT,
  scope_truncated INTEGER NOT NULL DEFAULT 0, validation_watermark TEXT,
  request_fingerprint TEXT, request_fingerprint_expires_at TEXT, normalized_request_hash TEXT,
  lease_owner TEXT, lease_expires_at TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT, expires_at TEXT,
  version INTEGER NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK (batch_size BETWEEN 1 AND 10000 AND rate_limit BETWEEN 1 AND 5000
    AND max_concurrency BETWEEN 1 AND 16),
  CHECK (source_mode = 'pds_reconciliation' OR max_concurrency = 1),
  CHECK (source_mode != 'jetstream_replay' OR
    (start_cursor IS NOT NULL AND end_cursor IS NOT NULL AND start_cursor < end_cursor)),
  CHECK (start_cursor IS NULL OR end_cursor IS NULL OR start_cursor < end_cursor),
  CHECK (checkpoint_cursor IS NULL OR
    ((start_cursor IS NULL OR checkpoint_cursor >= start_cursor)
      AND (end_cursor IS NULL OR checkpoint_cursor <= end_cursor))),
  CHECK (estimated_count >= 0 AND processed_count >= 0 AND failed_count >= 0
    AND reconciled_count >= 0 AND failed_count <= processed_count
    AND reconciled_count <= processed_count),
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS appview_recovery_failures (
  environment TEXT NOT NULL, id TEXT NOT NULL, job_id TEXT, source TEXT NOT NULL, record_identifier_hash TEXT NOT NULL,
  collection TEXT, operation TEXT, cursor INTEGER, error_type TEXT NOT NULL,
  retry_count INTEGER NOT NULL DEFAULT 0, first_failed_at TEXT NOT NULL, last_failed_at TEXT NOT NULL,
  resolved_at TEXT, expires_at TEXT NOT NULL, CHECK (retry_count >= 0),
  PRIMARY KEY (environment, id)
);
CREATE TABLE IF NOT EXISTS operations_alerts (
  environment TEXT NOT NULL, id TEXT NOT NULL, rule TEXT NOT NULL, condition_key TEXT NOT NULL,
  severity TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('open', 'acknowledged', 'resolved')),
  summary TEXT NOT NULL, evidence TEXT NOT NULL DEFAULT '{}', runbook_slug TEXT NOT NULL,
  opened_at TEXT NOT NULL, updated_at TEXT NOT NULL, acknowledged_by_did TEXT,
  resolved_by_did TEXT, delivery_attempts INTEGER NOT NULL DEFAULT 0, last_delivery_error TEXT,
  next_delivery_at TEXT, delivery_dead_lettered_at TEXT, expires_at TEXT,
  version INTEGER NOT NULL DEFAULT 0 CHECK (version >= 0),
  CHECK (delivery_attempts >= 0), PRIMARY KEY (environment, id)
);
`

// schemaIndexes is SQLiteOperationsStore's Schema.indexes, verbatim.
const schemaIndexes = `
CREATE UNIQUE INDEX IF NOT EXISTS idx_ingestion_stream_environment_source
  ON appview_ingestion_stream_state (environment, source);
CREATE UNIQUE INDEX IF NOT EXISTS idx_jetstream_endpoint_environment_id
  ON appview_jetstream_endpoints (environment, id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_metric_rollup_environment_key
  ON operations_metric_rollups (environment, bucket_start, metric_name, dimensions_hash);
CREATE INDEX IF NOT EXISTS idx_operations_trace_spans_trace
  ON operations_trace_spans (environment, trace_id, started_at);
CREATE INDEX IF NOT EXISTS idx_operations_commands_claim
  ON operations_commands (environment, action, status, created_at);
DROP INDEX IF EXISTS idx_operations_commands_one_active_action;
CREATE UNIQUE INDEX IF NOT EXISTS idx_operations_commands_one_active_action_env
  ON operations_commands (environment, action) WHERE status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS idx_operations_active_gaps
  ON appview_ingestion_gaps (environment, detected_at DESC, id DESC)
  WHERE status NOT IN ('resolved', 'ignored');
CREATE INDEX IF NOT EXISTS idx_appview_ingestion_inbox_claim
  ON appview_ingestion_inbox
    (environment, source_generation, status, next_attempt_at, seq)
  WHERE status IN ('pending', 'leased', 'retry');
CREATE INDEX IF NOT EXISTS idx_appview_ingestion_inbox_repo_fifo
  ON appview_ingestion_inbox (environment, source_generation, repo_did, seq)
  WHERE status IN ('pending', 'leased', 'retry');
CREATE INDEX IF NOT EXISTS idx_appview_ingestion_incidents_active
  ON appview_ingestion_incidents
    (environment, source, source_generation, source_host, cursor_kind, category, last_detected_at DESC)
  WHERE status IN ('open', 'recovering', 'verification_required');
CREATE INDEX IF NOT EXISTS idx_appview_ingestion_incidents_recent
  ON appview_ingestion_incidents (environment, last_detected_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_operations_active_backfills
  ON appview_backfill_jobs (environment, created_at DESC, id DESC)
  WHERE status IN ('queued', 'running', 'paused');
DROP INDEX IF EXISTS idx_operations_audit_idempotency;
CREATE INDEX IF NOT EXISTS idx_operations_idempotency_expiry
  ON operations_idempotency_records (environment, expires_at);
CREATE INDEX IF NOT EXISTS idx_operations_change_events_replay
  ON operations_change_events (environment, cursor);
CREATE INDEX IF NOT EXISTS idx_operations_change_events_expiry
  ON operations_change_events (environment, expires_at, cursor);
DROP INDEX IF EXISTS idx_operations_alert_open_rule;
CREATE UNIQUE INDEX IF NOT EXISTS idx_operations_alert_open_condition
  ON operations_alerts (environment, condition_key) WHERE status != 'resolved';
`
