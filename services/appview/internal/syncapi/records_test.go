package syncapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestPreferencesCacheIsScopedAndCanBeRefreshed(t *testing.T) {
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	const alice = "did:plc:alice"
	const bob = "did:plc:bob"
	calls := map[string]int{}
	s := service{db: db, read: func(_ context.Context, did, _, _ string) (snapshot, error) {
		calls[did]++
		if did == bob {
			return snapshot{}, nil
		}
		return parseSnapshot([]byte(`{"cid":"fixture-cid","value":{"automaticallyMarkAsRead":true}}`), time.Time{})
	}}
	request := func(did, query, etag string, want int) map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/sync/preferences"+query, nil)
		if did != "" {
			req = req.WithContext(reqctx.WithAuth(req.Context(), &reqctx.Auth{DID: did}))
		}
		req.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		if err := s.preferences(w, req); err != nil {
			httpx.WriteError(w, err)
		}
		if w.Code != want {
			t.Fatalf("preferences %d want %d: %s", w.Code, want, w.Body.String())
		}
		result := map[string]json.RawMessage{}
		if want == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	request("", "", "", http.StatusUnauthorized)
	if result := request(alice, "", "", http.StatusOK); string(result["revision"]) != `"fixture-cid"` {
		t.Fatalf("unexpected preference revision %s", result["revision"])
	}
	request(alice, "", `"fixture-cid"`, http.StatusNotModified)
	if calls[alice] != 1 {
		t.Fatalf("conditional GET fetched upstream again: %d", calls[alice])
	}
	if result := request(bob, "", "", http.StatusOK); string(result["record"]) != "null" {
		t.Fatalf("unexpected record for other viewer %s", result["record"])
	}
	request(alice, "?fresh=true", "", http.StatusOK)
	if calls[alice] != 2 || calls[bob] != 1 {
		t.Fatalf("unexpected upstream reads %v", calls)
	}
}
