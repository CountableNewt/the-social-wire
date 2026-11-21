package ingest

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/repo"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

type verificationTransport func(context.Context, *url.URL, string, int) (repo.HTTPResponse, error)

func (f verificationTransport) Get(ctx context.Context, u *url.URL, accept string, limit int) (repo.HTTPResponse, error) {
	return f(ctx, u, accept, limit)
}

func TestEntryDeletionRequiresStablePDSIdentity(t *testing.T) {
	const did = "did:plc:publisher"
	const entry = "at://" + did + "/site.standard.document/story"
	for _, test := range []struct {
		name, response string
		migrating      bool
		wantPresent    bool
	}{
		{"record missing", `{"error":"RecordNotFound"}`, false, false},
		{"unrelated 404", `{"error":"UnknownEndpoint"}`, false, true},
		{"PDS moved", `{"error":"RecordNotFound"}`, true, true},
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
			v := NewEntryVerifier(db, "https://plc.directory", time.Hour)
			if err := v.content.UpsertContentItem(ctx, thinstore.IndexedContentItem{
				URI: entry, CID: "indexed-cid", AuthorDID: did, Collection: "site.standard.document",
				CreatedAt: time.Now(), IndexedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
			identities := 0
			v.transport = verificationTransport(func(_ context.Context, u *url.URL, _ string, _ int) (repo.HTTPResponse, error) {
				switch u.Hostname() {
				case "plc.directory":
					identities++
					pds := "https://pds.publisher.dev"
					if test.migrating && identities == 2 {
						pds = "https://moved.publisher.dev"
					}
					return repo.HTTPResponse{Status: http.StatusOK, Body: []byte(`{"id":"` + did + `","service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"` + pds + `"}]}`)}, nil
				default:
					return repo.HTTPResponse{Status: http.StatusNotFound, Body: []byte(test.response)}, nil
				}
			})
			present, err := v.Verify(ctx, entry)
			if present != test.wantPresent || (!test.wantPresent && err != nil) {
				t.Fatalf("present=%v err=%v", present, err)
			}
			assertStored(t, ctx, v.content, entry, test.wantPresent)
		})
	}
}
