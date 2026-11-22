// Package preflight inspects offline SQLite snapshots without changing them.
package preflight

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

var (
	ErrSnapshot  = errors.New("use an existing absolute, regular offline SQLite snapshot, not a live database or symlink")
	ErrSidecar   = errors.New("snapshot has SQLite sidecar data; create a new verified offline backup first")
	ErrIntegrity = errors.New("snapshot integrity or foreign-key verification failed")
	ErrPortable  = errors.New("snapshot schema still requires private timestamp extensions; migrate locally and create a new verified backup")
	ErrVault     = errors.New("scheduling snapshot requires local vault migration and cleanup with the existing storage key, followed by a new verified backup")
	ErrSchema    = errors.New("snapshot does not contain the expected database schema")
	ErrRead      = errors.New("snapshot inspection failed")
)

// Inspect accepts a previously verified, standalone snapshot. It does not open
// application stores, run migrations, read authentication values into reports,
// or connect to any remote database. Callers must never pass a live main file.
func Inspect(ctx context.Context, path, kind string) (report Report, err error) {
	if kind != "appview" && kind != "scheduling" {
		return report, ErrSchema
	}
	before, err := snapshotFile(path)
	if err != nil {
		return report, err
	}
	uri := url.URL{Scheme: "file", Path: path}
	// Immutable prevents SQLite itself creating WAL/SHM sidecars. The caller
	// promises an offline snapshot; sidecars and file identity/mtime are checked
	// before and after inspection to reject an accidentally changing input.
	query := url.Values{"mode": {"ro"}, "immutable": {"1"}, "_pragma": {"query_only(1)", "busy_timeout(1000)"}}
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return report, ErrRead
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return report, ErrRead
	}
	defer tx.Rollback()
	if err := verifyIntegrity(ctx, tx); err != nil {
		return report, err
	}
	objects, tables, err := inventory(ctx, tx)
	if err != nil {
		return report, ErrRead
	}
	if err := expectedTables(kind, tables); err != nil {
		return report, err
	}
	// Apply vault checks whenever auth tables exist, even if a caller selected
	// appview by mistake. Never let a kind label bypass authentication checks.
	if kind == "scheduling" || tables["oauth_sessions"] || tables["oauth_states"] {
		if err := verifyVault(ctx, tx, tables); err != nil {
			return report, err
		}
	}
	report = Report{Kind: kind, DatabaseBytes: before.Size(), Tables: []TableCount{}, MigrationIdentifiers: []string{}}
	encoded, err := json.Marshal(objects)
	if err != nil {
		return Report{}, ErrRead
	}
	digest := sha256.Sum256(encoded)
	report.SchemaSHA256 = hex.EncodeToString(digest[:])
	for _, object := range objects {
		if object.Type != "table" || strings.HasPrefix(object.Name, "sqlite_") {
			continue
		}
		var count int64
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdentifier(object.Name)).Scan(&count); err != nil {
			return Report{}, ErrRead
		}
		report.Tables = append(report.Tables, TableCount{Name: object.Name, Rows: count})
	}
	if tables["grdb_migrations"] {
		rows, err := tx.QueryContext(ctx, "SELECT identifier FROM grdb_migrations ORDER BY identifier")
		if err != nil {
			return Report{}, ErrRead
		}
		for rows.Next() {
			var identifier string
			if err := rows.Scan(&identifier); err != nil {
				rows.Close()
				return Report{}, ErrRead
			}
			report.MigrationIdentifiers = append(report.MigrationIdentifiers, identifier)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return Report{}, ErrRead
		}
	}
	if err := tx.Commit(); err != nil {
		return Report{}, ErrRead
	}
	if err := sqlitedb.VerifyPortableSchema(ctx, db); err != nil {
		if ctx.Err() != nil {
			return Report{}, ErrRead
		}
		return Report{}, ErrPortable
	}
	after, err := snapshotFile(path)
	if err != nil {
		return Report{}, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return Report{}, ErrSnapshot
	}
	return report, nil
}

func snapshotFile(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrSnapshot
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrSnapshot
	}
	for _, suffix := range []string{"-wal", "-journal", "-log"} {
		sidecar, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !sidecar.Mode().IsRegular() || sidecar.Size() != 0 {
			return nil, ErrSidecar
		}
	}
	return info, nil
}

func verifyIntegrity(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return ErrIntegrity
	}
	count := 0
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil || result != "ok" {
			rows.Close()
			return ErrIntegrity
		}
		count++
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil || count != 1 {
		return ErrIntegrity
	}
	rows, err = tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return ErrIntegrity
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		return ErrIntegrity
	}
	return nil
}

func inventory(ctx context.Context, tx *sql.Tx) ([]schemaObject, map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_schema ORDER BY type,name,tbl_name")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	objects := []schemaObject{}
	tables := map[string]bool{}
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.Type, &object.Name, &object.Table, &object.SQL); err != nil {
			return nil, nil, err
		}
		objects = append(objects, object)
		if object.Type == "table" {
			tables[object.Name] = true
		}
	}
	return objects, tables, rows.Err()
}

func expectedTables(kind string, tables map[string]bool) error {
	required := []string{"content_items", "grdb_migrations"}
	if kind == "scheduling" {
		required = []string{"oauth_sessions", "oauth_states", "scheduled_jobs", "auth_vault_cleanup"}
	}
	for _, name := range required {
		if !tables[name] {
			return ErrSchema
		}
	}
	return nil
}

func verifyVault(ctx context.Context, tx *sql.Tx, tables map[string]bool) error {
	if !tables["oauth_sessions"] || !tables["oauth_states"] || !tables["auth_vault_cleanup"] {
		return ErrVault
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM auth_vault_cleanup WHERE pending!=0 OR typeof(pending)!='integer')").Scan(&pending); err != nil || pending {
		return ErrVault
	}
	for _, spec := range []struct{ table, first string }{{"oauth_sessions", "token_json"}, {"oauth_states", "pkce_verifier"}} {
		rows, err := tx.QueryContext(ctx, "SELECT "+quoteIdentifier(spec.first)+",dpop_key_json FROM "+quoteIdentifier(spec.table))
		if err != nil {
			return ErrVault
		}
		for rows.Next() {
			var first, key sql.NullString
			if err := rows.Scan(&first, &key); err != nil {
				rows.Close()
				return ErrVault
			}
			for index, value := range []sql.NullString{first, key} {
				if !value.Valid {
					if index == 0 || spec.table == "oauth_sessions" {
						rows.Close()
						return ErrVault
					}
					continue
				}
				encoded, sealed := strings.CutPrefix(value.String, "aesgcm:v1:")
				if !sealed {
					rows.Close()
					return ErrVault
				}
				ciphertext, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil || len(ciphertext) < 28 {
					rows.Close()
					return ErrVault
				}
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return ErrVault
		}
	}
	return nil
}

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
