package reader

import (
	"reflect"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestAllFeedComputesReadStateAfterSelectionWithoutChangingAuthority(t *testing.T) {
	db := testReaderDB(t)
	ids := seedItems(t, db)
	if _, err := db.Writer().Exec(`INSERT INTO read_marks VALUES(?,?,?)`, testViewer, ids[2], "2026-11-21T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec(`INSERT INTO appview_publication_read_floors(viewer_did,publication_id,read_floor_at,generation,updated_at,read_floor_at_us)
	 SELECT ?,'publication',created_at,1,'2026-11-21T00:00:00Z',created_at_us FROM content_items WHERE uri=?`, testViewer, ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec(`INSERT INTO appview_unread_overrides(viewer_did,subject_uri,created_at,created_at_us)
	 VALUES(?,?,'2026-11-21T00:00:00Z',1795219200000000)`, testViewer, ids[0]); err != nil {
		t.Fatal(err)
	}
	scopes := []feedMatch{{AuthorDID: testAuthor, PublicationID: "publication"}}
	for _, migrated := range []bool{false, true} {
		now := thinstore.CursorTimestamp(time.Now())
		query, args := jsonEntryBatch(scopes, migrated, testViewer, now, thinstore.FilterAll, nil, 3)
		rows, err := db.Reader().QueryContext(t.Context(), query, args...)
		if err != nil {
			t.Fatal(err)
		}
		actual, next, count, err := readEntries(rows, true)
		if err != nil {
			t.Fatal(err)
		}
		if count != 3 || len(actual) != 3 || actual[0].EntryID != ids[2] || !actual[0].IsRead || actual[1].EntryID != ids[1] || actual[1].IsRead == migrated || actual[2].EntryID != ids[0] || actual[2].IsRead {
			t.Fatalf("read marks, legacy floor or unread override changed for migrated=%t", migrated)
		}
		oracle, oracleArgs := unboundedFeedBatch(scopes, migrated, now, thinstore.FilterAll, nil, 3)
		rows, err = db.Reader().QueryContext(t.Context(), oracle, oracleArgs...)
		if err != nil {
			t.Fatal(err)
		}
		expected, expectedNext, expectedCount, err := readEntries(rows, true)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actual, expected) || !reflect.DeepEqual(next, expectedNext) || count != expectedCount {
			t.Fatal("deferred read-state joins differ from independent unbounded query")
		}
	}
}
