package analytics

import (
	"context"
	"database/sql"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Migrate installs the SQLite schema used by the existing creator analytics store.
func Migrate(ctx context.Context, db *sqlitedb.DB) error {
	return db.Migrate(ctx, []sqlitedb.Migration{{ID: "creator-analytics-v1", Apply: func(tx *sql.Tx) error {
		_, err := tx.Exec(`
        CREATE TABLE creator_analytics_items (
          author_did TEXT NOT NULL, subject_uri TEXT NOT NULL, item_json TEXT NOT NULL,
          refreshed_at REAL NOT NULL, PRIMARY KEY(author_did,subject_uri));
        CREATE TABLE creator_analytics_refresh (
          author_did TEXT PRIMARY KEY, refreshed_at REAL NOT NULL, inventory_complete INTEGER NOT NULL);
        CREATE TABLE creator_analytics_snapshots (
          author_did TEXT NOT NULL, subject_uri TEXT NOT NULL, day TEXT NOT NULL,
          likes INTEGER NOT NULL, replies INTEGER NOT NULL, reposts INTEGER NOT NULL, quotes INTEGER NOT NULL,
          PRIMARY KEY(author_did,subject_uri,day));
        CREATE TABLE creator_analytics_events (
          author_did TEXT NOT NULL, subject_uri TEXT NOT NULL, day TEXT NOT NULL, viewer_key TEXT NOT NULL,
          event TEXT NOT NULL CHECK(event IN ('impression','view','outbound_click')),
          surface TEXT NOT NULL CHECK(surface IN ('social-feed','article-feed','post-viewer','article-reader','messages')),
          PRIMARY KEY(subject_uri,day,viewer_key,event));
        CREATE INDEX creator_analytics_events_budget ON creator_analytics_events(day,viewer_key);
        CREATE INDEX creator_analytics_events_retention ON creator_analytics_events(day);
        CREATE TABLE creator_analytics_tracking (
          author_did TEXT NOT NULL, subject_uri TEXT NOT NULL, day TEXT NOT NULL,
          impressions INTEGER NOT NULL, views INTEGER NOT NULL, outbound_clicks INTEGER NOT NULL,
          PRIMARY KEY(author_did,subject_uri,day));
        `)
		return err
	}}})
}
