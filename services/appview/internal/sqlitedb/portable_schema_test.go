package sqlitedb

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableDatabaseReopensWithoutPrivateTimestampExtensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portable.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec(`CREATE TABLE entries (at TEXT NOT NULL, at_us INTEGER NOT NULL);
		CREATE INDEX entries_timestamp ON entries(at_us);
		INSERT INTO entries VALUES ('2026-01-01T00:00:00.000001Z', 1767225600000001)`); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPortableSchema(context.Background(), db.Reader()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	plain, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	var timestamp int64
	if err := plain.QueryRow("SELECT at_us FROM entries ORDER BY at_us").Scan(&timestamp); err != nil || timestamp != 1767225600000001 {
		t.Fatalf("portable read: %d %v", timestamp, err)
	}
	if _, err := plain.Exec("SELECT tsw_timestamp_us('2026-01-01T00:00:00Z')"); err == nil {
		t.Fatal("private timestamp function must not be registered")
	}
}

func TestPortableSchemaRejectsUnknownDependentObjectsWithoutDeletingThem(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Writer().Exec(`CREATE TABLE example (value TEXT);
		CREATE TRIGGER custom_legacy_trigger AFTER INSERT ON example
		BEGIN SELECT tsw_timestamp_us(NEW.value); END`); err != nil {
		t.Fatal(err)
	}
	err = VerifyPortableSchema(context.Background(), db.Reader())
	if err == nil || !strings.Contains(err.Error(), "custom_legacy_trigger") {
		t.Fatalf("missing portability diagnostic: %v", err)
	}
	var count int
	if err := db.Reader().QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='custom_legacy_trigger'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("unknown object was removed: %d %v", count, err)
	}
}
