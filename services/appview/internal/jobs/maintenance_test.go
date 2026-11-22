package jobs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

func TestConsolidatedCadenceSurvivesIndependentRunnersAndJitter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance.sqlite")
	var posts, rss, ttl, analytics atomic.Int32
	actions := map[string]Action{}
	for name, count := range map[string]*atomic.Int32{"scheduling": &posts, "rss-poll": &rss, "ttl-cleanup": &ttl, "analytics-cleanup": &analytics} {
		actions[name] = Action{Timeout: time.Second, Run: func(context.Context) error { count.Add(1); return nil }}
	}
	a, _ := New(runnerDatabase(t, path), "", actions)
	b, _ := New(runnerDatabase(t, path), "", actions)
	now := time.Now()
	for _, at := range []time.Time{now, now.Add(time.Minute), now.Add(30 * time.Minute), now.Add(time.Hour), now.Add(24 * time.Hour)} {
		if _, err := b.runMaintenanceAt(context.Background(), at); err != nil {
			t.Fatal(err)
		}
		// Every dispatch must consider due posts, including dispatches jittered
		// less than fifteen minutes apart. Longer maintenance uses durable gates.
		if _, err := a.runMaintenanceAt(context.Background(), at); err != nil {
			t.Fatal(err)
		}
	}
	if posts.Load() != 10 || rss.Load() != 4 || ttl.Load() != 3 || analytics.Load() != 2 {
		t.Fatalf("wrong cadence: posts=%d rss=%d ttl=%d analytics=%d", posts.Load(), rss.Load(), ttl.Load(), analytics.Load())
	}
}

func TestConsolidatedFailureDoesNotSuppressRetryOrOtherActions(t *testing.T) {
	db := runnerDatabase(t, filepath.Join(t.TempDir(), "failure.sqlite"))
	var attempts, posts atomic.Int32
	runner, _ := New(db, "", map[string]Action{
		"rss-poll": {Timeout: time.Second, Run: func(context.Context) error {
			if attempts.Add(1) == 1 {
				return errors.New("fixture failure")
			}
			return nil
		}},
		"scheduling": {Timeout: time.Second, Run: func(context.Context) error { posts.Add(1); return nil }},
	})
	now := time.Now()
	if _, err := runner.runMaintenanceAt(context.Background(), now); err == nil {
		t.Fatal("failure hidden")
	}
	if _, err := runner.runMaintenanceAt(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || posts.Load() != 2 {
		t.Fatal("failed cadence was not retried or canceled other work")
	}
}

func TestConsolidatedScheduleUsesOnlyCronSecretAndHeadCannotRun(t *testing.T) {
	db := runnerDatabase(t, filepath.Join(t.TempDir(), "routes.sqlite"))
	var calls atomic.Int32
	manual, cron := strings.Repeat("m", 32), strings.Repeat("c", 32)
	runner, _ := New(db, manual, map[string]Action{"scheduling": {Timeout: time.Second, Run: func(context.Context) error { calls.Add(1); return nil }}})
	router := httpx.NewRouter()
	Register(router, runner, cron)
	for _, probe := range []struct {
		method, secret string
		status         int
	}{
		{"GET", "", 401}, {"GET", manual, 401}, {"HEAD", cron, 405}, {"POST", cron, 405}, {"GET", cron, 200},
	} {
		request := httptest.NewRequest(probe.method, "/internal/schedules/maintenance", nil)
		request.Header.Set("Authorization", "Bearer "+probe.secret)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != probe.status {
			t.Fatalf("%s: %d", probe.method, response.Code)
		}
		if response.Header().Get("Cache-Control") != "no-store" && probe.method == http.MethodGet {
			t.Fatal("cacheable maintenance response")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected execution: %d", calls.Load())
	}
}
