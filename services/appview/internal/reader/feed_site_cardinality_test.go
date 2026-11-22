package reader

import (
	"reflect"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestKnownSiteCardinalityPreservesAllFiltersAndPagination(t *testing.T) {
	db := testReaderDB(t)
	ids := seedItems(t, db)
	if _, err := db.Writer().Exec("INSERT INTO read_marks VALUES(?,?,?)", testViewer, ids[2], "2026-11-21T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec(`INSERT INTO appview_publication_read_floors(viewer_did,publication_id,read_floor_at,generation,updated_at,read_floor_at_us)
	 SELECT ?,'news',created_at,1,'2026-11-21T00:00:00Z',created_at_us FROM content_items WHERE uri=?`, testViewer, ids[1]); err != nil {
		t.Fatal(err)
	}
	other := "at://did:plc:author/site.standard.publication/other"
	cases := map[string][]feedMatch{
		"single-site only": {{AuthorDID: testAuthor, PublicationID: "news", Sites: []string{testPub}}, {AuthorDID: testAuthor, PublicationID: "other", Sites: []string{other}}},
		"non-single only":  {{AuthorDID: testAuthor, PublicationID: "both", Sites: []string{testPub, other}}, {AuthorDID: testAuthor, PublicationID: "unscoped"}, {AuthorDID: testAuthor, PublicationID: "empty", Sites: []string{}}},
		"mixed":            {{AuthorDID: testAuthor, PublicationID: "news", Sites: []string{testPub}}, {AuthorDID: testAuthor, PublicationID: "both", Sites: []string{testPub, other}}, {AuthorDID: testAuthor, PublicationID: "unscoped"}},
	}
	for name, scopes := range cases {
		t.Run(name, func(t *testing.T) {
			for _, migrated := range []bool{false, true} {
				for _, filter := range []thinstore.EntryListFilter{thinstore.FilterAll, thinstore.FilterRead, thinstore.FilterUnread} {
					var cursor *string
					for page := 0; page < 4; page++ {
						now := thinstore.CursorTimestamp(time.Now())
						query, args := jsonEntryBatch(scopes, migrated, testViewer, now, filter, cursor, 2)
						rows, err := db.Reader().QueryContext(t.Context(), query, args...)
						if err != nil {
							t.Fatal(err)
						}
						actual, next, count, err := readEntries(rows, true)
						if err != nil {
							t.Fatal(err)
						}
						oracle, oracleArgs := unboundedFeedBatch(scopes, migrated, now, filter, cursor, 2)
						rows, err = db.Reader().QueryContext(t.Context(), oracle, oracleArgs...)
						if err != nil {
							t.Fatal(err)
						}
						expected, expectedNext, expectedCount, err := readEntries(rows, true)
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(actual, expected) || !reflect.DeepEqual(next, expectedNext) || count != expectedCount {
							t.Fatalf("migrated=%t filter=%s page=%d changed scoped rows/cursor/read flags", migrated, filter, page)
						}
						cursor = next
						if count < 2 {
							break
						}
					}
				}
			}
		})
	}
	// This guards the measured remote Describe cost: homogeneous input must not
	// retain both correlated plans merely because mixed input can need them.
	single, _ := jsonEntryBatch(cases["single-site only"], false, testViewer, "now", thinstore.FilterUnread, nil, 2)
	general, _ := jsonEntryBatch(cases["non-single only"], false, testViewer, "now", thinstore.FilterUnread, nil, 2)
	mixed, _ := jsonEntryBatch(cases["mixed"], false, testViewer, "now", thinstore.FilterUnread, nil, 2)
	if len(single)*4 > len(mixed)*3 || len(general)*4 > len(mixed)*3 {
		t.Fatal("homogeneous feeds still pay to describe an unreachable correlated plan")
	}
}
