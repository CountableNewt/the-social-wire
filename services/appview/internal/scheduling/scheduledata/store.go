// Package scheduledata opens and updates the scheduler database shared with
// existing Skej installations.
package scheduledata

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

const sealedPrefix = "aesgcm:v1:"

type Store struct {
	db  *sqlitedb.DB
	key cipher.AEAD
}

// Open opens path and migrates the schema before exposing the store.
func Open(ctx context.Context, path, secret string, requireEncryption bool) (*Store, error) {
	var key cipher.AEAD
	if secret == "" && requireEncryption {
		return nil, errors.New("SKEJ_AUTH_ENCRYPTION_KEY is required for live OAuth storage")
	}
	if secret != "" {
		digest := sha256.Sum256([]byte(secret))
		block, err := aes.NewCipher(digest[:])
		if err != nil {
			return nil, err
		}
		key, err = cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
	}
	var db *sqlitedb.DB
	var err error
	if path == ":memory:" {
		db, err = sqlitedb.OpenMemory()
	} else {
		if path == "" {
			return nil, errors.New("scheduledata: SQLite path is required")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		db, err = sqlitedb.Open(path)
	}
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, key: key}
	if err := store.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.Writer().ExecContext(ctx, "PRAGMA secure_delete=ON"); err != nil {
		return err
	}
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, scheduleSchema); err != nil {
			return err
		}
		for _, migration := range []struct{ table, column, definition string }{
			{"oauth_states", "auth_server", "TEXT"},
			{"oauth_states", "token_endpoint", "TEXT"},
			{"oauth_states", "pds_endpoint", "TEXT"},
			{"oauth_states", "dpop_key_json", "TEXT"},
			{"oauth_states", "purpose", "TEXT NOT NULL DEFAULT 'sign_in'"},
			{"oauth_states", "initiator_did", "TEXT"},
			{"oauth_states", "invite_token", "TEXT"},
			{"oauth_states", "return_to", "TEXT"},
			{"pro_entitlements", "scope_type", "TEXT NOT NULL DEFAULT 'actor'"},
			{"pro_entitlements", "scope_value", "TEXT"},
			{"web_sessions", "display_name", "TEXT"},
			{"web_sessions", "avatar", "TEXT"},
			{"scheduled_jobs", "scheduled_at", "TEXT"},
			{"scheduled_jobs", "next_attempt_at", "TEXT"},
			{"scheduled_jobs", "last_attempt_at", "TEXT"},
			{"scheduled_jobs", "publish_rkey", "TEXT"},
			{"scheduled_jobs", "record_type", "TEXT"},
			{"scheduled_jobs", "depends_on_schedule_uri", "TEXT"},
			{"scheduled_jobs", "parent_published_uri", "TEXT"},
		} {
			exists, err := sqlitedb.ColumnExists(tx, migration.table, migration.column)
			if err != nil {
				return err
			}
			if !exists {
				if _, err := tx.ExecContext(ctx, "ALTER TABLE "+migration.table+" ADD COLUMN "+migration.column+" "+migration.definition); err != nil {
					return err
				}
			}
		}
		_, err := tx.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS scheduled_jobs_due_idx ON scheduled_jobs (status, scheduled_at, scheduled_for);
			CREATE TABLE IF NOT EXISTS auth_vault_cleanup (id INTEGER PRIMARY KEY CHECK(id = 1), pending INTEGER NOT NULL);
			INSERT OR IGNORE INTO viewer_accounts
				(viewer_did,account_did,access_kind,team_uri,capabilities_json,is_default,created_at,updated_at)
			SELECT did,did,'owner',NULL,'["create","approve","manage","viewAnalytics"]',1,updated_at,updated_at FROM oauth_sessions;
			INSERT OR IGNORE INTO viewer_accounts
				(viewer_did,account_did,access_kind,team_uri,capabilities_json,is_default,created_at,updated_at)
			SELECT did,did,'owner',NULL,'["create","approve","manage","viewAnalytics"]',1,expires_at,expires_at FROM web_sessions;
		`)
		if err != nil {
			return err
		}
		return s.migrateVault(ctx, tx)
	})
	if err != nil {
		return err
	}
	var pending bool
	err = s.db.Writer().QueryRowContext(ctx, "SELECT pending FROM auth_vault_cleanup WHERE id=1").Scan(&pending)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !pending) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.checkpoint(ctx); err != nil {
		return err
	}
	if _, err := s.db.Writer().ExecContext(ctx, "VACUUM"); err != nil {
		return err
	}
	if err := s.checkpoint(ctx); err != nil {
		return err
	}
	if _, err := s.db.Writer().ExecContext(ctx, "UPDATE auth_vault_cleanup SET pending=0 WHERE id=1"); err != nil {
		return err
	}
	return s.checkpoint(ctx)
}

func (s *Store) checkpoint(ctx context.Context) error {
	var busy, logFrames, processed int
	if err := s.db.Writer().QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &processed); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("scheduledata: auth vault cleanup blocked by an active database reader")
	}
	return nil
}

func (s *Store) seal(plaintext string) (string, error) {
	if s.key == nil {
		return plaintext, nil
	}
	nonce := make([]byte, s.key.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	combined := s.key.Seal(nonce, nonce, []byte(plaintext), nil)
	return sealedPrefix + base64.StdEncoding.EncodeToString(combined), nil
}

func (s *Store) open(stored string) (string, error) {
	payload, encrypted := strings.CutPrefix(stored, sealedPrefix)
	if !encrypted {
		return stored, nil
	}
	if s.key == nil {
		return "", errors.New("SKEJ_AUTH_ENCRYPTION_KEY is required to decrypt auth material")
	}
	combined, err := base64.StdEncoding.DecodeString(payload)
	nonceSize := s.key.NonceSize()
	if err != nil || len(combined) < nonceSize+s.key.Overhead() {
		return "", errors.New("scheduledata: malformed encrypted auth material")
	}
	plaintext, err := s.key.Open(nil, combined[:nonceSize], combined[nonceSize:], nil)
	if err != nil || !utf8.Valid(plaintext) {
		return "", errors.New("scheduledata: unable to decrypt auth material")
	}
	return string(plaintext), nil
}

func (s *Store) sealedIfNeeded(value *string) (*string, bool, error) {
	if value == nil {
		return nil, false, nil
	}
	if strings.HasPrefix(*value, sealedPrefix) {
		_, err := s.open(*value)
		return value, false, err
	}
	if s.key == nil {
		return value, false, nil
	}
	sealed, err := s.seal(*value)
	return &sealed, err == nil, err
}

// All vault rows are validated inside one write transaction. A wrong key or
// an interrupted migration leaves every row as it was before opening.
func (s *Store) migrateVault(ctx context.Context, tx *sql.Tx) error {
	changed := false
	for _, spec := range []struct {
		table, id, primary string
	}{
		{"oauth_sessions", "did", "token_json"},
		{"oauth_states", "state", "pkce_verifier"},
	} {
		rows, err := tx.QueryContext(ctx, "SELECT "+spec.id+","+spec.primary+",dpop_key_json FROM "+spec.table)
		if err != nil {
			return err
		}
		type update struct {
			id      string
			primary *string
			dpop    *string
		}
		pending := []update{}
		for rows.Next() {
			var id string
			var raw, rawDPoP *string
			if err := rows.Scan(&id, &raw, &rawDPoP); err != nil {
				rows.Close()
				return err
			}
			primary, replacePrimary, err := s.sealedIfNeeded(raw)
			if err != nil {
				rows.Close()
				return err
			}
			dpop, replaceDPoP, err := s.sealedIfNeeded(rawDPoP)
			if err != nil {
				rows.Close()
				return err
			}
			if replacePrimary || replaceDPoP {
				pending = append(pending, update{id, primary, dpop})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, row := range pending {
			query := fmt.Sprintf("UPDATE %s SET %s=?,dpop_key_json=? WHERE %s=?", spec.table, spec.primary, spec.id)
			if _, err := tx.ExecContext(ctx, query, row.primary, row.dpop, row.id); err != nil {
				return err
			}
			changed = true
		}
	}
	if changed {
		_, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO auth_vault_cleanup(id,pending) VALUES(1,1)")
		return err
	}
	return nil
}
