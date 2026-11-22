package sqlitedb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// VerifyPortableSchema rejects persisted objects that still depend on the
// retired connection extensions. Run after schema upgrades, before serving.
// Unknown objects are reported, never dropped by a prefix or text match.
func VerifyPortableSchema(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT type,name,sql FROM sqlite_schema WHERE sql IS NOT NULL ORDER BY type,name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, name, definition string
		if err := rows.Scan(&kind, &name, &definition); err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(definition), "tsw_timestamp") {
			return fmt.Errorf("%s %q still requires retired timestamp extensions", kind, name)
		}
	}
	return rows.Err()
}
