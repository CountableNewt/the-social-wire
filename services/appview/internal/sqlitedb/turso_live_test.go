package sqlitedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	turso "turso.tech/database/tursogo-serverless"
)

// This opt-in check only reads deliberately isolated probe stores. Credentials
// stay in owner-only files and never appear in normal test output or CI.
func TestLiveTursoReadOnlySessions(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set TSW_TURSO_PROBE_DIR for isolated hosted-engine acceptance")
	}
	for _, name := range []string{"appview", "scheduling"} {
		t.Run(name, func(t *testing.T) {
			read := func(suffix string) string {
				path := filepath.Join(dir, name+suffix)
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
					t.Fatal("probe credentials must be owner-only regular files")
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal("probe credentials could not be read")
				}
				return strings.TrimSpace(string(data))
			}
			endpoint, token := read("-url"), read("-token")
			parsed, err := url.Parse(endpoint)
			if err != nil || !strings.Contains(parsed.Hostname(), "-probe-") {
				t.Fatal("live acceptance requires an isolated probe database")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			fail := func(phase string, err error) {
				// Raw provider diagnostics may contain SQL. Keep the error chain
				// private while returning only a fixed phase to the test runner.
				var chain []string
				for err != nil {
					chain = append(chain, err.Error())
					err = errors.Unwrap(err)
				}
				_ = os.WriteFile(filepath.Join(dir, name+"-readonly-diagnostic.log"), []byte(strings.Join(chain, "\n")), 0600)
				t.Fatalf("hosted probe failed during %s; private diagnostic saved", phase)
			}
			db, err := OpenTurso(ctx, endpoint, token)
			if err != nil {
				fail("connection", err)
			}
			defer db.Close()
			var foreignKeys int
			if err := db.Reader().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
				fail("foreign-key verification", err)
			}
			if foreignKeys != 1 {
				t.Fatal("hosted probe did not retain foreign-key enforcement")
			}
			connection, err := db.Reader().Conn(ctx)
			if err != nil {
				fail("metadata probe connection", err)
			}
			err = connection.Raw(func(raw any) error {
				session := raw.(*tursoConnection).remoteSession
				for _, probe := range []struct{ name, query string }{
					{"inline CTE", "WITH value AS (SELECT 1 AS one) SELECT one FROM value"},
					{"materialized CTE", "WITH value AS MATERIALIZED (SELECT 1 AS one) SELECT one FROM value"},
					{"derived table", "SELECT one FROM (SELECT 1 AS one)"},
				} {
					description, err := session.DescribeContext(ctx, probe.query)
					if err != nil {
						return err
					}
					t.Logf("%s read-only metadata: %t", probe.name, description.IsReadOnly)
				}
				return nil
			})
			connection.Close()
			if err != nil {
				fail("read-only metadata", err)
			}
			if name == "appview" {
				connection, err := db.Reader().Conn(ctx)
				if err != nil {
					fail("query-only probe connection", err)
				}
				err = connection.Raw(func(raw any) (err error) {
					wrapped := raw.(*tursoConnection)
					session := wrapped.remoteSession
					queryOnlyEnabled := false
					if _, err := session.ExecContext(ctx, "BEGIN IMMEDIATE", nil); err != nil {
						return err
					}
					defer func() {
						var resetErr error
						if queryOnlyEnabled {
							_, resetErr = session.ExecContext(ctx, "PRAGMA query_only=OFF", nil)
						}
						_, rollbackErr := session.ExecContext(ctx, "ROLLBACK", nil)
						if resetErr != nil || rollbackErr != nil {
							wrapped.broken = true
							err = errors.Join(err, resetErr, rollbackErr)
						}
					}()
					table := fmt.Sprintf("query_only_probe_%d", time.Now().UnixNano())
					if _, err := session.ExecContext(ctx, "CREATE TABLE "+table+"(id INTEGER PRIMARY KEY)", nil); err != nil {
						return err
					}
					if _, err := session.ExecContext(ctx, "INSERT INTO "+table+" VALUES(1)", nil); err != nil {
						return err
					}
					if _, err := session.ExecContext(ctx, "PRAGMA query_only=ON", nil); err != nil {
						wrapped.broken = true
						if strings.Contains(err.Error(), "SQL not allowed statement") {
							t.Log("PRAGMA query_only is forbidden by the provider; no fallback is permitted")
							return nil
						}
						return err
					}
					queryOnlyEnabled = true
					rows, err := session.QueryContext(ctx, "PRAGMA query_only", nil)
					if err != nil {
						return err
					}
					values := make([]driver.Value, 1)
					nextErr := rows.Next(values)
					closeErr := rows.Close()
					if closeErr != nil {
						return closeErr
					}
					if nextErr != nil || values[0] != int64(1) {
						t.Log("PRAGMA query_only is not verifiably enabled; no fallback is permitted")
						return nil
					}
					if _, err := session.ExecContext(ctx, "INSERT INTO "+table+" VALUES(2)", nil); err == nil {
						return errors.New("query_only reported enabled but allowed controlled DML")
					}
					if _, err := session.ExecContext(ctx, "PRAGMA query_only=OFF", nil); err != nil {
						return err
					}
					if _, err := session.ExecContext(ctx, "INSERT INTO "+table+" VALUES(3)", nil); err != nil {
						return err
					}
					t.Log("PRAGMA query_only verified and controlled DML rejected; rolled-back fixture only")
					return nil
				})
				connection.Close()
				if err != nil {
					fail("query-only enforcement", err)
				}
			}
			for range 2 {
				err := db.Read(ctx, func(tx *sql.Tx) error {
					var number int
					if err := tx.QueryRowContext(ctx, "SELECT CAST(json_extract(?, '$.value') AS INTEGER)", `{"value":42}`).Scan(&number); err != nil {
						return err
					}
					if number != 42 {
						return errors.New("bound JSON query returned an unexpected value")
					}
					statement, err := tx.PrepareContext(ctx, "SELECT ? + 1")
					if err != nil {
						return err
					}
					defer statement.Close()
					if err := statement.QueryRowContext(ctx, 41).Scan(&number); err != nil {
						return err
					}
					if number != 42 {
						return errors.New("prepared query returned an unexpected value")
					}
					return nil
				})
				if err != nil {
					fail("described read-only transaction", err)
				}
			}
		})
	}
}

// Exercise cold connection expansion and write admission like the eight-action
// dispatcher plus a separate request-scoped telemetry writer. Only isolated,
// uniquely named probe leases are touched; no actual maintenance action runs.
func TestLiveTursoColdMaintenanceWriteFanout(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set TSW_TURSO_PROBE_DIR for isolated hosted-engine acceptance")
	}
	read := func(name string) string {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("probe credentials must be owner-only regular files")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("cannot read probe credential")
		}
		return strings.TrimSpace(string(data))
	}
	endpoint, token := read("appview-url"), read("appview-token")
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() != "tsw-dev-appview-probe-countablenewt.aws-us-west-2.turso.io" {
		t.Fatal("isolated AppView probe required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	var failures int
	for round := range 4 {
		pools := make([]*DB, 2)
		for i := range pools {
			pools[i], err = OpenTurso(ctx, endpoint, token)
			if err != nil {
				t.Fatal("probe connection failed")
			}
		}
		prefix := fmt.Sprintf("synthetic-fanout-%d-%d-", time.Now().UnixNano(), round)
		start := make(chan struct{})
		results := make(chan error, 9)
		var group sync.WaitGroup
		for index := range 9 {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				pool := pools[0]
				if index == 8 {
					pool = pools[1]
				}
				results <- pool.Write(ctx, func(tx *sql.Tx) error {
					if index == 8 {
						_, err := tx.ExecContext(ctx, "INSERT INTO http_job_leases(name,owner,expires_at_us) VALUES(?,?,?)", fmt.Sprintf("%s%d", prefix, index), "synthetic", time.Now().Add(time.Minute).UnixMicro())
						return err
					}
					var owner string
					err := tx.QueryRowContext(ctx, `INSERT INTO http_job_leases(name,owner,expires_at_us) VALUES(?,?,?)
						ON CONFLICT(name) DO UPDATE SET owner=excluded.owner,expires_at_us=excluded.expires_at_us
						WHERE http_job_leases.expires_at_us<=? RETURNING owner`, fmt.Sprintf("%s%d", prefix, index), "synthetic", time.Now().Add(time.Minute).UnixMicro(), time.Now().UnixMicro()).Scan(&owner)
					if err == nil && owner != "synthetic" {
						return errors.New("synthetic lease ownership mismatch")
					}
					return err
				})
			}()
		}
		close(start)
		group.Wait()
		close(results)
		for err := range results {
			if err == nil {
				continue
			}
			failures++
			var server *turso.Error
			var httpError *turso.HTTPError
			switch {
			case errors.As(err, &server):
				t.Logf("round=%d server_code=%q extended_code=%q", round, server.Code, server.ExtendedCode)
			case errors.As(err, &httpError):
				t.Logf("round=%d http_status=%d", round, httpError.StatusCode)
			default:
				t.Logf("round=%d error_type=%T", round, err)
			}
		}
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		_, cleanupErr := pools[0].Writer().ExecContext(cleanup, "DELETE FROM http_job_leases WHERE name LIKE ?", prefix+"%")
		done()
		for _, pool := range pools {
			_ = pool.Close()
		}
		if cleanupErr != nil {
			kind, code := ErrorDiagnostic(cleanupErr)
			t.Fatalf("synthetic probe cleanup failed kind=%s code=%s", kind, code)
		}
	}
	if failures != 0 {
		t.Fatalf("cold maintenance fanout failed %d of 36 synthetic writes", failures)
	}
	t.Log("all 36 synthetic writes completed across four cold nine-writer rounds")
}
