package thinstore

import (
	"database/sql"
	"fmt"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// migrateStoredTimestamps changes only derived comparison keys. Do not run the
// old projection triggers while backfilling content: they alter read marks.
func migrateStoredTimestamps(tx *sql.Tx) error {
	if err := sqlitedb.ExecAll(tx, "DROP TRIGGER IF EXISTS pds_content_INSERT", "DROP TRIGGER IF EXISTS pds_content_UPDATE"); err != nil {
		return err
	}
	var projectionTable int
	if err := tx.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='appview_pds_projection_write'").Scan(&projectionTable); err != nil {
		return err
	}
	var priorActive int
	if projectionTable != 0 {
		if err := tx.QueryRow("SELECT active FROM appview_pds_projection_write WHERE singleton=1").Scan(&priorActive); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE appview_pds_projection_write SET active=1 WHERE singleton=1"); err != nil {
			return err
		}
	}
	for _, column := range []struct{ table, source, target string }{
		{"content_items", "created_at", "created_at_us"},
		{"content_items", "indexed_at", "indexed_at_us"},
		{"appview_publication_read_floors", "read_floor_at", "read_floor_at_us"},
		{"appview_unread_overrides", "created_at", "created_at_us"},
	} {
		exists, err := sqlitedb.ColumnExists(tx, column.table, column.target)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.Exec("ALTER TABLE " + column.table + " ADD COLUMN " + column.target + " INTEGER"); err != nil {
				return err
			}
		}
		if err := backfillTimestampColumn(tx, column.table, column.source, column.target); err != nil {
			return err
		}
	}
	if projectionTable != 0 {
		if _, err := tx.Exec("UPDATE appview_pds_projection_write SET active=? WHERE singleton=1", priorActive); err != nil {
			return err
		}
	}
	if err := sqlitedb.ExecAll(tx, `CREATE INDEX IF NOT EXISTS idx_content_items_rss_indexed_us
		ON content_items(author_did,publication_site,indexed_at_us DESC,uri)`); err != nil {
		return err
	}
	// SQLite cannot add a NOT NULL column to nonempty historical tables without
	// a fabricated default. Guard new writes instead of accepting missing keys.
	for _, table := range []struct{ name, invalid string }{
		{"content_items", "typeof(NEW.created_at_us)!='integer' OR typeof(NEW.indexed_at_us)!='integer'"},
		{"appview_publication_read_floors", "typeof(NEW.read_floor_at_us)!='integer'"},
		{"appview_unread_overrides", "typeof(NEW.created_at_us)!='integer'"},
	} {
		for _, operation := range []string{"INSERT", "UPDATE"} {
			if _, err := tx.Exec("CREATE TRIGGER IF NOT EXISTS stored_timestamp_" + table.name + "_" + operation +
				" BEFORE " + operation + " ON " + table.name + " WHEN " + table.invalid +
				" BEGIN SELECT RAISE(ABORT,'StoredTimestampRequired'); END"); err != nil {
				return err
			}
		}
	}
	return nil
}

// Close each bounded read before updating, so migration memory remains bounded
// and SQLite does not mutate the table underneath a live selection cursor.
func backfillTimestampColumn(tx *sql.Tx, table, source, target string) error {
	type row struct {
		id int64
		at string
	}
	for {
		rows, err := tx.Query("SELECT rowid," + source + " FROM " + table + " WHERE " + target + " IS NULL LIMIT 1000")
		if err != nil {
			return err
		}
		batch := make([]row, 0, 1000)
		for rows.Next() {
			var value row
			if err := rows.Scan(&value.id, &value.at); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, value)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, value := range batch {
			micros, err := sqlitedb.TimestampMicroseconds(value.at)
			if err != nil {
				return fmt.Errorf("backfill %s.%s rowid %d: %w", table, source, value.id, err)
			}
			if _, err := tx.Exec("UPDATE "+table+" SET "+target+"=? WHERE rowid=?", micros, value.id); err != nil {
				return err
			}
		}
	}
}
