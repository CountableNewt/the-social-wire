package operations

import (
	"context"
	"database/sql"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Migrate creates or upgrades the operations schema exactly as
// SQLiteOperationsStore.init did, in one write transaction. It is idempotent
// against a Swift-created file and never drops data.
func Migrate(ctx context.Context, db *sqlitedb.DB) error {
	return db.Write(ctx, migrate)
}

func migrate(tx *sql.Tx) error {
	if _, err := tx.Exec(schemaTables); err != nil {
		return err
	}
	if err := migrateCheckpointSnapshotComplete(tx); err != nil {
		return err
	}
	if err := migrateInboxFilteredScope(tx); err != nil {
		return err
	}
	for _, table := range []string{
		"operations_metric_rollups", "operations_trace_spans", "operations_audit_events",
		"appview_ingestion_stream_state", "appview_jetstream_endpoints", "operations_commands",
		"appview_ingestion_gaps", "appview_backfill_jobs", "appview_recovery_failures",
		"operations_alerts",
	} {
		if err := addColumnIfMissing(tx, table, "environment", "TEXT NOT NULL DEFAULT '__legacy_unscoped__'"); err != nil {
			return err
		}
	}
	for _, column := range additiveColumns {
		if err := addColumnIfMissing(tx, column[0], column[1], column[2]); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(lifecycleBackfillSQL); err != nil {
		return err
	}
	if _, err := tx.Exec(schemaIndexes); err != nil {
		return err
	}
	return installChangeEventTriggers(tx)
}

// additiveColumns are the guarded ALTER TABLE steps, in Swift's order.
var additiveColumns = [][3]string{
	{"appview_ingestion_stream_state", "version", "INTEGER NOT NULL DEFAULT 0"},
	{"appview_ingestion_stream_state", "queue_capacity", "INTEGER"},
	{"appview_ingestion_stream_state", "queue_overflow_total", "INTEGER"},
	{"appview_ingestion_stream_state", "queue_observed_at", "TEXT"},
	{"appview_ingestion_stream_state", "transport_heartbeat_at", "TEXT"},
	{"appview_ingestion_stream_state", "last_indexed_mutation_at", "TEXT"},
	{"appview_ingestion_stream_state", "projection_watermark", "TEXT"},
	{"appview_ingestion_stream_state", "validation_watermark", "TEXT"},
	{"appview_jetstream_endpoints", "version", "INTEGER NOT NULL DEFAULT 0"},
	{"operations_commands", "version", "INTEGER NOT NULL DEFAULT 0"},
	{"operations_commands", "expires_at", "TEXT"},
	{"operations_commands", "lease_expires_at", "TEXT"},
	{"appview_ingestion_gaps", "version", "INTEGER NOT NULL DEFAULT 0"},
	{"appview_ingestion_gaps", "expires_at", "TEXT"},
	{"appview_backfill_jobs", "version", "INTEGER NOT NULL DEFAULT 0"},
	{"appview_backfill_jobs", "failure_reason", "TEXT"},
	{"appview_backfill_jobs", "idempotency_key", "TEXT"},
	{"appview_backfill_jobs", "verification_status", "TEXT NOT NULL DEFAULT 'required'"},
	{"appview_backfill_jobs", "verification_reason", "TEXT"},
	{"appview_backfill_jobs", "scope_truncated", "INTEGER NOT NULL DEFAULT 0"},
	{"appview_backfill_jobs", "validation_watermark", "TEXT"},
	{"appview_backfill_jobs", "request_fingerprint", "TEXT"},
	{"appview_backfill_jobs", "request_fingerprint_expires_at", "TEXT"},
	{"appview_backfill_jobs", "normalized_request_hash", "TEXT"},
	{"appview_backfill_jobs", "author_results", "TEXT NOT NULL DEFAULT '[]'"},
	{"appview_backfill_jobs", "expires_at", "TEXT"},
	{"operations_alerts", "version", "INTEGER NOT NULL DEFAULT 0"},
	{"operations_alerts", "condition_key", "TEXT NOT NULL DEFAULT '__legacy__'"},
	{"operations_alerts", "next_delivery_at", "TEXT"},
	{"operations_alerts", "delivery_dead_lettered_at", "TEXT"},
	{"operations_alerts", "expires_at", "TEXT"},
	{"operations_audit_events", "idempotency_key", "TEXT"},
	{"operations_audit_events", "request_id", "TEXT"},
	{"operations_audit_events", "expected_version", "INTEGER"},
	{"operations_audit_events", "before_state", "TEXT NOT NULL DEFAULT '{}'"},
	{"operations_audit_events", "after_state", "TEXT NOT NULL DEFAULT '{}'"},
	{"operations_audit_events", "outcome", "TEXT NOT NULL DEFAULT 'recorded'"},
	{"operations_idempotency_records", "request_fingerprint", "TEXT"},
	{"operations_idempotency_records", "result_payload", "TEXT NOT NULL DEFAULT '{}'"},
}

func addColumnIfMissing(tx *sql.Tx, table, column, definition string) error {
	exists, err := sqlitedb.ColumnExists(tx, table, column)
	if err != nil || exists {
		return err
	}
	_, err = tx.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition)
	return err
}

func tableSQL(tx *sql.Tx, table string) (string, error) {
	var definition sql.NullString
	err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&definition)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return definition.String, err
}

func migrateCheckpointSnapshotComplete(tx *sql.Tx) error {
	definition, err := tableSQL(tx, "appview_jetstream_checkpoints")
	if err != nil {
		return err
	}
	hasBefore, err := sqlitedb.ColumnExists(tx, "appview_jetstream_checkpoints", "replay_before_seq")
	if err != nil {
		return err
	}
	if strings.Contains(definition, "'snapshot_complete'") && hasBefore {
		return nil
	}
	expression := "NULL"
	if hasBefore {
		expression = "replay_before_seq"
	}
	_, err = tx.Exec(strings.Replace(checkpointRebuildSQL, "%s", expression, 1))
	return err
}

func migrateInboxFilteredScope(tx *sql.Tx) error {
	definition, err := tableSQL(tx, "appview_ingestion_inbox")
	if err != nil || strings.Contains(definition, "'filtered_scope'") {
		return err
	}
	hasPolicy, err := sqlitedb.ColumnExists(tx, "appview_ingestion_inbox", "filtered_scope_policy")
	if err != nil {
		return err
	}
	hasAt, err := sqlitedb.ColumnExists(tx, "appview_ingestion_inbox", "filtered_scope_at")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(inboxRebuildCreateSQL); err != nil {
		return err
	}
	copySQL := inboxCopyLegacySQL
	if hasPolicy && hasAt {
		copySQL = inboxCopyWithFilteredSQL
	}
	if _, err := tx.Exec(copySQL); err != nil {
		return err
	}
	_, err = tx.Exec(inboxRebuildFinishSQL)
	return err
}

func installChangeEventTriggers(tx *sql.Tx) error {
	for _, table := range []string{"operations_service_state", "appview_ingestion_stream_state"} {
		for _, operation := range []string{"insert", "update"} {
			if _, err := tx.Exec("DROP TRIGGER IF EXISTS operations_change_" + table + "_" + operation); err != nil {
				return err
			}
		}
	}
	targets := [][3]string{
		{"appview_jetstream_endpoints", "endpoint", "id"},
		{"operations_commands", "command", "id"},
		{"appview_ingestion_gaps", "gap", "id"},
		{"appview_backfill_jobs", "job", "id"},
		{"operations_alerts", "alert", "id"},
	}
	for _, target := range targets {
		table, entity, id := target[0], target[1], target[2]
		for _, operation := range []string{"INSERT", "UPDATE"} {
			lower := strings.ToLower(operation)
			trigger := "operations_change_" + table + "_" + lower
			statement := `
CREATE TRIGGER IF NOT EXISTS ` + trigger + `
AFTER ` + operation + ` ON ` + table + `
FOR EACH ROW WHEN NEW.environment != '__legacy_unscoped__'
BEGIN
  INSERT OR IGNORE INTO operations_change_event_watermarks
    (environment, latest_cursor, earliest_available_cursor, updated_at)
  VALUES (NEW.environment, 0, 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
  UPDATE operations_change_event_watermarks
  SET latest_cursor = latest_cursor + 1,
      updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  WHERE environment = NEW.environment;
  INSERT INTO operations_change_events
    (environment, cursor, event_type, entity_type, entity_id, payload,
     occurred_at, expires_at)
  VALUES (
    NEW.environment,
    (SELECT latest_cursor FROM operations_change_event_watermarks
     WHERE environment = NEW.environment),
    '` + entity + `.` + lower + `',
    '` + entity + `',
    NEW.` + id + `,
    '{}',
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '+30 days'));
END;`
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
	}
	return nil
}
