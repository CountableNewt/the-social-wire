package sqlitedb

import (
	"context"
	"database/sql"
	"testing"
)

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
