package appviewruntime

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/analytics"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	jobrunner "github.com/CountableNewt/the-social-wire/services/appview/internal/jobs"
	scheduler "github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/backend"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestFunctionSurfacesMountOnlyOwnedProtectedRoutes(t *testing.T) {
	env := config.Env{"APP_ENV": "dev", "SQLITE_DB_PATH": filepath.Join(t.TempDir(), "appview.sqlite"), "PDS_ATTESTATION_RECEIPT_SECRET": "surface-fixture-attestation-secret", "SCHEDULING_PUBLIC_ORIGIN": "https://fixture.example", "SCHEDULING_STORAGE_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)), "LATR_IOS_PROXY_URL": "https://fixture.example", "CRON_SECRET": strings.Repeat("c", 32)}
	core, err := config.LoadCore(env)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client := publicHTTPClient()
	defer client.CloseIdleConnections()
	embedded, err := scheduler.Open(t.Context(), core, env, client, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer embedded.Close()
	runner, err := jobrunner.New(db, strings.Repeat("j", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	routes := []struct {
		surface      Surface
		method, path string
	}{
		{Reader, http.MethodGet, "/xrpc/app.thesocialwire.appview.getFeed"},
		{Reader, http.MethodGet, "/xrpc/app.thesocialwire.publication.getSidebar"},
		{Reader, http.MethodGet, "/xrpc/app.thesocialwire.sync.getPreferences"},
		{Publishing, http.MethodGet, "/xrpc/app.thesocialwire.articleDraft.list"},
		{Publishing, http.MethodGet, "/xrpc/app.thesocialwire.socialDraft.getManifest"},
		{Publishing, http.MethodGet, "/v1/media/gifs"},
		{Publishing, http.MethodGet, "/xrpc/app.thesocialwire.publication.getArticleEmbed"},
		{Scheduling, http.MethodGet, "/xrpc/at.skej.schedule.list"},
		{Analytics, http.MethodGet, "/v1/analytics/summary"},
		{Integrations, http.MethodGet, "/v1/semble/collections"},
		{Integrations, http.MethodGet, "/xrpc/link.latr.bookmarks.listBookmarks"},
		{Maintenance, http.MethodPost, "/internal/jobs/scheduling"},
		{Maintenance, http.MethodGet, "/internal/schedules/maintenance"},
	}
	for _, surface := range []Surface{Reader, Publishing, Scheduling, Analytics, Integrations, Maintenance} {
		t.Run(string(surface), func(t *testing.T) {
			handler, err := buildRouter(core, env, runtimeServices{surface: surface, db: db, repo: atproto.NewRepoClient(client, core.PLCURL, slog.Default()), analytics: analytics.NewStore(db, "fixture"), scheduling: embedded, maintenance: runner, context: t.Context(), logger: slog.Default(), serverless: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range routes {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
				want := http.StatusNotFound
				if route.surface == surface {
					want = http.StatusUnauthorized
				}
				if response.Code != want {
					t.Errorf("%s %s returned%d,want%d", route.method, route.path, response.Code, want)
				}
			}
			for _, route := range []struct {
				owner Surface
				path  string
			}{{Reader, "/ios-client-metadata.json"}, {Reader, "/freshness"}, {Scheduling, "/oauth/scheduling/client-metadata.json"}} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route.path, nil))
				want := http.StatusNotFound
				if route.owner == surface {
					want = http.StatusOK
				}
				if response.Code != want {
					t.Errorf("metadata %s returned%d,want%d", route.path, response.Code, want)
				}
			}
		})
	}
}

func TestIntegrationsDoNotOpenEitherDatabaseOrConfigureUnownedServices(t *testing.T) {
	// Every unrelated dependency is deliberately invalid. Constructing integrations
	// must neither contact database endpoints nor configure draft/scheduler secrets.
	core := config.Core{DatabaseBackend: "turso", TursoDatabaseURL: "invalid", SchedulingTursoDatabaseURL: "invalid", AttestationReceiptSecret: strings.Repeat("a", 32)}
	env := config.Env{"SOCIAL_DRAFTS_ENABLED": "true", "SOCIAL_DRAFT_STORAGE_KEY": "invalid", "SCHEDULING_STORAGE_KEY": "invalid"}
	services, close, err := openSpecializedServices(t.Context(), Integrations, core, env, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if services.db != nil || services.drafts != nil || services.scheduling != nil || services.analytics != nil || services.maintenance != nil || services.enroll != nil {
		t.Fatal("integrations initialized an unrelated dependency")
	}
	router, err := buildRouter(core, env, services)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("database-free readiness=%d", response.Code)
	}
}

func TestSchedulingDoesNotOpenAppViewOrConfigureDraftStorage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-database.sqlite")
	if err := os.WriteFile(path, []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	core := config.Core{DatabaseBackend: "sqlite", SQLitePath: path}
	env := config.Env{"SCHEDULING_PUBLIC_ORIGIN": "https://fixture.example", "SCHEDULING_STORAGE_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)), "SOCIAL_DRAFTS_ENABLED": "true", "SOCIAL_DRAFT_STORAGE_KEY": "invalid"}
	services, close, err := openSpecializedServices(t.Context(), Scheduling, core, env, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if services.db != nil || services.drafts != nil || services.analytics != nil || services.maintenance != nil || services.enroll != nil || services.scheduling == nil {
		t.Fatal("scheduling initialized an unrelated dependency or omitted its vault")
	}
	if err := services.scheduling.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "not sqlite" {
		t.Fatal("scheduling touched the AppView database")
	}
}

func TestReaderAndAnalyticsIgnoreDraftAndSchedulingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appview.sqlite")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := initialize(t.Context(), db, false); err != nil {
		t.Fatal(err)
	}
	if err := markRuntimeSchema(t.Context(), db, false); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	core := config.Core{DatabaseBackend: "sqlite", SQLitePath: path}
	env := config.Env{"APP_ENV": "dev", "PUBLIC_CONTENT_DISCOVERY_MODE": "off", "SOCIAL_DRAFTS_ENABLED": "true", "SOCIAL_DRAFT_STORAGE_KEY": "invalid", "SCHEDULING_STORAGE_KEY": "invalid"}
	for _, surface := range []Surface{Reader, Analytics} {
		t.Run(string(surface), func(t *testing.T) {
			services, close, err := openSpecializedServices(t.Context(), surface, core, env, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			defer close()
			if services.db == nil || services.drafts != nil || services.scheduling != nil || services.maintenance != nil {
				t.Fatal("surface opened unowned storage")
			}
			if (services.enroll != nil) != (surface == Reader) {
				t.Fatal("reader enrollment ownership changed")
			}
			if (services.analytics != nil) != (surface == Analytics) {
				t.Fatal("analytics ownership changed")
			}
		})
	}
}

func TestSpecializedSurfacesCannotStartStandaloneTimers(t *testing.T) {
	for _, surface := range []Surface{Reader, Publishing, Scheduling, Analytics, Integrations, Maintenance} {
		env := config.Env{"APP_ENV": "local", "PDS_ATTESTATION_RECEIPT_SECRET": "fixture"}
		err := runSurface(t.Context(), surface, nil, env, slog.Default())
		if err == nil || !strings.Contains(err.Error(), "specialized AppView surfaces require serverless") {
			t.Fatalf("%s accepted an interval-runner startup: %v", surface, err)
		}
	}
}
