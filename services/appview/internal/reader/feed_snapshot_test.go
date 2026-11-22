package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func seedFeedSnapshot(t *testing.T, db *sqlitedb.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO appview_viewer_feeds VALUES('did:plc:viewer','subscribed','','2026-11-21T00:00:00Z')`,
		`INSERT INTO appview_feed_publications VALUES('did:plc:viewer','subscribed','','publication')`,
		`INSERT INTO appview_publication_scopes VALUES('did:plc:viewer','publication','did:plc:author','at://did:plc:author/site.standard.publication/news','[]','[]','["one"]','["subscribed"]','2026-11-21T00:00:00Z')`,
	} {
		if _, err := db.Writer().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFeedSnapshotMatchesIndependentScopeReadAndUsesOneQuery(t *testing.T) {
	db := testReaderDB(t)
	seedFeedSnapshot(t, db)
	seedItems(t, db)
	err := db.Read(t.Context(), func(tx *sql.Tx) error {
		old, err := scopes(t.Context(), tx, testViewer, "subscribed", "")
		if err != nil {
			return err
		}
		counter := &readerQueryCounter{Tx: tx}
		updated, got, migrated, err := feedSnapshot(t.Context(), counter, testViewer, "subscribed", "")
		if err != nil {
			return err
		}
		if updated != "2026-11-21T00:00:00Z" || migrated || !reflect.DeepEqual(got, old) || counter.count != 1 {
			t.Fatalf("snapshot mismatch: updated=%s migrated=%v scopes=%+v queries=%d", updated, migrated, got, counter.count)
		}
		page, err := listEntriesWithAuthority(t.Context(), counter, testViewer, got, thinstore.FilterAll, nil, 50, true, migrated)
		if err != nil {
			return err
		}
		if len(page.Entries) != 2 || counter.count != 3 {
			t.Fatalf("full feed bundle: entries=%d queries=%d", len(page.Entries), counter.count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFeedSnapshotPreservesMissingEmptyAndAuthorityFences(t *testing.T) {
	db := testReaderDB(t)
	seedFeedSnapshot(t, db)
	if _, _, _, err := feedSnapshot(t.Context(), db.Reader(), "missing", "subscribed", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing feed: %v", err)
	}
	if _, err := db.Writer().Exec(`INSERT INTO appview_pds_read_state_authority(viewer_did,manifest_cid,projection_ready) VALUES(?, 'manifest',0) ON CONFLICT(viewer_did) DO UPDATE SET manifest_cid='manifest',projection_ready=0`, testViewer); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := feedSnapshot(t.Context(), db.Reader(), testViewer, "subscribed", ""); err == nil {
		t.Fatal("stale migrated authority accepted")
	}
	if _, err := db.Writer().Exec(`UPDATE appview_pds_read_state_authority SET projection_ready=1 WHERE viewer_did=?`, testViewer); err != nil {
		t.Fatal(err)
	}
	if _, got, migrated, err := feedSnapshot(t.Context(), db.Reader(), testViewer, "subscribed", ""); err != nil || len(got) != 1 || !migrated {
		t.Fatalf("ready migrated authority: scopes=%d migrated=%v err=%v", len(got), migrated, err)
	}
	if _, err := db.Writer().Exec(`UPDATE appview_pds_read_state_authority SET projection_ready=0 WHERE viewer_did=?`, testViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec(`DELETE FROM appview_feed_publications WHERE viewer_did=?`, testViewer); err != nil {
		t.Fatal(err)
	}
	_, got, migrated, err := feedSnapshot(t.Context(), db.Reader(), testViewer, "subscribed", "")
	if err != nil || len(got) != 0 || !migrated {
		t.Fatalf("empty feed changed: scopes=%d migrated=%v err=%v", len(got), migrated, err)
	}
}

func TestGetFeedSnapshotHTTPPaginationAndUnavailableStates(t *testing.T) {
	db := testReaderDB(t)
	seedFeedSnapshot(t, db)
	ids := seedItems(t, db)
	router := httpx.NewRouter()
	Register(router.Group(), db, publications.New(db, nil), nil, Options{})
	get := func(path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r = r.WithContext(reqctx.WithAuth(context.Background(), &reqctx.Auth{DID: testViewer}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("got %d %s want %d", w.Code, w.Body.String(), status)
		}
		return w
	}
	path := "/xrpc/app.thesocialwire.appview.getFeed?kind=subscribed&limit=1"
	w := get(path, http.StatusOK)
	var page thinstore.EntryListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].EntryID != ids[2] || page.Cursor == nil {
		t.Fatalf("first page: %+v", page)
	}
	if w.Header().Get("X-AppView-Membership-Updated-At") != "2026-11-21T00:00:00Z" {
		t.Fatal("membership timestamp changed")
	}
	w = get(path+"&cursor="+url.QueryEscape(*page.Cursor), http.StatusOK)
	page = thinstore.EntryListResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].EntryID != ids[1] || page.Cursor != nil {
		t.Fatalf("second page: %+v", page)
	}
	get("/xrpc/app.thesocialwire.appview.getFeed?kind=following", http.StatusNotFound)
	if _, err := db.Writer().Exec(`DELETE FROM appview_viewer_feeds`); err != nil {
		t.Fatal(err)
	}
	get(path, http.StatusServiceUnavailable)
}
