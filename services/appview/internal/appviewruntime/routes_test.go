package appviewruntime

import (
	"bufio"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	scheduler "github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/backend"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestStandaloneRouterMountsDocumentedProtectedRoutes(t *testing.T) {
	env := config.Env{
		"APP_ENV":                        "dev",
		"SQLITE_DB_PATH":                 filepath.Join(t.TempDir(), "social-wire.sqlite"),
		"PDS_ATTESTATION_RECEIPT_SECRET": "routing-fixture-attestation-key-at-least-32",
		"SCHEDULING_PUBLIC_ORIGIN":       "https://routing-fixture.example",
		"SCHEDULING_STORAGE_KEY":         base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}
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
	logger := slog.Default()
	embedded, err := scheduler.Open(t.Context(), core, env, client, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer embedded.Close()
	router, err := buildRouter(core, env, runtimeServices{
		db: db, repo: atproto.NewRepoClient(client, core.PLCURL, logger),
		scheduling: embedded, context: t.Context(), logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	spec, err := os.Open("../../../../packages/spec/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer spec.Close()
	paths := regexp.MustCompile(`^  (/[^\s:]+):$`)
	methods := regexp.MustCompile(`^    (get|post|put|delete|patch):$`)
	scanner := bufio.NewScanner(spec)
	var path string
	var tested int
	for scanner.Scan() {
		line := scanner.Text()
		if route := paths.FindStringSubmatch(line); route != nil {
			path = route[1]
			continue
		}
		match := methods.FindStringSubmatch(line)
		if match == nil || !activeProtectedRoute(path) {
			continue
		}
		tested++
		method := strings.ToUpper(match[1])
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s returned %d to an anonymous caller", method, path, response.Code)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if tested < 40 {
		t.Fatalf("tested only %d standalone routes", tested)
	}
}

func activeProtectedRoute(path string) bool {
	for _, prefix := range []string{
		"/v1/appview/", "/v1/publications/", "/v1/media/", "/v1/lists", "/v1/sync/", "/v1/pds/cache/",
		"/v1/semble/", "/v1/subscription/", "/xrpc/at.skej.",
		"/xrpc/app.thesocialwire.appview.", "/xrpc/app.thesocialwire.articleDraft.",
		"/xrpc/app.thesocialwire.socialDraft.",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
