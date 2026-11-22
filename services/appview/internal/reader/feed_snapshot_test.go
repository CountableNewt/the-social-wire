package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

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
		updated, got, matched, migrated, err := feedSnapshotWithMatches(t.Context(), counter, testViewer, "subscribed", "")
		if err != nil {
			return err
		}
		if updated != "2026-11-21T00:00:00Z" || migrated || !reflect.DeepEqual(got, old) || counter.count != 1 {
			t.Fatalf("snapshot mismatch: updated=%s migrated=%v scopes=%+v queries=%d", updated, migrated, got, counter.count)
		}
		page, err := listMatchedEntries(t.Context(), counter, testViewer, matched, thinstore.FilterAll, nil, 50, true, migrated)
		if err != nil {
			return err
		}
		if len(page.Entries) != 2 || counter.count != 2 {
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

func TestCombinedFeedSnapshotPreservesAliasMatchingFiltersAndPagination(t *testing.T) {
	db := testReaderDB(t)
	seedFeedSnapshot(t, db)
	ids := seedItems(t, db)
	for _, statement := range []string{
		`UPDATE content_items SET publication_site='https://example.com/News' WHERE uri LIKE '%/0'`,
		`UPDATE content_items SET publication_site=NULL WHERE uri LIKE '%/2'`,
		`UPDATE appview_publication_scopes SET publication_at_uri='at://did:plc:AUTHOR/site.standard.publication/news' WHERE publication_id='publication'`,
		`INSERT INTO appview_publication_scopes VALUES('did:plc:viewer','website','did:plc:author',NULL,'[]','["https://example.com/News/#section"]','[]','["subscribed"]','2026-11-21T00:00:00Z')`,
		`INSERT INTO appview_publication_scopes VALUES('did:plc:viewer','unscoped','did:plc:author',NULL,'[]','[]','[]','["subscribed"]','2026-11-21T00:00:00Z')`,
		`INSERT INTO appview_publication_scopes VALUES('did:plc:viewer','no-match','did:plc:author',NULL,'[]','["https://unmatched.example"]','[]','["subscribed"]','2026-11-21T00:00:00Z')`,
		`INSERT INTO appview_feed_publications VALUES('did:plc:viewer','subscribed','','website'),('did:plc:viewer','subscribed','','unscoped'),('did:plc:viewer','subscribed','','no-match')`,
		`INSERT INTO appview_publication_scopes VALUES('did:plc:viewer','outside-feed','did:plc:outsider',NULL,'[]','["https://outside.example"]','[]','["following"]','2026-11-21T00:00:00Z')`,
		`INSERT INTO appview_viewer_feeds VALUES('did:plc:viewer','following','','2026-11-21T00:00:00Z')`,
		`INSERT INTO appview_feed_publications VALUES('did:plc:viewer','following','','outside-feed')`,
	} {
		if _, err := db.Writer().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Writer().Exec(`INSERT INTO read_marks VALUES(?,?,?)`, testViewer, ids[1], "2026-11-21T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []thinstore.EntryListFilter{thinstore.FilterAll, thinstore.FilterRead, thinstore.FilterUnread} {
		var cursor *string
		for pageIndex := range 6 {
			err := db.Read(t.Context(), func(tx *sql.Tx) error {
				oldScopes, err := scopes(t.Context(), tx, testViewer, "subscribed", "")
				if err != nil {
					return err
				}
				expected, err := listEntriesWithAuthority(t.Context(), tx, testViewer, oldScopes, filter, cursor, 1, true, false)
				if err != nil {
					return err
				}
				counter := &readerQueryCounter{Tx: tx}
				_, gotScopes, matched, migrated, err := feedSnapshotWithMatches(t.Context(), counter, testViewer, "subscribed", "")
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(gotScopes, oldScopes) {
					t.Fatal("combined query changed publication scopes")
				}
				if len(matched) != 3 {
					t.Fatalf("alias/unscoped/no-match selection changed: matches=%d", len(matched))
				}
				for _, match := range matched {
					if match.PublicationID == "outside-feed" || match.PublicationID == "no-match" {
						t.Fatal("unrelated publication leaked into feed")
					}
				}
				actual, err := listMatchedEntries(t.Context(), counter, testViewer, matched, filter, cursor, 1, true, migrated)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("filter=%s page=%d changed entries/cursor", filter, pageIndex)
				}
				if counter.count != 2 {
					t.Fatalf("combined feed required %d data queries", counter.count)
				}
				cursor = actual.Cursor
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if cursor == nil {
				break
			}
		}
	}
}

func TestFeedSnapshotDeduplicatesSharedAuthorsWithoutLosingPublicationAliases(t *testing.T) {
	db := testReaderDB(t)
	const publications = 80
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	err := db.Write(t.Context(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO appview_viewer_feeds VALUES(?,'subscribed','','2026-11-21T00:00:00Z')`, testViewer); err != nil {
			return err
		}
		for i := range publications {
			publication := fmt.Sprintf("publication-%03d", i)
			atURI := fmt.Sprintf("at://%s/site.standard.publication/p%d", testAuthor, i)
			siteURL := fmt.Sprintf("https://example.com/publication/%d", i)
			sites, _ := json.Marshal([]string{siteURL + "/"})
			if _, err := tx.Exec(`INSERT INTO appview_publication_scopes VALUES(?,?,?,?, '[]',?,'[]','["subscribed"]','2026-11-21T00:00:00Z')`, testViewer, publication, testAuthor, atURI, string(sites)); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO appview_feed_publications VALUES(?,'subscribed','',?)`, testViewer, publication); err != nil {
				return err
			}
			rawSite := fmt.Sprintf("at://did:plc:AUTHOR/site.standard.publication/p%d", i)
			if i%2 == 0 {
				rawSite = siteURL
			}
			for j := range 3 {
				uri := fmt.Sprintf("at://%s/site.standard.document/shared-%d-%d", testAuthor, i, j)
				at := base.Add(time.Duration(i*3+j) * time.Second)
				stamp := thinstore.CursorTimestamp(at)
				render, _ := json.Marshal(map[string]string{"title": "Fixture", "publishedAt": stamp})
				if _, err := tx.Exec(`INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at,created_at_us,indexed_at_us)
				 VALUES(?,'cid',?,'site.standard.document',?,?,?,?,'2099-01-01T00:00:00Z',?,?)`, uri, testAuthor, stamp, stamp, rawSite, string(render), sqlitedb.TimeMicroseconds(at), sqlitedb.TimeMicroseconds(at)); err != nil {
					return err
				}
				if j == 0 {
					if _, err := tx.Exec("INSERT INTO read_marks VALUES(?,?,?)", testViewer, uri, stamp); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []thinstore.EntryListFilter{thinstore.FilterAll, thinstore.FilterRead, thinstore.FilterUnread} {
		var cursor *string
		for pageIndex := range 3 {
			err := db.Read(t.Context(), func(tx *sql.Tx) error {
				oldScopes, err := scopes(t.Context(), tx, testViewer, "subscribed", "")
				if err != nil {
					return err
				}
				expected, err := listEntriesWithAuthority(t.Context(), tx, testViewer, oldScopes, filter, cursor, 7, true, false)
				if err != nil {
					return err
				}
				_, gotScopes, matched, migrated, err := feedSnapshotWithMatches(t.Context(), tx, testViewer, "subscribed", "")
				if err != nil {
					return err
				}
				if len(matched) != publications || !reflect.DeepEqual(gotScopes, oldScopes) {
					t.Fatal("shared-author deduplication lost publication identities or aliases")
				}
				actual, err := listMatchedEntries(t.Context(), tx, testViewer, matched, filter, cursor, 7, true, migrated)
				if err != nil {
					return err
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("shared-author filter=%s page=%d changed entries or cursor", filter, pageIndex)
				}
				cursor = actual.Cursor
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
