package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/repo"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

type providerFunc func(*http.Request) (*http.Response, error)

func (f providerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestMicrocosmInsertsVerifiedScopedRecordsOnly(t *testing.T) {
	const author = "did:plc:author"
	const collection = "site.standard.document"
	const site = "at://" + author + "/site.standard.publication/news"
	const uri = "at://" + author + "/" + collection + "/doc"
	scope := DiscoveryScope{Author: author, Keys: []string{site}}
	for _, test := range []struct {
		name, recordSite string
		want             bool
	}{
		{"matching", site, true},
		{"unrelated", "https://somewhere.example", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			db, err := sqlitedb.OpenMemory()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := thinstore.Migrate(ctx, db); err != nil {
				t.Fatal(err)
			}
			store := thinstore.SQLiteIngestStore{DB: db}
			provider := providerFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("DPoP") != "" {
					t.Fatal("credentials sent to public provider")
				}
				var body string
				switch r.URL.Hostname() {
				case "constellation.microcosm.blue":
					query := r.URL.Query()
					if query.Get("did") != author || query.Get("subject") != site {
						t.Fatalf("unexpected provider scope: %v", query)
					}
					body = `{"records":[]}`
					if query.Get("source") == collection+":site" {
						body = `{"records":[{"did":"` + author + `","collection":"` + collection + `","rkey":"doc"}]}`
					}
				case "slingshot.microcosm.blue":
					cid := repo.HashCID(repo.EncodeDAGCBOR(repo.MapValue(map[string]repo.Value{
						"$type": repo.StringValue(collection), "title": repo.StringValue("Story"), "site": repo.StringValue(test.recordSite),
					})))
					data, err := json.Marshal(map[string]any{"uri": uri, "cid": cid.String(), "value": map[string]string{
						"$type": collection, "title": "Story", "site": test.recordSite,
					}})
					if err != nil {
						t.Fatal(err)
					}
					body = string(data)
				default:
					t.Fatalf("unexpected provider %s", r.URL.Hostname())
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			service := NewMicrocosm(provider, store)
			count, err := service.discover(ctx, scope)
			if (count == 1) != test.want {
				t.Fatalf("unexpected indexed count %d", count)
			}
			if (test.want && err != nil) || (!test.want && !errors.Is(err, errDiscoveryInvalid)) {
				t.Fatalf("unexpected verification result: %v", err)
			}
			assertStored(t, ctx, store, uri, test.want)
		})
	}
}

func assertStored(t *testing.T, ctx context.Context, store thinstore.SQLiteIngestStore, uri string, want bool) {
	t.Helper()
	have, err := store.HasContentItem(ctx, uri)
	if err != nil || have != want {
		t.Fatalf("content stored=%v want=%v error=%v", have, want, err)
	}
}
