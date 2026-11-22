package scheduledata

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Only explicitly isolated stores are eligible; no OAuth requests or real jobs run.
func TestLiveTursoIndependentSchedulingLeaseOwners(t *testing.T) {
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
	endpoint, token := read("scheduling-url"), read("scheduling-token")
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() != "tsw-dev-scheduling-probe-countablenewt.aws-us-west-2.turso.io" {
		t.Fatal("isolated scheduling probe required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	fail := func(phase string, err error) {
		t.Helper()
		if err != nil {
			_ = os.WriteFile(filepath.Join(dir, "scheduling-lease-diagnostic.log"), []byte(err.Error()), 0600)
			t.Fatalf("hosted lease acceptance failed during %s; private diagnostic saved", phase)
		}
	}
	stores := make([]*Store, 2)
	for i := range stores {
		db, err := sqlitedb.OpenTurso(ctx, endpoint, token)
		fail("connection", err)
		t.Cleanup(func() { db.Close() })
		var count int
		err = db.Reader().QueryRowContext(ctx, "SELECT (SELECT count(*) FROM oauth_sessions)+(SELECT count(*) FROM scheduled_jobs)").Scan(&count)
		fail("empty probe check", err)
		if count != 0 {
			t.Fatal("scheduling probe contains user state")
		}
		stores[i], err = encryptedStore(db, "synthetic-lease-acceptance-key", true)
		fail("store", err)
	}
	actor := fmt.Sprintf("did:example:lease-acceptance-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, table := range []string{"oauth_refresh_leases", "scheduler_actor_leases", "pending_schedule_submissions"} {
			column := "did"
			if table == "scheduler_actor_leases" {
				column = "actor_did"
			}
			_, err := stores[0].db.Writer().ExecContext(cleanup, "DELETE FROM "+table+" WHERE "+column+"=?", actor)
			if err != nil {
				t.Errorf("synthetic fixture cleanup failed for %s", table)
			}
		}
	})
	race := func(claim func(*Store, string) (bool, error)) int {
		type result struct {
			index int
			held  bool
			err   error
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		for i, s := range stores {
			go func(i int, s *Store) {
				<-start
				held, err := claim(s, fmt.Sprintf("owner-%d", i))
				results <- result{i, held, err}
			}(i, s)
		}
		close(start)
		winner := -1
		for range stores {
			r := <-results
			fail("concurrent claim", r.err)
			if r.held {
				if winner != -1 {
					t.Fatal("two independent owners acquired one lease")
				}
				winner = r.index
			}
		}
		if winner < 0 {
			t.Fatal("no independent owner acquired lease")
		}
		return winner
	}
	now := time.Now().UnixMicro()
	until := now + int64(time.Minute/time.Microsecond)
	winner := race(func(s *Store, owner string) (bool, error) {
		return s.ClaimActorLease(ctx, "create", actor, owner, now, until, 32)
	})
	loser := 1 - winner
	owner := fmt.Sprintf("owner-%d", winner)
	fail("foreign release", stores[loser].ReleaseActorLease(ctx, "create", actor, fmt.Sprintf("owner-%d", loser)))
	fail("owner retained", stores[winner].CheckActorLease(ctx, "create", actor, owner, now))
	if err := stores[loser].RememberScheduleSubmissionOwned(ctx, actor, "synthetic", fmt.Sprintf("owner-%d", loser), "2026-01-01T00:00:00Z"); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("foreign create owner passed submission fence")
	}
	ok, err := stores[loser].ClaimActorLease(ctx, "create", actor, "successor", until, until+1_000_000, 32)
	fail("expired takeover", err)
	if !ok {
		t.Fatal("expired create lease did not transfer")
	}
	if err := stores[winner].RenewActorLease(ctx, "create", actor, owner, until, until+2_000_000); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("stale create owner renewed successor lease")
	}
	now = time.Now().UnixMicro()
	until = now + int64(time.Minute/time.Microsecond)
	winner = race(func(s *Store, owner string) (bool, error) {
		return s.ClaimOAuthRefresh(ctx, actor, owner, "synthetic-token-hash", now, until)
	})
	owner = fmt.Sprintf("owner-%d", winner)
	fail("refresh intent", stores[winner].BeginOAuthRefresh(ctx, actor, owner, "synthetic-token-hash", now))
	ok, err = stores[1-winner].ClaimOAuthRefresh(ctx, actor, "successor", "synthetic-token-hash", until, until+1_000_000)
	if ok || !errors.Is(err, ErrRefreshUncertain) {
		t.Fatal("expired refresh intent permitted reuse of single-use token")
	}
	t.Log("independent hosted create/refresh owners, stale-owner fences, expiry takeover and consumed-token protection passed")
}
