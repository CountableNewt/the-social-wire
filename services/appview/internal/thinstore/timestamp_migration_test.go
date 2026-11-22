package thinstore

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestContentWritersStoreComparisonKeysForTheirSerializedTimestamps(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := SQLiteIngestStore{DB: db}
	item := IndexedContentItem{URI: "entry", CID: "first", AuthorDID: "author", Collection: "site.standard.document",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 999999500, time.UTC), IndexedAt: time.Date(2026, 1, 1, 0, 0, 2, 123456789, time.UTC), ExpiresAt: distantFuture}
	check := func() {
		t.Helper()
		var created, indexed string
		var createdMicros, indexedMicros int64
		if err := db.Reader().QueryRow("SELECT created_at,indexed_at,created_at_us,indexed_at_us FROM content_items WHERE uri='entry'").Scan(&created, &indexed, &createdMicros, &indexedMicros); err != nil {
			t.Fatal(err)
		}
		wantCreated, err := sqlitedb.TimestampMicroseconds(created)
		if err != nil {
			t.Fatal(err)
		}
		wantIndexed, err := sqlitedb.TimestampMicroseconds(indexed)
		if err != nil {
			t.Fatal(err)
		}
		if created != pdsTimestamp(item.CreatedAt) || indexed != isoString(item.IndexedAt) || createdMicros != wantCreated || indexedMicros != wantIndexed {
			t.Fatalf("stored keys diverge from serialized timestamps: %s %s %d %d", created, indexed, createdMicros, indexedMicros)
		}
	}
	if inserted, err := store.InsertContentItemIfAbsent(ctx, item); err != nil || !inserted {
		t.Fatalf("insert failed: %v %v", inserted, err)
	}
	check()
	item.CID = "second"
	item.CreatedAt = item.CreatedAt.Add(time.Second)
	item.IndexedAt = item.IndexedAt.Add(time.Second)
	if err := store.UpsertContentItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	check()
	item.CID = "third"
	item.CreatedAt = item.CreatedAt.Add(time.Second)
	item.IndexedAt = item.IndexedAt.Add(time.Second)
	if changed, err := store.CompareAndSwapContent(ctx, item, "second"); err != nil || !changed {
		t.Fatalf("CAS failed: %v %v", changed, err)
	}
	check()
}

func TestStoredTimestampMigrationPreservesReadProjectionAndOriginalStrings(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Write(ctx, migrateCore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec(`
		INSERT INTO content_items VALUES('entry','cid','author','collection','2026-01-01T01:00:00.000000500+01:00','2026-01-01T00:00:00.999999Z',NULL,'{}','2027-01-01T00:00:00Z');
		INSERT INTO read_marks VALUES('viewer','entry','2026-01-02T00:00:00Z');
		INSERT INTO appview_publication_read_floors VALUES('viewer','publication','2026-01-01T00:00:00.000000500Z',NULL,1,'2026-01-02T00:00:00Z');
		INSERT INTO appview_unread_overrides VALUES('viewer','override','2026-01-01T00:00:00.999999Z');
		CREATE TABLE appview_pds_projection_write(singleton INTEGER PRIMARY KEY,active INTEGER NOT NULL);
		INSERT INTO appview_pds_projection_write VALUES(1,0);
		CREATE TRIGGER pds_content_UPDATE AFTER UPDATE ON content_items BEGIN DELETE FROM read_marks; END;
		CREATE TRIGGER pds_fence_fixture BEFORE UPDATE ON appview_publication_read_floors WHEN (SELECT active FROM appview_pds_projection_write WHERE singleton=1)=0 BEGIN SELECT RAISE(ABORT,'must preserve projection authority'); END;
	`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var raw string
	var created, indexed int64
	if err := db.Reader().QueryRow("SELECT created_at,created_at_us,indexed_at_us FROM content_items WHERE uri='entry'").Scan(&raw, &created, &indexed); err != nil {
		t.Fatal(err)
	}
	want, _ := sqlitedb.TimestampMicroseconds("2026-01-01T00:00:00.000000500Z")
	if raw != "2026-01-01T01:00:00.000000500+01:00" || created != want || indexed != want-1+999999 {
		t.Fatalf("changed timestamp representation or instant: %q %d %d", raw, created, indexed)
	}
	var floor, override int64
	if err := db.Reader().QueryRow("SELECT read_floor_at_us FROM appview_publication_read_floors").Scan(&floor); err != nil {
		t.Fatal(err)
	}
	if err := db.Reader().QueryRow("SELECT created_at_us FROM appview_unread_overrides").Scan(&override); err != nil {
		t.Fatal(err)
	}
	if floor != created || override != indexed {
		t.Fatalf("comparison keys disagree: %d %d", floor, override)
	}
	var count, active int
	if err := db.Reader().QueryRow("SELECT COUNT(*) FROM read_marks").Scan(&count); err != nil || count != 1 {
		t.Fatalf("read projection changed: %d %v", count, err)
	}
	if err := db.Reader().QueryRow("SELECT active FROM appview_pds_projection_write").Scan(&active); err != nil || active != 0 {
		t.Fatalf("authority flag not restored: %d %v", active, err)
	}
	if _, err := db.Writer().Exec("INSERT INTO appview_unread_overrides(viewer_did,subject_uri,created_at) VALUES('viewer','missing','2026-01-01T00:00:00Z')"); err == nil || !strings.Contains(err.Error(), "StoredTimestampRequired") {
		t.Fatalf("accepted missing derived key: %v", err)
	}
}

func TestStoredTimestampMigrationRollsBackInvalidHistoricalRows(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Write(ctx, migrateCore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec("INSERT INTO content_items VALUES('entry','cid','author','collection','invalid','2026-01-01T00:00:00Z',NULL,'{}','2027-01-01T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, migrateStoredTimestamps)
	if err == nil || !strings.Contains(err.Error(), "content_items.created_at rowid") {
		t.Fatalf("missing migration row context: %v", err)
	}
	if err := db.Write(ctx, func(tx *sql.Tx) error {
		exists, err := sqlitedb.ColumnExists(tx, "content_items", "created_at_us")
		if err == nil && exists {
			t.Fatal("partial migration column persisted")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
