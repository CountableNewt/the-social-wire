package appviewruntime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
)

func TestMigrationMaintenanceAllSurfacesBypassInvalidDependencies(t *testing.T) {
	for _, surface := range []Surface{All, Reader, Publishing, Scheduling, Analytics, Integrations, Maintenance} {
		t.Run(string(surface), func(t *testing.T) {
			env := config.Env{"MIGRATION_MAINTENANCE_MODE": "true", "RUNTIME_MODE": "serverless", "DATABASE_BACKEND": "invalid", "TURSO_DATABASE_URL": "invalid", "SOCIAL_DRAFT_STORAGE_KEY": "invalid", "SOCIAL_DRAFTS_ENABLED": "true", "SCHEDULING_STORAGE_KEY": "invalid", "PORT": "invalid"}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			// CLI listen overrides work without parsing or opening any normal dependency.
			if e := runSurface(ctx, surface, []string{"serve", "--port", "0", "--hostname", "127.0.0.1"}, env, slog.New(slog.NewTextHandler(io.Discard, nil))); e != nil {
				t.Fatalf("maintenance initialized normal dependencies: %v", e)
			}
		})
	}
}

func TestMigrationMaintenanceRejectsEveryRouteAndMethod(t *testing.T) {
	handler := migrationMaintenanceHandler()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace, "CUSTOM"} {
		for _, path := range []string{"/", "/readyz", "/healthz", "/internal/cron/maintenance", "/internal/jobs/scheduling", "/v1/appview/bootstrap-stream", "/xrpc/app.thesocialwire.getFeed"} {
			request := httptest.NewRequest(method, path, nil)
			request.Header.Set("Authorization", "Bearer private")
			request.Header.Set("Origin", "https://example.invalid")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 503 || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Retry-After") != "60" {
				t.Fatalf("maintenance exception: %s %s", method, path)
			}
			if response.Body.String() != "Service temporarily unavailable\n" || response.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("unexpected maintenance response")
			}
		}
	}
}

func TestMigrationMaintenanceRejectsInitializationAndInvalidArguments(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	env := config.Env{"MIGRATION_MAINTENANCE_MODE": "true", "RUNTIME_MODE": "serverless"}
	for _, args := range [][]string{{"--initialize-only"}, {"--initialize-only=false"}, {"serve", "--initialize-only"}, {"--unknown"}, {"serve", "extra"}, {"--port", "-1"}, {"--port", "65536"}, {"--port", "invalid"}} {
		if e := runSurface(t.Context(), Maintenance, args, env, logger); e == nil {
			t.Fatal("unsafe maintenance arguments accepted")
		}
	}
	for _, mode := range []string{"", "server", "invalid"} {
		env["RUNTIME_MODE"] = mode
		if e := runSurface(t.Context(), Reader, nil, env, logger); e == nil || !strings.Contains(e.Error(), "serverless") {
			t.Fatal("non-serverless maintenance accepted")
		}
	}
}

func TestDisabledMigrationMaintenancePreservesNormalConfigurationFailure(t *testing.T) {
	for _, value := range []string{"", "false"} {
		for _, surface := range []Surface{All, Reader, Publishing, Scheduling, Analytics, Integrations, Maintenance} {
			env := config.Env{"MIGRATION_MAINTENANCE_MODE": value, "DATABASE_BACKEND": "invalid"}
			if e := runSurface(t.Context(), surface, nil, env, slog.New(slog.NewTextHandler(io.Discard, nil))); e == nil || strings.Contains(e.Error(), "migration maintenance") {
				t.Fatal("disabled guard changed normal initialization")
			}
		}
	}
	if _, e := migrationMaintenanceEnabled(config.Env{"MIGRATION_MAINTENANCE_MODE": "tru"}); e == nil {
		t.Fatal("maintenance typo failed open")
	}
}
