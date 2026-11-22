package jobs

import (
	"context"
	"fmt"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveTursoIndependentMaintenanceLeaseOwners(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set TSW_TURSO_PROBE_DIR for hosted lease acceptance")
	}
	read := func(name string) string {
		p := filepath.Join(dir, name)
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("probe credentials must be private regular files")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal("cannot read probe credential")
		}
		return strings.TrimSpace(string(b))
	}
	endpoint, token := read("appview-url"), read("appview-token")
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() != "tsw-dev-appview-probe-countablenewt.aws-us-west-2.turso.io" {
		t.Fatal("isolated AppView probe required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	fail := func(phase string, err error) {
		t.Helper()
		if err != nil {
			_ = os.WriteFile(filepath.Join(dir, "maintenance-lease-diagnostic.log"), []byte(err.Error()), 0600)
			t.Fatalf("hosted lease acceptance failed during %s; private diagnostic saved", phase)
		}
	}
	runners := make([]*Runner, 2)
	for i := range runners {
		db, err := sqlitedb.OpenTurso(ctx, endpoint, token)
		fail("connection", err)
		t.Cleanup(func() { db.Close() })
		runners[i], err = New(db, "", nil)
		fail("runner", err)
	}
	name := fmt.Sprintf("synthetic-lease-acceptance-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := runners[0].db.Writer().ExecContext(cleanup, "DELETE FROM http_job_leases WHERE name=?", name)
		if err != nil {
			t.Error("synthetic maintenance lease cleanup failed")
		}
	})
	now := time.Now()
	expires := now.Add(time.Minute)
	type result struct {
		index int
		held  bool
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for i, r := range runners {
		go func(i int, r *Runner) {
			<-start
			held, err := r.claim(ctx, name, fmt.Sprintf("owner-%d", i), now, expires)
			results <- result{i, held, err}
		}(i, r)
	}
	close(start)
	winner := -1
	for range runners {
		r := <-results
		fail("concurrent claim", r.err)
		if r.held {
			if winner != -1 {
				t.Fatal("two maintenance owners acquired one lease")
			}
			winner = r.index
		}
	}
	if winner < 0 {
		t.Fatal("no maintenance owner acquired lease")
	}
	loser := 1 - winner
	ok, err := runners[loser].claim(ctx, name, "successor", expires, expires.Add(time.Minute))
	fail("expired takeover", err)
	if !ok {
		t.Fatal("expired maintenance lease did not transfer")
	}
	fail("stale release", runners[winner].release(ctx, name, fmt.Sprintf("owner-%d", winner)))
	var owner string
	err = runners[loser].db.Reader().QueryRowContext(ctx, "SELECT owner FROM http_job_leases WHERE name=?", name).Scan(&owner)
	fail("successor verification", err)
	if owner != "successor" {
		t.Fatal("stale release removed successor")
	}
	t.Log("independent hosted maintenance owners, expiry takeover and stale-release protection passed; no maintenance actions executed")
}
