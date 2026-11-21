package sqlitedb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenRegistersTimestampExtensions(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	err = db.Write(ctx, func(tx *sql.Tx) error {
		return ExecAll(tx,
			`CREATE TABLE items (at TEXT NOT NULL COLLATE tsw_timestamp)`,
			`CREATE INDEX items_at ON items(at COLLATE tsw_timestamp)`,
			`INSERT INTO items VALUES ('2026-01-01T00:00:01Z'), ('2026-01-01T00:00:00.500000Z'), ('2026-01-01T01:00:00+02:00')`)
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Reader().QueryContext(ctx, `SELECT at FROM items ORDER BY at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var at string
		if err := rows.Scan(&at); err != nil {
			t.Fatal(err)
		}
		got = append(got, at)
	}
	want := []string{"2026-01-01T01:00:00+02:00", "2026-01-01T00:00:00.500000Z", "2026-01-01T00:00:01Z"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	var micros int64
	if err := db.Reader().QueryRowContext(ctx, `SELECT tsw_timestamp_us('1970-01-01T00:00:01.5Z')`).Scan(&micros); err != nil || micros != 1_500_000 {
		t.Fatalf("%d %v", micros, err)
	}
}

func TestCompareTimestamps(t *testing.T) {
	if CompareTimestamps("2026-01-01T00:00:00Z", "2026-01-01T00:00:00.000001Z") >= 0 {
		t.Fatal("whole second should sort before a later microsecond")
	}
	if CompareTimestamps("2026-01-01T02:00:00+02:00", "2026-01-01T00:30:00Z") >= 0 {
		t.Fatal("offset values compare by instant")
	}
	if CompareTimestamps("b", "a") <= 0 {
		t.Fatal("unparseable values compare as text")
	}
}

func TestMigrateSkipsRecordedIdentifiers(t *testing.T) {
	db, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	runs := 0
	migrations := []Migration{{ID: "v1", Apply: func(tx *sql.Tx) error {
		runs++
		_, err := tx.Exec(`CREATE TABLE t (id INTEGER)`)
		return err
	}}}
	for range 2 {
		if err := db.Migrate(ctx, migrations); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 {
		t.Fatalf("ran %d times", runs)
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		ok, err := ColumnExists(tx, "t", "id")
		if err == nil && !ok {
			t.Fatal("column missing")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
