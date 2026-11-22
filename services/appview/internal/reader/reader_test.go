package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

const (
	testViewer = "did:plc:viewer"
	testAuthor = "did:plc:author"
	testPub    = "at://did:plc:author/site.standard.publication/news"
)

func testReaderDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, migrate := range []func(context.Context, *sqlitedb.DB) error{thinstore.Migrate, publications.Migrate, Migrate, Migrate} {
		if err := migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func seedItems(t *testing.T, db *sqlitedb.DB) []string {
	t.Helper()
	ids := []string{}
	created := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	for i := range 3 {
		id := fmt.Sprintf("at://%s/site.standard.document/%d", testAuthor, i)
		ids = append(ids, id)
		date := thinstore.CursorTimestamp(created.Add(time.Duration(i) * time.Second))
		render, _ := json.Marshal(map[string]string{
			"title": fmt.Sprintf("Article %d", i), "publishedAt": date, "summary": "Summary", "contentHtml": "<p>Body</p>",
		})
		site := testPub
		if i == 0 {
			site = "at://did:plc:author/site.standard.publication/other"
		}
		_, err := db.Writer().Exec(`INSERT INTO content_items
			(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at,created_at_us,indexed_at_us)
			VALUES(?, 'cid', ?, 'site.standard.document', ?, ?, ?, ?, ?, ?, ?)`, id, testAuthor, date, date, site, string(render),
			thinstore.CursorTimestamp(time.Now().Add(time.Hour)), sqlitedb.TimeMicroseconds(created.Add(time.Duration(i)*time.Second)), sqlitedb.TimeMicroseconds(created.Add(time.Duration(i)*time.Second)))
		if err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func TestReaderRoutesScopePaginationAndReadMarks(t *testing.T) {
	db := testReaderDB(t)
	ids := seedItems(t, db)
	router := httpx.NewRouter()
	Register(router.Group(), db, publications.New(db, nil), nil, Options{})
	request := func(method, path, body string, wantStatus int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(reqctx.WithAuth(r.Context(), &reqctx.Auth{DID: testViewer}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != wantStatus {
			t.Fatalf("%s %s: got %d %s, want %d", method, path, w.Code, w.Body.String(), wantStatus)
		}
		return w
	}
	path := "/xrpc/app.thesocialwire.appview.listEntries?authorDid=" + testAuthor + "&publicationAtUri=" + testPub + "&limit=1"
	w := request(http.MethodGet, path, "", http.StatusOK)
	var page thinstore.EntryListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].EntryID != ids[2] || page.Cursor == nil {
		t.Fatalf("first page: %+v", page)
	}
	w = request(http.MethodGet, path+"&cursor="+*page.Cursor, "", http.StatusOK)
	page = thinstore.EntryListResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].EntryID != ids[1] || page.Cursor != nil {
		t.Fatalf("second page: %+v", page)
	}
	payload := fmt.Sprintf(`{"subjectUri":%q}`, ids[2])
	for range 2 {
		w = request(http.MethodPost, "/xrpc/app.thesocialwire.appview.putReadMark", payload, http.StatusOK)
		if w.Body.String() != "{}" {
			t.Fatalf("expected empty object, got %s", w.Body.String())
		}
	}
	var revision int
	if err := db.Reader().QueryRow("SELECT legacy_revision FROM appview_pds_read_state_authority WHERE viewer_did=?", testViewer).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("idempotent retry advanced revision: revision=%d err=%v", revision, err)
	}
	w = request(http.MethodGet, "/v1/appview/entry?entryId="+ids[2], "", http.StatusOK)
	if !strings.Contains(w.Body.String(), `"isRead":true`) || !strings.Contains(w.Body.String(), `"contentHtml":"<p>Body</p>"`) {
		t.Fatalf("detail response: %s", w.Body.String())
	}
	w = request(http.MethodGet, "/v1/appview/unread-counts?authorDid="+testAuthor+"&publicationAtUri="+testPub, "", http.StatusOK)
	if !strings.Contains(w.Body.String(), fmt.Sprintf("%q:1", testPub)) {
		t.Fatalf("unread response: %s", w.Body.String())
	}
	request(http.MethodDelete, "/v1/appview/read-marks", payload, http.StatusOK)
	w = request(http.MethodGet, "/v1/appview/entry?entryId="+ids[2], "", http.StatusOK)
	if !strings.Contains(w.Body.String(), `"isRead":false`) {
		t.Fatalf("detail response: %s", w.Body.String())
	}
}

func TestMarkAllUsesNewestScopedBoundaryAndFencesMigratedViewers(t *testing.T) {
	db := testReaderDB(t)
	ids := seedItems(t, db)
	pub := testPub
	ctx := context.Background()
	scope := thinstore.NewPublicationScope(testViewer, pub, testAuthor, &pub, nil, nil, nil, time.Now())
	result, err := markAll(ctx, db, testViewer, []thinstore.PublicationScope{scope})
	if err != nil {
		t.Fatal(err)
	}
	if result.Marked != 2 || result.UnreadCounts[pub] != 0 || len(result.Boundaries) != 1 ||
		result.Boundaries[0].EntryID == nil || *result.Boundaries[0].EntryID != ids[2] {
		t.Fatalf("mark all response: %+v", result)
	}
	if _, err := db.Writer().Exec("UPDATE appview_pds_read_state_authority SET manifest_cid='migrated' WHERE viewer_did=?", testViewer); err != nil {
		t.Fatal(err)
	}
	err = changeReadMark(ctx, db, testViewer, ids[1], time.Now(), true)
	if httpx.StatusOf(err) != http.StatusConflict {
		t.Fatalf("legacy writer accepted or misclassified: %v", err)
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO read_marks(viewer_did,subject_uri,created_at) VALUES(?,?,?)", testViewer, ids[1], thinstore.CursorTimestamp(time.Now()))
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "PDSReadStateAlreadyMigrated") {
		t.Fatalf("SQLite fence missing: %v", err)
	}
}

func TestReadAgeCalendarCutoffsAcrossDST(t *testing.T) {
	zone, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 3, 10, 0, 0, 0, 0, zone)
	var histogram [8]int
	histogram[1], histogram[3] = 2, 4
	result := ageOptions(today, histogram)
	if result.ReferenceDay != "2026-03-10T05:00:00.000Z" || len(result.Options) != 2 ||
		result.Options[0].Count != 6 || result.Options[1].Count != 4 || result.Options[1].Before != "2026-03-08T06:00:00.000Z" {
		t.Fatalf("unexpected read-age options: %+v", result)
	}
}
