package reader

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestListRenderProjectionPreservesValidationAndStoredDetail(t *testing.T) {
	db := testReaderDB(t)
	ids := seedItems(t, db)
	if _, err := db.Writer().Exec("DELETE FROM content_items WHERE uri<>?", ids[1]); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"title":"Article","publishedAt":"2026-11-21T00:00:00Z","contentHtml":"` + strings.Repeat("article HTML ", 5000) + `","summary":"Summary","articleUrl":"https://example.com/article","contentLabels":["news"]}`,
		`{"title":"Article","contentHtml":null}`,
		`{"title":"Article"}`,
		`{"title":"Article","contentHtml":true}`,
		`{"title":"Article","contentHtml":42}`,
		`{"title":"Article","contentHtml":[]}`,
		`{"title":"Article","contentHtml":{}}`,
		`{"title":"Article","contentHtml":"first","contentHtml":true}`,
		`{"title":"Article","contentHtml":true,"contentHtml":"last"}`,
		`{"title":"Article","contentHtml":"first","contentHtml":"last"}`,
		`{"title":"Article","contentHtml":"first","ContentHtml":true}`,
		`{"title":"Article","contentHtml":`,
	} {
		if !strings.Contains(raw, `"publishedAt"`) {
			raw = strings.Replace(raw, `{"title":`, `{"publishedAt":"2026-11-21T00:00:00Z","title":`, 1)
		}
		if _, err := db.Writer().Exec("UPDATE content_items SET render_json=?", raw); err != nil {
			t.Fatal(err)
		}
		var projected string
		if err := db.Reader().QueryRow("SELECT " + listRenderSQL + " FROM content_items ci").Scan(&projected); err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		expected, _, expectedOK := makeEntry(ids[1], raw, at, false)
		actual, _, actualOK := makeEntry(ids[1], projected, at, false)
		if actualOK != expectedOK || !reflect.DeepEqual(actual, expected) {
			t.Fatal("list projection changed entry fields or invalid-render admission")
		}
		if len(raw) > 10000 && len(projected) >= len(raw)/10 {
			t.Fatal("list projection still carries article HTML")
		}
		var stored string
		if err := db.Reader().QueryRow("SELECT render_json FROM content_items").Scan(&stored); err != nil || stored != raw {
			t.Fatal("list query changed stored article detail")
		}
	}
}

func TestFeedPageContinuesPastDuplicateURLsAndInvalidRenders(t *testing.T) {
	db := testReaderDB(t)
	base := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	ids := make([]string, 12)
	for i := range ids {
		ids[i] = fmt.Sprintf("at://%s/site.standard.document/dedup-%d", testAuthor, i)
		original := fmt.Sprintf("https://example.com/article/%d", i)
		if i >= 8 {
			original = "https://example.com/shared"
		}
		at := thinstore.CursorTimestamp(base.Add(time.Duration(i) * time.Second))
		render, _ := json.Marshal(map[string]string{"title": "Article", "publishedAt": at, "articleUrl": original, "contentHtml": "Full detail"})
		if i == 6 {
			render = []byte(`{"title":`)
		}
		if i == 7 {
			render = []byte(`{"title":"Invalid","publishedAt":"2026-11-21T00:00:00Z","contentHtml":true}`)
		}
		if _, err := db.Writer().Exec(`INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at,created_at_us,indexed_at_us)
		 VALUES(?,'cid',?,'site.standard.document',?,?,'publication',?,'2099-01-01T00:00:00Z',?,?)`, ids[i], testAuthor, at, at, string(render), sqlitedb.TimeMicroseconds(base.Add(time.Duration(i)*time.Second)), sqlitedb.TimeMicroseconds(base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	var cursor *string
	for pageIndex, want := range [][]string{{ids[11], ids[5]}, {ids[4], ids[3]}, {ids[2], ids[1]}, {ids[0]}} {
		err := db.Read(context.Background(), func(tx *sql.Tx) error {
			counter := &readerQueryCounter{Tx: tx}
			page, err := listMatchedEntries(t.Context(), counter, testViewer, []feedMatch{{AuthorDID: testAuthor, PublicationID: "publication"}}, thinstore.FilterAll, cursor, 2, true, false)
			if err != nil {
				return err
			}
			var got []string
			for _, entry := range page.Entries {
				got = append(got, entry.EntryID)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("page=%d changed unique valid rows: got %v want %v", pageIndex, got, want)
			}
			if pageIndex == 0 && counter.count < 2 {
				t.Fatal("fixture did not exercise continuation past a full unusable batch")
			}
			cursor = page.Cursor
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if cursor != nil {
		t.Fatal("last page unexpectedly has a cursor")
	}
}
