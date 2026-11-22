package appviewruntime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestReadinessIncludesSchedulerAndRemoteSchemaMarkers(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	probe := func(core config.Core, checks readinessChecks) int {
		router := httpx.NewRouter()
		registerHealth(router, core, db, slog.Default(), checks)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return response.Code
	}
	if got := probe(config.Core{}, readinessChecks{}); got != http.StatusOK {
		t.Fatalf("local nil scheduler: %d", got)
	}
	if got := probe(config.Core{}, readinessChecks{scheduling: func(context.Context) error { return errors.New("private connection failure") }}); got != http.StatusServiceUnavailable {
		t.Fatalf("scheduler failure: %d", got)
	}
	remote := config.Core{DatabaseBackend: "turso"}
	if got := probe(remote, readinessChecks{}); got != http.StatusServiceUnavailable {
		t.Fatalf("missing remote marker: %d", got)
	}
	if err := markRuntimeSchema(ctx, db, false); err != nil {
		t.Fatal(err)
	}
	if got := probe(remote, readinessChecks{}); got != http.StatusOK {
		t.Fatalf("ready remote marker: %d", got)
	}
	if got := probe(remote, readinessChecks{draftsEnabled: true}); got != http.StatusServiceUnavailable {
		t.Fatalf("missing draft schema: %d", got)
	}
	if err := invalidateRuntimeSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if got := probe(remote, readinessChecks{}); got != http.StatusServiceUnavailable {
		t.Fatalf("invalidated marker remained ready: %d", got)
	}
}
