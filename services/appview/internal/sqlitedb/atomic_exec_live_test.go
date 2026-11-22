package sqlitedb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This opt-in comparison owns only uniquely named synthetic lease rows in the
// disposable probe. No actual job is invoked and all fixture rows are removed.
func TestLiveTursoAtomicExecComparison(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set disposable probe profile for mutation comparison")
	}
	read := func(name string) string {
		path := filepath.Join(dir, name)
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("private probe profile required")
		}
		data, e := os.ReadFile(path)
		if e != nil {
			t.Fatal("probe profile unavailable")
		}
		return strings.TrimSpace(string(data))
	}
	endpoint, token := read("appview-url"), read("appview-token")
	parsed, e := url.Parse(endpoint)
	if e != nil || parsed.Hostname() != "tsw-dev-appview-probe-countablenewt.aws-us-west-2.turso.io" {
		t.Fatal("isolated probe required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	db, e := OpenTurso(ctx, endpoint, token)
	if e != nil {
		t.Fatal("probe unavailable")
	}
	defer db.Close()
	prefix := fmt.Sprintf("atomic-exec-probe-%d-", time.Now().UnixNano())
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		if _, e := db.ExecContext(cleanup, "DELETE FROM http_job_leases WHERE name LIKE ?", prefix+"%"); e != nil {
			kind, code := ErrorDiagnostic(e)
			t.Errorf("fixture cleanup kind=%s code=%s", kind, code)
		}
	}()
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("%s%d", prefix, i)
		if _, e := db.ExecContext(ctx, "INSERT INTO http_job_leases(name,owner,expires_at_us) VALUES(?,?,?)", key, "synthetic", int64(0)); e != nil {
			kind, code := ErrorDiagnostic(e)
			t.Fatalf("fixture insert kind=%s code=%s", kind, code)
		}
		start := time.Now()
		var rows int64
		if i == 0 {
			result, err := db.Writer().ExecContext(ctx, "DELETE FROM http_job_leases WHERE name=?", key)
			e = err
			if e == nil {
				rows, e = result.RowsAffected()
			}
		} else {
			result, err := db.ExecContext(ctx, "DELETE FROM http_job_leases WHERE name=?", key)
			e = err
			if e == nil {
				rows, e = result.RowsAffected()
			}
		}
		kind, code := ErrorDiagnostic(e)
		t.Logf("atomic=%t duration_ms=%d rows=%d kind=%s code=%s", i == 1, time.Since(start).Milliseconds(), rows, kind, code)
		if e != nil || rows != 1 {
			t.Fail()
		}
	}
}
