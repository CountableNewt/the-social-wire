package drafts

import (
	"context"
	"database/sql"
	_ "embed"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

//go:embed schema.sql
var schema string

// Migrate initializes the schema shared with the Swift standalone runtime.
func Migrate(ctx context.Context, db *sqlitedb.DB) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, schema)
		return err
	})
}
