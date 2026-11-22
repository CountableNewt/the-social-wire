package reader

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// migrateBoundaryMicroseconds upgrades legacy boundary rows before projection
// triggers are installed. Original timestamp strings remain the PDS/API values.
func migrateBoundaryMicroseconds(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqlitedb.ColumnExists(tx, "appview_pds_read_state_boundaries", "boundary_at_us")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE appview_pds_read_state_boundaries ADD COLUMN boundary_at_us INTEGER"); err != nil {
			return err
		}
	}
	for {
		rows, err := tx.QueryContext(ctx, "SELECT rowid,boundary_at FROM appview_pds_read_state_boundaries WHERE boundary_at_us IS NULL LIMIT 256")
		if err != nil {
			return err
		}
		type boundaryRow struct{ id, micros int64 }
		batch := []boundaryRow{}
		for rows.Next() {
			var id int64
			var raw string
			if err := rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return err
			}
			micros, err := sqlitedb.TimestampMicroseconds(raw)
			if err != nil {
				rows.Close()
				return fmt.Errorf("backfill appview_pds_read_state_boundaries row %d: %w", id, err)
			}
			batch = append(batch, boundaryRow{id, micros})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		for _, row := range batch {
			if _, err := tx.ExecContext(ctx, "UPDATE appview_pds_read_state_boundaries SET boundary_at_us=? WHERE rowid=?", row.micros, row.id); err != nil {
				return err
			}
		}
	}
	for _, operation := range []string{"INSERT", "UPDATE"} {
		if _, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS boundary_microseconds_`+operation+` BEFORE `+operation+` ON appview_pds_read_state_boundaries
			WHEN typeof(NEW.boundary_at_us) != 'integer'
			BEGIN SELECT RAISE(ABORT,'BoundaryTimestampMicrosecondsRequired'); END;`); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS appview_pds_boundary_microseconds_lookup
		ON appview_pds_read_state_boundaries(viewer_did,author_did,boundary_at_us)`)
	return err
}
