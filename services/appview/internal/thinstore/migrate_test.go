package thinstore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestRetiredFeatureCleanupPreservesLiveAndUnlistedTables(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		if err := sqlitedb.ExecAll(tx,
			`CREATE TABLE grdb_migrations (identifier TEXT PRIMARY KEY)`,
			`INSERT INTO grdb_migrations VALUES ('existing-swift-migration')`,
			`CREATE TABLE podcast_unrelated_extension (id TEXT PRIMARY KEY)`,
			`INSERT INTO podcast_unrelated_extension VALUES ('keep')`,
			`CREATE TABLE private_social_draft_accounts (id TEXT PRIMARY KEY)`,
			`INSERT INTO private_social_draft_accounts VALUES ('private-draft')`,
		); err != nil {
			return err
		}
		for _, table := range retiredFeatureTables {
			if _, err := tx.Exec("CREATE TABLE " + table + " (id TEXT PRIMARY KEY)"); err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO " + table + " VALUES ('retired')"); err != nil {
				return err
			}
		}
		// These reproduce the historical dependency chain, with nonempty rows,
		// so dropping a parent before its children fails with foreign keys on.
		return sqlitedb.ExecAll(tx,
			`DROP TABLE podcast_clips; DROP TABLE podcast_jobs; DROP TABLE podcast_subscriptions; DROP TABLE podcast_episodes`,
			`CREATE TABLE podcast_episodes (id TEXT PRIMARY KEY, show_id TEXT REFERENCES podcast_shows(id)); INSERT INTO podcast_episodes VALUES ('episode', 'retired')`,
			`CREATE TABLE podcast_jobs (id TEXT PRIMARY KEY, episode_id TEXT REFERENCES podcast_episodes(id)); INSERT INTO podcast_jobs VALUES ('job', 'episode')`,
			`CREATE TABLE podcast_clips (id TEXT PRIMARY KEY, episode_id TEXT REFERENCES podcast_episodes(id)); INSERT INTO podcast_clips VALUES ('clip', 'episode')`,
			`CREATE TABLE podcast_subscriptions (id TEXT PRIMARY KEY, show_id TEXT REFERENCES podcast_shows(id)); INSERT INTO podcast_subscriptions VALUES ('subscription', 'retired')`,
			`DROP TABLE podcast_private_episodes; CREATE TABLE podcast_private_episodes (id TEXT PRIMARY KEY, show_id TEXT REFERENCES podcast_private_shows(id)); INSERT INTO podcast_private_episodes VALUES ('private-episode', 'retired')`,
			`DROP TABLE finance_personalized_snapshots; CREATE TABLE finance_personalized_snapshots (id TEXT PRIMARY KEY, generation_id TEXT REFERENCES finance_generations(id)); INSERT INTO finance_personalized_snapshots VALUES ('snapshot', 'retired')`,
			`DROP TABLE sports_personalized_snapshots; CREATE TABLE sports_personalized_snapshots (id TEXT PRIMARY KEY, generation_id TEXT REFERENCES sports_generations(id)); INSERT INTO sports_personalized_snapshots VALUES ('snapshot', 'retired')`,
		)
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range retiredFeatureTables {
		var count int
		if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil || count != 0 {
			t.Errorf("retired table %s remains: count=%d error=%v", table, count, err)
		}
	}
	for table, want := range map[string]string{"podcast_unrelated_extension": "keep", "private_social_draft_accounts": "private-draft"} {
		var id string
		if err := db.Reader().QueryRow("SELECT id FROM " + table).Scan(&id); err != nil || id != want {
			t.Errorf("live row lost in %s: id=%q error=%v", table, id, err)
		}
	}
	var migrations int
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM grdb_migrations WHERE identifier IN ('existing-swift-migration', 'retire-gated-feature-tables-v1')`).Scan(&migrations); err != nil || migrations != 2 {
		t.Fatalf("migration history not preserved: count=%d error=%v", migrations, err)
	}
}

func TestRetiredFeatureCleanupOnFreshDatabase(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for range 2 {
		if err := Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.Reader().QueryRow(`SELECT COUNT(*) FROM content_items`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reader schema unavailable: count=%d error=%v", count, err)
	}
}
