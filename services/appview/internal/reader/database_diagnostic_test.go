package reader

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	turso "turso.tech/database/tursogo-serverless"
)

func TestReaderDatabaseDiagnosticsNeverLogPrivateProviderDetails(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	logDatabaseFailure(context.Background(), "feed", &turso.HTTPError{StatusCode: 404, Message: "SELECT private_secret FROM https://private.example/token"})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["phase"] != "feed" || record["error_kind"] != "http" || record["error_code"] != "404" {
		t.Fatal("safe machine diagnostic missing")
	}
	if strings.Contains(output.String(), "private") || strings.Contains(output.String(), "SELECT") {
		t.Fatal("provider details leaked")
	}
	output.Reset()
	logDatabaseFailure(context.Background(), "read-state-status", &turso.Error{Code: "SELECT private_secret", Message: "private URL"})
	if strings.Contains(output.String(), "private") || !strings.Contains(output.String(), "UNKNOWN") {
		t.Fatal("invalid machine code leaked")
	}
}

func TestReadStateDatabaseFailureStillFailsClosedWithDiagnostic(t *testing.T) {
	db := testReaderDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := &service{db: db}
	called := false
	handler := s.requireReadState(func(http.ResponseWriter, *http.Request) error { called = true; return nil })
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.thesocialwire.appview.getFeed", nil)
	r = r.WithContext(reqctx.WithAuth(r.Context(), &reqctx.Auth{DID: testViewer}))
	w := httptest.NewRecorder()
	if err := handler(w, r); err != nil {
		t.Fatal(err)
	}
	if called || w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "ReadStateNotReady") {
		t.Fatal("read-state database failure no longer fails closed")
	}
	// Feed failures retain their existing client-facing status mapping.
	deadline := httptest.NewRecorder()
	if err := feedFailure(deadline, r, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	if deadline.Code != http.StatusGatewayTimeout {
		t.Fatal("feed deadline status changed")
	}
}
