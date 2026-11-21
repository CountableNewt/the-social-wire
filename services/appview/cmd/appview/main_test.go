package main

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestInitializeOnlyIsRepeatable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "volume", "social-wire.sqlite")
	env := config.Env{
		"APP_ENV":                           "dev",
		"BACKEND_MODE":                      "standalone",
		"SQLITE_DB_PATH":                    path,
		"ENABLE_THIN_APPVIEW":               "true",
		"PDS_ATTESTATION_RECEIPT_SECRET":    "startup-fixture-attestation-secret",
		"PODCASTS_ENABLED":                  "true",
		"SOCIAL_DRAFTS_ENABLED":             "true",
		"SOCIAL_DRAFT_STORAGE_KEY":          base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"SOCIAL_DRAFT_S3_ENDPOINT":          "https://example.com",
		"SOCIAL_DRAFT_S3_BUCKET":            "fixture",
		"SOCIAL_DRAFT_S3_ACCESS_KEY_ID":     "fixture",
		"SOCIAL_DRAFT_S3_SECRET_ACCESS_KEY": "fixture",
		"SOCIAL_DRAFT_S3_REGION":            "auto",
	}
	ctx := context.Background()
	for range 2 {
		if err := run(ctx, []string{"--initialize-only"}, env, slog.Default()); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"content_items", "operations_service_state", "podcast_episodes", "private_social_draft_accounts"} {
		var name string
		if err := db.Reader().QueryRowContext(ctx, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name = ?", table).Scan(&name); err != nil {
			t.Errorf("missing %s schema: %v", table, err)
		}
	}
}

func TestReadinessRequiresBothMediaChecks(t *testing.T) {
	for _, test := range []struct {
		name, failingPath string
		want              int
	}{
		{name: "ready", want: http.StatusOK},
		{name: "startup", failingPath: "/startupz", want: http.StatusServiceUnavailable},
		{name: "health", failingPath: "/healthz", want: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == test.failingPath {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if r.URL.Path == "/startupz" {
					_, _ = io.WriteString(w, `{"ready":true}`)
				} else {
					_, _ = io.WriteString(w, `{"status":"ok"}`)
				}
			}))
			defer media.Close()
			db, err := sqlitedb.OpenMemory()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			router := httpx.NewRouter()
			core := config.Core{PodcastsEnabled: true, PodcastMediaWorkerURL: media.URL}
			registerHealth(router, core, db, slog.Default())
			for path, want := range map[string]int{"/readyz": test.want, "/livez": http.StatusOK} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != want {
					t.Errorf("%s = %d, want %d", path, response.Code, want)
				}
			}
		})
	}
}
