package sqlitedb

import (
	"context"
	"database/sql"
)

// ExecContext executes one mutation. Remote mutations use the existing explicit
// write transaction: Turso's autocommit cursor path can delay write admission.
// A result is usable only after successful commit; uncertain outcomes are never
// retried. SQLite retains its original direct-execution behavior.
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if !db.remote {
		return db.write.ExecContext(ctx, query, args...)
	}
	var result sql.Result
	err := db.Write(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = tx.ExecContext(ctx, query, args...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
