package scheduledata

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Empty isolated stores exercise the exact worker SQL without publishing,
// refreshing accounts, modifying real jobs or initializing remote schemas.
func TestLiveTursoEmptyWorkerQueries(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set TSW_TURSO_PROBE_DIR for isolated worker acceptance")
	}
	read := func(name string) string {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("private probe files required")
		}
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("cannot read probe credentials")
		}
		return strings.TrimSpace(string(value))
	}
	endpoint, token := read("scheduling-url"), read("scheduling-token")
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() != "tsw-dev-scheduling-probe-countablenewt.aws-us-west-2.turso.io" {
		t.Fatal("isolated scheduling probe required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	db, err := sqlitedb.OpenTurso(ctx, endpoint, token)
	if err != nil {
		t.Fatal("cannot open isolated scheduling probe")
	}
	defer db.Close()
	var count int
	err = db.Reader().QueryRowContext(ctx, "SELECT (SELECT count(*) FROM oauth_sessions)+(SELECT count(*) FROM scheduled_jobs)+(SELECT count(*) FROM pending_schedule_submissions)").Scan(&count)
	if err != nil || count != 0 {
		t.Fatal("worker probe must contain no user or pending state")
	}
	store, err := encryptedStore(db, "synthetic-worker-probe-key", true)
	if err != nil {
		t.Fatal("cannot create probe store")
	}
	for _, phase := range []string{"cold", "after-idle"} {
		if phase == "after-idle" {
			// Retained HTTP sessions must also work after a typical serverless gap.
			select {
			case <-time.After(35 * time.Second):
			case <-ctx.Done():
				t.Fatal("idle probe deadline exceeded")
			}
		}
		now := time.Now()
		for _, probe := range []struct {
			name string
			run  func() error
		}{
			{"pending-submission", func() error {
				_, err := store.NextPendingScheduleSubmission(ctx, scheduling.ScheduleTimestamp(now), scheduling.ScheduleTimestamp(now.Add(5*time.Minute)), scheduling.ScheduleTimestamp(now.Add(-24*time.Hour)))
				return err
			}},
			{"blocked-jobs", func() error { _, err := store.BlockedJobs(ctx); return err }},
			{"due-jobs", func() error { _, err := store.ClaimDueJobs(ctx, scheduling.ScheduleTimestamp(now), 1); return err }},
		} {
			t.Run(phase+"/"+probe.name, func(t *testing.T) {
				if err := probe.run(); err != nil {
					kind, code := sqlitedb.ErrorDiagnostic(err)
					t.Fatalf("worker probe failed: kind=%s code=%s", kind, code)
				}
			})
		}
	}
}
