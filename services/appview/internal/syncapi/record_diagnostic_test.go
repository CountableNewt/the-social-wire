package syncapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	turso "turso.tech/database/tursogo-serverless"
)

func TestRecordDiagnosticsNeverExposePrivateProviderDetails(t *testing.T) {
	var out bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(prior) })
	for _, err := range []error{&turso.HTTPError{StatusCode: 503, Message: "SELECT private_secret https://private.example/token did:plc:private"}, &turso.Error{Code: "SELECT private_secret", Message: "private preferences"}} {
		out.Reset()
		logRecordFailure(context.Background(), "cache-upsert", err)
		var record map[string]any
		if json.Unmarshal(out.Bytes(), &record) != nil {
			t.Fatal("invalid diagnostic")
		}
		if record["phase"] != "cache-upsert" || record["error_kind"] == nil || record["error_code"] == nil {
			t.Fatal("missing safe diagnostic")
		}
		for _, secret := range []string{"private", "SELECT", "did:plc:", "https://"} {
			if strings.Contains(out.String(), secret) {
				t.Fatal("private diagnostic value leaked")
			}
		}
		if len(record) != 6 {
			t.Fatal("unexpected diagnostic attributes")
		}
	}
}

func TestRecordFailurePhasesPreserveStatusAndDoNotLogCacheMisses(t *testing.T) {
	for _, phase := range []string{"cache-read", "public-read", "cache-delete", "cache-upsert", "cache-miss"} {
		t.Run(phase, func(t *testing.T) {
			var out bytes.Buffer
			prior := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
			defer slog.SetDefault(prior)
			db, err := sqlitedb.OpenMemory()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := Migrate(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			s := service{db: db, read: func(context.Context, string, string, string) (snapshot, error) {
				if phase == "public-read" {
					return snapshot{}, httpx.Status(http.StatusBadGateway)
				}
				if phase == "cache-upsert" {
					return parseSnapshot([]byte(`{"cid":"fixture","value":{"private":"private-payload"}}`), time.Time{})
				}
				return snapshot{}, nil
			}}
			force := phase == "cache-delete" || phase == "cache-upsert"
			if phase == "cache-read" || force {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest(http.MethodGet, "/v1/sync/preferences?private=query", nil)
			req = req.WithContext(reqctx.WithAuth(req.Context(), &reqctx.Auth{DID: "did:plc:private-viewer"}))
			_, err = s.get(req, "app.thesocialwire.preferences", "private-rkey", force, time.Minute, time.Hour)
			if phase == "cache-miss" {
				if err != nil || out.Len() != 0 {
					t.Fatal("normal cache miss logged as failure")
				}
				return
			}
			want := http.StatusInternalServerError
			if phase == "public-read" {
				want = http.StatusBadGateway
			}
			if err == nil || httpx.StatusOf(err) != want {
				t.Fatal("diagnostic changed response status")
			}
			var record map[string]any
			if json.Unmarshal(out.Bytes(), &record) != nil || record["phase"] != phase {
				t.Fatal("incorrect failure phase")
			}
			if strings.Contains(out.String(), "private-") || strings.Contains(out.String(), "preferences") || strings.Contains(out.String(), "did:plc:") {
				t.Fatal("request data leaked")
			}
		})
	}
}
