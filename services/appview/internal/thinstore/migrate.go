package thinstore

import (
	"context"
	"database/sql"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Migrate creates or upgrades the thin AppView store schema in the order
// SQLiteThinAppViewStore's initializer used: finance, sports, then the core
// tables. Repository-recovery and PDS read-state migrations (readstate module)
// must run after this, matching the Swift initializer. Every statement is
// idempotent against an existing Swift-created file.
func Migrate(ctx context.Context, db *sqlitedb.DB) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if err := migrateFinance(tx); err != nil {
			return err
		}
		if err := migrateSports(tx); err != nil {
			return err
		}
		return migrateCore(tx)
	})
}

func migrateFinance(tx *sql.Tx) error {
	return sqlitedb.ExecAll(tx, `
      CREATE TABLE IF NOT EXISTS finance_selection_sync (viewer_did TEXT PRIMARY KEY, synced_at DOUBLE NOT NULL);
      CREATE TABLE IF NOT EXISTS finance_selections (
        viewer_did TEXT NOT NULL, record_key TEXT NOT NULL, kind TEXT NOT NULL,
        reference TEXT NOT NULL, updated_at DOUBLE NOT NULL, PRIMARY KEY(viewer_did, record_key));
      CREATE TABLE IF NOT EXISTS finance_selection_versions (
        viewer_did TEXT NOT NULL, record_key TEXT NOT NULL, event_at DOUBLE NOT NULL,
        repo_rev TEXT NOT NULL, is_deleted BOOLEAN NOT NULL, PRIMARY KEY(viewer_did, record_key));
      `)
}

func migrateSports(tx *sql.Tx) error {
	return sqlitedb.ExecAll(tx, `
      CREATE TABLE IF NOT EXISTS sports_selection_sync (viewer_did TEXT PRIMARY KEY, synced_at DOUBLE NOT NULL);
      CREATE TABLE IF NOT EXISTS sports_selections (
        viewer_did TEXT NOT NULL, record_key TEXT NOT NULL, action TEXT NOT NULL,
        reference TEXT NOT NULL, updated_at DOUBLE NOT NULL, PRIMARY KEY(viewer_did, record_key));
      CREATE TABLE IF NOT EXISTS sports_selection_versions (
        viewer_did TEXT NOT NULL, record_key TEXT NOT NULL, event_at DOUBLE NOT NULL,
        repo_rev TEXT NOT NULL, is_deleted BOOLEAN NOT NULL, PRIMARY KEY(viewer_did, record_key));
      `)
}

func migrateCore(tx *sql.Tx) error {
	if err := sqlitedb.ExecAll(tx,
		`
      CREATE TABLE IF NOT EXISTS content_items (
        uri TEXT PRIMARY KEY,
        cid TEXT NOT NULL,
        author_did TEXT NOT NULL,
        collection TEXT NOT NULL,
        created_at TEXT NOT NULL,
        indexed_at TEXT NOT NULL,
        publication_site TEXT,
        render_json TEXT NOT NULL,
        expires_at TEXT NOT NULL
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_author_collection_created
        ON content_items (author_did, collection, created_at DESC);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_expires
        ON content_items (expires_at);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_author_expires
        ON content_items (author_did, expires_at);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_author_site_expires
        ON content_items (author_did, publication_site, expires_at);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_author_created_uri
        ON content_items (author_did, created_at DESC, uri DESC);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_author_site_created_uri
        ON content_items (author_did, publication_site, created_at DESC, uri DESC);
      `,
		// Persist only built-in SQLite expressions: sibling services migrate this
		// shared database through connections without our custom collation.
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_author_timestamp_uri
        ON content_items (author_did, `+FeedTimestampSQL("created_at")+` DESC, uri DESC);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_author_site_timestamp_uri
        ON content_items (author_did, publication_site, `+FeedTimestampSQL("created_at")+` DESC, uri DESC);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_content_items_timestamp_uri
        ON content_items (`+FeedTimestampSQL("created_at")+` DESC, uri DESC);
      `,
		`
      CREATE TABLE IF NOT EXISTS read_marks (
        viewer_did TEXT NOT NULL,
        subject_uri TEXT NOT NULL,
        created_at TEXT NOT NULL,
        PRIMARY KEY (viewer_did, subject_uri)
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_read_marks_viewer_created
        ON read_marks (viewer_did, created_at DESC);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_read_marks_cleanup
        ON read_marks (created_at, viewer_did, subject_uri);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_ingestion_checkpoints (
        environment TEXT NOT NULL,
        source TEXT NOT NULL,
        repo_did TEXT NOT NULL,
        collection TEXT NOT NULL,
        cursor TEXT,
        event_time TEXT,
        observed_at TEXT NOT NULL,
        PRIMARY KEY (environment, source, repo_did, collection)
      );
      `,
	); err != nil {
		return err
	}
	if err := migrateIngestionCheckpointEnvironmentIfNeeded(tx); err != nil {
		return err
	}
	if err := sqlitedb.ExecAll(tx,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_ingestion_checkpoints_observed
        ON appview_ingestion_checkpoints (observed_at);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_jetstream_checkpoints (
        environment TEXT NOT NULL,
        source_generation TEXT NOT NULL,
        source_host TEXT NOT NULL,
        stream_nsid TEXT NOT NULL,
        filter_fingerprint TEXT NOT NULL,
        cursor_kind TEXT NOT NULL,
        last_staged_seq INTEGER,
        last_staged_event_at TEXT,
        last_staged_at TEXT,
        last_applied_seq INTEGER,
        last_applied_event_at TEXT,
        last_applied_at TEXT,
        last_reconciled_repo_rev TEXT,
        last_reconciled_at TEXT,
        replay_state TEXT NOT NULL DEFAULT 'idle',
        replay_after_seq INTEGER,
        replay_sealed_seq INTEGER,
        replay_bytes_downloaded INTEGER NOT NULL DEFAULT 0,
        replay_retry_count INTEGER NOT NULL DEFAULT 0,
        replay_range_resume_count INTEGER NOT NULL DEFAULT 0,
        replay_last_progress_at TEXT,
        replay_etag TEXT,
        updated_at TEXT NOT NULL,
        PRIMARY KEY (environment, source_generation)
      );
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_ingestion_inbox (
        environment TEXT NOT NULL,
        source_generation TEXT NOT NULL,
        seq INTEGER NOT NULL,
        source_host TEXT NOT NULL,
        cursor_kind TEXT NOT NULL,
        event_kind TEXT NOT NULL,
        repo_did TEXT NOT NULL,
        collection TEXT,
        operation TEXT,
        repo_rev TEXT,
        record_key TEXT,
        record_cid TEXT,
        payload TEXT NOT NULL,
        event_time TEXT NOT NULL,
        status TEXT NOT NULL DEFAULT 'pending',
        attempt_count INTEGER NOT NULL DEFAULT 0,
        next_attempt_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        lease_owner TEXT,
        lease_token TEXT,
        lease_expires_at TEXT,
        failure_category TEXT,
        failure_reason TEXT,
        staged_at TEXT NOT NULL,
        applied_at TEXT,
        dead_lettered_at TEXT,
        reconciled_at TEXT,
        filtered_scope_policy TEXT,
        filtered_scope_at TEXT,
        expires_at TEXT,
        updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (environment, source_generation, seq)
      );
      `,
	); err != nil {
		return err
	}
	if err := migrateIngestionInboxScopeFilterColumnsIfNeeded(tx); err != nil {
		return err
	}
	if err := sqlitedb.ExecAll(tx,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_ingestion_inbox_claim
        ON appview_ingestion_inbox
          (environment, source_generation, status, next_attempt_at, lease_expires_at, seq);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_ingestion_inbox_repo_fifo
        ON appview_ingestion_inbox (environment, source_generation, repo_did, seq, status);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_ingestion_incidents (
        environment TEXT NOT NULL,
        id TEXT NOT NULL,
        source_generation TEXT,
        source_host TEXT,
        source TEXT NOT NULL,
        cursor_kind TEXT NOT NULL,
        start_cursor INTEGER,
        end_cursor INTEGER,
        category TEXT NOT NULL,
        status TEXT NOT NULL DEFAULT 'open',
        occurrence_count INTEGER NOT NULL DEFAULT 1,
        first_detected_at TEXT NOT NULL,
        last_detected_at TEXT NOT NULL,
        last_error TEXT,
        replay_state TEXT,
        replay_bytes_downloaded INTEGER NOT NULL DEFAULT 0,
        replay_retry_count INTEGER NOT NULL DEFAULT 0,
        replay_range_resume_count INTEGER NOT NULL DEFAULT 0,
        replay_sealed_seq INTEGER,
        recovered_through_cursor INTEGER,
        verification_evidence TEXT NOT NULL DEFAULT '{}',
        resolved_at TEXT,
        updated_at TEXT NOT NULL,
        version INTEGER NOT NULL DEFAULT 0,
        PRIMARY KEY (environment, id)
      );
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_ingestion_reconciliation_requests (
        environment TEXT NOT NULL,
        id TEXT NOT NULL,
        source_generation TEXT NOT NULL,
        repo_did TEXT NOT NULL,
        reason TEXT NOT NULL,
        trigger_seq INTEGER NOT NULL,
        status TEXT NOT NULL DEFAULT 'pending',
        attempt_count INTEGER NOT NULL DEFAULT 0,
        next_attempt_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
        lease_owner TEXT,
        lease_token TEXT,
        lease_expires_at TEXT,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        completed_at TEXT,
        PRIMARY KEY (environment, id),
        UNIQUE (environment, source_generation, repo_did, trigger_seq, reason)
      );
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_ingestion_leases (
        environment TEXT NOT NULL,
        lease_name TEXT NOT NULL,
        source_generation TEXT NOT NULL,
        owner_id TEXT NOT NULL,
        fencing_token INTEGER NOT NULL,
        acquired_at TEXT NOT NULL,
        lease_expires_at TEXT NOT NULL,
        released_at TEXT,
        updated_at TEXT NOT NULL,
        CHECK (fencing_token > 0),
        PRIMARY KEY (environment, lease_name)
      );
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_tap_repo_state (
        environment TEXT NOT NULL,
        repo_did TEXT NOT NULL,
        repo_rev TEXT,
        account_status TEXT NOT NULL,
        pds_base TEXT,
        last_event_id INTEGER,
        last_event_live INTEGER,
        parity_status TEXT NOT NULL,
        matched_event_count INTEGER NOT NULL DEFAULT 0,
        mismatched_event_count INTEGER NOT NULL DEFAULT 0,
        last_mismatch TEXT,
        last_indexed_at TEXT,
        last_validated_at TEXT,
        updated_at TEXT NOT NULL,
        PRIMARY KEY (environment, repo_did)
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_tap_repo_state_parity
        ON appview_tap_repo_state (environment, parity_status, updated_at DESC);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_tap_parity_discrepancies (
        environment TEXT NOT NULL,
        event_id INTEGER NOT NULL,
        repo_did TEXT NOT NULL,
        uri TEXT NOT NULL,
        collection TEXT NOT NULL,
        mismatch_kind TEXT NOT NULL,
        expected_cid TEXT,
        observed_cid TEXT,
        status TEXT NOT NULL,
        opened_at TEXT NOT NULL,
        resolved_at TEXT,
        resolution_event_id INTEGER,
        PRIMARY KEY (environment, event_id)
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_tap_parity_discrepancies_open
        ON appview_tap_parity_discrepancies (environment, repo_did, uri, opened_at)
        WHERE status = 'open';
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_tap_event_receipts (
        environment TEXT NOT NULL,
        event_id INTEGER NOT NULL,
        repo_did TEXT NOT NULL,
        event_type TEXT NOT NULL,
        processed_at TEXT NOT NULL,
        expires_at TEXT NOT NULL,
        PRIMARY KEY (environment, event_id)
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_tap_event_receipts_environment_expires
        ON appview_tap_event_receipts (environment, expires_at, event_id);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_tap_repository_registrations (
        environment TEXT NOT NULL,
        repo_did TEXT NOT NULL,
        is_registered INTEGER NOT NULL,
        registered_at TEXT,
        removed_at TEXT,
        updated_at TEXT NOT NULL,
        PRIMARY KEY (environment, repo_did)
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_tap_repository_registrations_active
        ON appview_tap_repository_registrations (environment, repo_did)
        WHERE is_registered = 1;
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_projection_repair_outbox (
        environment TEXT NOT NULL,
        id TEXT NOT NULL,
        event_id INTEGER NOT NULL,
        uri TEXT NOT NULL,
        author_did TEXT NOT NULL,
        publication_site TEXT,
        action TEXT NOT NULL,
        status TEXT NOT NULL DEFAULT 'queued',
        attempts INTEGER NOT NULL DEFAULT 0,
        lease_owner TEXT,
        lease_until TEXT,
        next_attempt_at TEXT NOT NULL,
        last_error TEXT,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        expires_at TEXT NOT NULL,
        PRIMARY KEY (environment, id),
        UNIQUE (environment, event_id)
      );
      `,
	); err != nil {
		return err
	}
	if err := migrateProjectionRepairEnvironmentPrimaryKeyIfNeeded(tx); err != nil {
		return err
	}
	if err := sqlitedb.ExecAll(tx,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_projection_repair_claim
        ON appview_projection_repair_outbox
          (environment, status, next_attempt_at, lease_until, created_at);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_projection_repair_cleanup
        ON appview_projection_repair_outbox (environment, status, expires_at, id);
      `,
		`
      CREATE TABLE IF NOT EXISTS rss_feed_fetch_metadata (
        feed_url TEXT PRIMARY KEY,
        etag TEXT,
        last_modified TEXT,
        last_poll_at TEXT,
        backoff_until TEXT,
        consecutive_error_count INTEGER NOT NULL DEFAULT 0
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_rss_feed_fetch_metadata_backoff
        ON rss_feed_fetch_metadata (backoff_until);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_publication_scopes (
        viewer_did TEXT NOT NULL,
        publication_id TEXT NOT NULL,
        author_did TEXT NOT NULL,
        publication_at_uri TEXT,
        publication_scope_at_uris TEXT NOT NULL,
        publication_site_urls TEXT NOT NULL,
        scope_keys TEXT NOT NULL,
        section_keys TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        PRIMARY KEY (viewer_did, publication_id)
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_publication_scopes_author
        ON appview_publication_scopes (author_did);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_viewer_feeds (
        viewer_did TEXT NOT NULL,
        feed_kind TEXT NOT NULL,
        feed_id TEXT NOT NULL DEFAULT '',
        updated_at TEXT NOT NULL,
        PRIMARY KEY (viewer_did, feed_kind, feed_id)
      );
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_feed_publications (
        viewer_did TEXT NOT NULL,
        feed_kind TEXT NOT NULL,
        feed_id TEXT NOT NULL DEFAULT '',
        publication_id TEXT NOT NULL,
        PRIMARY KEY (viewer_did, feed_kind, feed_id, publication_id),
        FOREIGN KEY (viewer_did, feed_kind, feed_id)
          REFERENCES appview_viewer_feeds (viewer_did, feed_kind, feed_id)
          ON DELETE CASCADE
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_feed_publications_scope
        ON appview_feed_publications (viewer_did, feed_kind, feed_id, publication_id);
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_publication_scopes_viewer
        ON appview_publication_scopes (viewer_did);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_unread_counters (
        viewer_did TEXT NOT NULL,
        publication_id TEXT NOT NULL,
        unread_count INTEGER NOT NULL,
        generation INTEGER NOT NULL,
        accuracy TEXT NOT NULL,
        dirty INTEGER NOT NULL DEFAULT 0,
        counted_at TEXT NOT NULL,
        PRIMARY KEY (viewer_did, publication_id)
      );
      `,
		`
      CREATE INDEX IF NOT EXISTS idx_appview_unread_counters_dirty
        ON appview_unread_counters (dirty, counted_at);
      `,
		`
      CREATE TABLE IF NOT EXISTS appview_publication_read_floors (
        viewer_did TEXT NOT NULL,
        publication_id TEXT NOT NULL,
        read_floor_at TEXT NOT NULL,
        read_floor_uri TEXT,
        generation INTEGER NOT NULL,
        updated_at TEXT NOT NULL,
        PRIMARY KEY (viewer_did, publication_id)
      );
      `,
	); err != nil {
		return err
	}
	hasFloorURI, err := sqlitedb.ColumnExists(tx, "appview_publication_read_floors", "read_floor_uri")
	if err != nil {
		return err
	}
	if !hasFloorURI {
		if _, err := tx.Exec("ALTER TABLE appview_publication_read_floors ADD COLUMN read_floor_uri TEXT"); err != nil {
			return err
		}
	}
	return sqlitedb.ExecAll(tx, `
      CREATE TABLE IF NOT EXISTS appview_unread_overrides (
        viewer_did TEXT NOT NULL,
        subject_uri TEXT NOT NULL,
        created_at TEXT NOT NULL,
        PRIMARY KEY (viewer_did, subject_uri)
      );
      CREATE INDEX IF NOT EXISTS idx_appview_unread_overrides_cleanup
        ON appview_unread_overrides (created_at, viewer_did, subject_uri);
      `)
}

func migrateIngestionInboxScopeFilterColumnsIfNeeded(tx *sql.Tx) error {
	for _, column := range []string{"filtered_scope_policy", "filtered_scope_at"} {
		exists, err := sqlitedb.ColumnExists(tx, "appview_ingestion_inbox", column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.Exec("ALTER TABLE appview_ingestion_inbox ADD COLUMN " + column + " TEXT"); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateIngestionCheckpointEnvironmentIfNeeded rebuilds the legacy unscoped
// checkpoint table, quarantining its evidence instead of assigning it to the
// active environment (SQLite cannot add a column to an existing primary key).
func migrateIngestionCheckpointEnvironmentIfNeeded(tx *sql.Tx) error {
	exists, err := sqlitedb.ColumnExists(tx, "appview_ingestion_checkpoints", "environment")
	if err != nil || exists {
		return err
	}
	return sqlitedb.ExecAll(tx,
		`
      ALTER TABLE appview_ingestion_checkpoints
        RENAME TO appview_ingestion_checkpoints_legacy_unscoped;
      `,
		`
      CREATE TABLE appview_ingestion_checkpoints (
        environment TEXT NOT NULL,
        source TEXT NOT NULL,
        repo_did TEXT NOT NULL,
        collection TEXT NOT NULL,
        cursor TEXT,
        event_time TEXT,
        observed_at TEXT NOT NULL,
        PRIMARY KEY (environment, source, repo_did, collection)
      );
      `,
		`
      INSERT INTO appview_ingestion_checkpoints
        (environment, source, repo_did, collection, cursor, event_time, observed_at)
      SELECT '__legacy_unscoped__', source, repo_did, collection, cursor, event_time, observed_at
      FROM appview_ingestion_checkpoints_legacy_unscoped;
      `,
		"DROP TABLE appview_ingestion_checkpoints_legacy_unscoped;",
	)
}

// migrateProjectionRepairEnvironmentPrimaryKeyIfNeeded rebuilds early builds'
// global-id outbox as an environment-scoped identifier.
func migrateProjectionRepairEnvironmentPrimaryKeyIfNeeded(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT name, pk FROM pragma_table_info('appview_projection_repair_outbox')`)
	if err != nil {
		return err
	}
	type pkColumn struct {
		position int
		name     string
	}
	var columns []pkColumn
	for rows.Next() {
		var name string
		var position int
		if err := rows.Scan(&name, &position); err != nil {
			rows.Close()
			return err
		}
		if position > 0 {
			columns = append(columns, pkColumn{position, name})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	ordered := make([]string, len(columns))
	for i := range columns {
		for _, column := range columns {
			if column.position == i+1 {
				ordered[i] = column.name
			}
		}
	}
	if len(ordered) == 2 && ordered[0] == "environment" && ordered[1] == "id" {
		return nil
	}
	return sqlitedb.ExecAll(tx, `
      ALTER TABLE appview_projection_repair_outbox
        RENAME TO appview_projection_repair_outbox_legacy;
      CREATE TABLE appview_projection_repair_outbox (
        environment TEXT NOT NULL,
        id TEXT NOT NULL,
        event_id INTEGER NOT NULL,
        uri TEXT NOT NULL,
        author_did TEXT NOT NULL,
        publication_site TEXT,
        action TEXT NOT NULL,
        status TEXT NOT NULL DEFAULT 'queued',
        attempts INTEGER NOT NULL DEFAULT 0,
        lease_owner TEXT,
        lease_until TEXT,
        next_attempt_at TEXT NOT NULL,
        last_error TEXT,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL,
        expires_at TEXT NOT NULL,
        PRIMARY KEY (environment, id),
        UNIQUE (environment, event_id)
      );
      INSERT INTO appview_projection_repair_outbox (
        environment, id, event_id, uri, author_did, publication_site, action, status,
        attempts, lease_owner, lease_until, next_attempt_at, last_error, created_at,
        updated_at, expires_at
      )
      SELECT environment, id, event_id, uri, author_did, publication_site, action, status,
        attempts, lease_owner, lease_until, next_attempt_at, last_error, created_at,
        updated_at, expires_at
      FROM appview_projection_repair_outbox_legacy;
      DROP TABLE appview_projection_repair_outbox_legacy;
      `)
}
