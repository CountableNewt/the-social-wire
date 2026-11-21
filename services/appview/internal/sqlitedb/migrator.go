package sqlitedb

import (
	"context"
	"database/sql"
)

// Migration is one named schema step. Identifiers are permanent: databases
// created by the Swift backend already record them in grdb_migrations.
type Migration struct {
	ID    string
	Apply func(*sql.Tx) error
}

// Migrate applies, in order, each migration whose identifier is not yet in
// GRDB's grdb_migrations table, so existing production files skip steps the
// Swift DatabaseMigrator already ran.
func (db *DB) Migrate(ctx context.Context, migrations []Migration) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS grdb_migrations (identifier TEXT NOT NULL PRIMARY KEY)`); err != nil {
			return err
		}
		for _, migration := range migrations {
			var applied int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM grdb_migrations WHERE identifier = ?`, migration.ID).Scan(&applied); err != nil {
				return err
			}
			if applied > 0 {
				continue
			}
			if err := migration.Apply(tx); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO grdb_migrations (identifier) VALUES (?)`, migration.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// ExecAll runs each statement in order inside tx, for idempotent schema scripts.
func ExecAll(tx *sql.Tx, statements ...string) error {
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// ColumnExists reports whether table has column, for additive ALTER TABLE steps.
func ColumnExists(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
