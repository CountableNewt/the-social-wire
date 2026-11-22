package scheduledata

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Bump this version whenever serving requires a schema change; remote startup
// deliberately cannot repair schema drift by applying migrations.
const remoteSchemaVersion = "scheduling-turso-v1"

var errRemoteVault = errors.New("scheduling snapshot requires local encrypted-vault migration and secure cleanup before Turso import")

type remoteQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) verifyRemoteVault(ctx context.Context, db remoteQueryer) error {
	for _, spec := range []struct{ table, primary string }{{"oauth_sessions", "token_json"}, {"oauth_states", "pkce_verifier"}} {
		var exists int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?", spec.table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		rows, err := db.QueryContext(ctx, "SELECT "+spec.primary+",dpop_key_json FROM "+spec.table)
		if err != nil {
			return errRemoteVault
		}
		for rows.Next() {
			var primary, dpop *string
			if err := rows.Scan(&primary, &dpop); err != nil {
				rows.Close()
				return errRemoteVault
			}
			for _, raw := range []*string{primary, dpop} {
				if raw == nil {
					continue
				}
				if !strings.HasPrefix(*raw, sealedPrefix) {
					rows.Close()
					return errRemoteVault
				}
				if _, err := s.open(*raw); err != nil {
					rows.Close()
					return errors.New("scheduling encrypted snapshot could not be verified with the configured storage key")
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	var exists int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='auth_vault_cleanup'").Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		var pending int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM auth_vault_cleanup WHERE pending!=0").Scan(&pending); err != nil {
			return err
		}
		if pending != 0 {
			return errRemoteVault
		}
	}
	return nil
}

func (s *Store) invalidateReadiness(ctx context.Context) error {
	var exists int
	if err := s.db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='scheduler_schema_readiness'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err := s.db.Writer().ExecContext(ctx, "DELETE FROM scheduler_schema_readiness")
	return err
}
func (s *Store) markReady(ctx context.Context) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS scheduler_schema_readiness(singleton INTEGER PRIMARY KEY CHECK(singleton=1),version TEXT NOT NULL)"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO scheduler_schema_readiness(singleton,version) VALUES(1,?)", remoteSchemaVersion)
		return err
	})
}
func (s *Store) verifySchemaVersion(ctx context.Context) error {
	var version string
	if err := s.db.Reader().QueryRowContext(ctx, "SELECT version FROM scheduler_schema_readiness WHERE singleton=1").Scan(&version); err != nil || version != remoteSchemaVersion {
		return errors.New("scheduling schema is not ready; run AppView --initialize-only before serving Turso")
	}
	return nil
}

func (s *Store) verifyReady(ctx context.Context) error {
	if err := s.verifySchemaVersion(ctx); err != nil {
		return err
	}
	return s.verifyRemoteVault(ctx, s.db.Reader())
}

// Ready is a lightweight probe. Full vault/key validation occurs at startup.
func (s *Store) Ready(ctx context.Context) error {
	if err := s.db.Ping(ctx); err != nil {
		return err
	}
	if s.db.IsRemote() {
		return s.verifySchemaVersion(ctx)
	}
	return nil
}
