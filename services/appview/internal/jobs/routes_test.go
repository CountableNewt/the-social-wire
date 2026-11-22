package jobs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

func TestJobHTTPUsesDedicatedSecretAndDoesNotRequireViewerOAuth(t *testing.T) {
	db := runnerDatabase(t, filepath.Join(t.TempDir(), "jobs.sqlite"))
	calls := 0
	secret := strings.Repeat("secret", 8)
	runner, err := New(db, secret, map[string]Action{"discovery": {Run: func(context.Context) error { calls++; return nil }, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	router := httpx.NewRouter()
	Register(router, runner)
	for _, authorization := range []string{"", "Bearer wrong", "DPoP " + secret} {
		request := httptest.NewRequest(http.MethodPost, "/internal/jobs/discovery", nil)
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		reply := httptest.NewRecorder()
		router.ServeHTTP(reply, request)
		if reply.Code != http.StatusUnauthorized || calls != 0 {
			t.Fatalf("unauthorized execution: %d calls=%d", reply.Code, calls)
		}
		if reply.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("job response cacheable")
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/jobs/discovery", nil)
	request.Header.Set("Authorization", "Bearer "+secret)
	reply := httptest.NewRecorder()
	router.ServeHTTP(reply, request)
	if reply.Code != http.StatusOK || calls != 1 || !strings.Contains(reply.Body.String(), `"completed":true`) {
		t.Fatalf("valid job: %d %s calls=%d", reply.Code, reply.Body.String(), calls)
	}
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/internal/jobs/discovery", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET accepted: %d", get.Code)
	}
}

func TestMissingSecretAndDisabledActionsNeverExecute(t *testing.T) {
	db := runnerDatabase(t, filepath.Join(t.TempDir(), "jobs.sqlite"))
	for _, secret := range []string{"", strings.Repeat("s", 32)} {
		runner, err := New(db, secret, nil)
		if err != nil {
			t.Fatal(err)
		}
		router := httpx.NewRouter()
		Register(router, runner)
		request := httptest.NewRequest(http.MethodPost, "/internal/jobs/scheduling", nil)
		request.Header.Set("Authorization", "Bearer "+secret)
		reply := httptest.NewRecorder()
		router.ServeHTTP(reply, request)
		if reply.Code != http.StatusServiceUnavailable {
			t.Fatalf("disabled execution: %d", reply.Code)
		}
	}
}

func TestBusyJobReturnsUnclaimedWithoutRunningTwice(t *testing.T) {
	db := runnerDatabase(t, filepath.Join(t.TempDir(), "jobs.sqlite"))
	runner, err := New(db, strings.Repeat("s", 32), map[string]Action{"discovery": {Run: func(context.Context) error { t.Fatal("duplicate execution"); return nil }, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := runner.claim(context.Background(), "discovery", "other", time.Now(), time.Now().Add(time.Minute)); err != nil || !claimed {
		t.Fatal(err)
	}
	router := httpx.NewRouter()
	Register(router, runner)
	request := httptest.NewRequest(http.MethodPost, "/internal/jobs/discovery", nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("s", 32))
	reply := httptest.NewRecorder()
	router.ServeHTTP(reply, request)
	if reply.Code != http.StatusOK || !strings.Contains(reply.Body.String(), `"claimed":false`) {
		t.Fatalf("busy response: %d %s", reply.Code, reply.Body.String())
	}
}

func TestCronRequiresSeparateSecretAndHeadNeverRunsWork(t *testing.T) {
	db := runnerDatabase(t, filepath.Join(t.TempDir(), "cron.sqlite"))
	calls := 0
	manual, cron := strings.Repeat("m", 32), strings.Repeat("c", 32)
	runner, err := New(db, manual, map[string]Action{"rss-poll": {Run: func(context.Context) error { calls++; return nil }, Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	router := httpx.NewRouter()
	Register(router, runner, cron)
	for _, example := range []struct {
		method, path, secret string
		status               int
	}{
		{http.MethodGet, "/internal/cron/rss-poll", manual, http.StatusUnauthorized},
		{http.MethodPost, "/internal/jobs/rss-poll", cron, http.StatusUnauthorized},
		{http.MethodHead, "/internal/cron/rss-poll", cron, http.StatusMethodNotAllowed},
		{http.MethodGet, "/internal/cron/rss-poll", cron, http.StatusOK},
	} {
		request := httptest.NewRequest(example.method, example.path, nil)
		request.Header.Set("Authorization", "Bearer "+example.secret)
		reply := httptest.NewRecorder()
		router.ServeHTTP(reply, request)
		if reply.Code != example.status {
			t.Fatalf("%s %s: %d", example.method, example.path, reply.Code)
		}
	}
	if calls != 1 {
		t.Fatalf("cron executions=%d", calls)
	}
}
