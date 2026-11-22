package appviewruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Bump this version whenever serving requires a schema change; remote startup
// deliberately cannot repair schema drift by applying migrations.
const runtimeSchemaVersion = "appview-turso-v3"

func openDatabase(ctx context.Context, core config.Core) (*sqlitedb.DB, error) {
	if core.DatabaseBackend == "turso" {
		return sqlitedb.OpenTurso(ctx, core.TursoDatabaseURL, core.TursoAuthToken)
	}
	if err := os.MkdirAll(filepath.Dir(core.SQLitePath), 0700); err != nil {
		return nil, fmt.Errorf("create SQLite directory: %w", err)
	}
	return sqlitedb.Open(core.SQLitePath)
}

func invalidateRuntimeSchema(ctx context.Context, db *sqlitedb.DB) error {
	var exists int
	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='appview_schema_readiness'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, "DELETE FROM appview_schema_readiness")
	return err
}

func markRuntimeSchema(ctx context.Context, db *sqlitedb.DB, draftsEnabled bool) error {
	return db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS appview_schema_readiness(singleton INTEGER PRIMARY KEY CHECK(singleton=1),version TEXT NOT NULL,drafts_enabled INTEGER NOT NULL CHECK(drafts_enabled IN (0,1)))"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO appview_schema_readiness(singleton,version,drafts_enabled) VALUES(1,?,?)", runtimeSchemaVersion, draftsEnabled)
		return err
	})
}

func verifyRuntimeSchemaMarker(ctx context.Context, db *sqlitedb.DB, draftsEnabled bool) error {
	var version string
	var draftsReady bool
	if err := db.Reader().QueryRowContext(ctx, "SELECT version,drafts_enabled FROM appview_schema_readiness WHERE singleton=1").Scan(&version, &draftsReady); err != nil {
		return fmt.Errorf("verify AppView schema readiness: %w", err)
	}
	if version != runtimeSchemaVersion || (draftsEnabled && !draftsReady) {
		return errors.New("AppView schema is not ready; run AppView --initialize-only before serving Turso")
	}
	return nil
}

func verifyRuntimeSchema(ctx context.Context, db *sqlitedb.DB, draftsEnabled bool) error {
	if err := verifyRuntimeSchemaMarker(ctx, db, draftsEnabled); err != nil {
		return err
	}
	return sqlitedb.VerifyPortableSchema(ctx, db.Reader())
}
