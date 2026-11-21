package operations

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func openFileDB(t *testing.T) (*sqlitedb.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "operations.sqlite")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func newTestStore(t *testing.T, environment string) *Store {
	t.Helper()
	db, _ := openFileDB(t)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return NewStore(db, environment, nil)
}
