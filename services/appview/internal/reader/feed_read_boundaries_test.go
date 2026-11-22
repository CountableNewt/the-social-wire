package reader

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestUnreadFilteringPreservesRoundedBoundariesAndPublicationOverrides(t *testing.T) {
	for _, floor := range []string{"2026-11-22T12:00:00.000001100Z", "1970-01-01T00:00:00.000000100Z"} {
		t.Run(floor, func(t *testing.T) { checkUnreadBoundaryFixture(t, floor) })
	}
}

func checkUnreadBoundaryFixture(t *testing.T, floorRaw string) {
	t.Helper()
	db := testReaderDB(t)
	floorAt, err := time.Parse(time.RFC3339Nano, floorRaw)
	if err != nil {
		t.Fatal(err)
	}
	floorUS := sqlitedb.TimeMicroseconds(floorAt)
	center := time.UnixMicro(floorUS).UTC()
	uri := func(key string) string { return "at://" + testAuthor + "/site.standard.document/" + key }
	for _, row := range []struct {
		key, site string
		at        time.Time
		expired   bool
	}{
		{"z-critical", "inside", center.Add(-100 * time.Nanosecond), false},
		{"a-floor-tie", "inside", center.Add(400 * time.Nanosecond), false},
		{"a-new-micro", "inside", center.Add(500 * time.Nanosecond), false},
		{"z-offset", "inside", center.In(time.FixedZone("offset", -5*60*60)), false},
		{"old-override", "inside", center.Add(-2 * time.Hour), false},
		{"outside-override", "outside", center.Add(-3 * time.Hour), false},
		{"marked", "inside", center.Add(time.Minute), false},
		{"expired", "inside", center.Add(2 * time.Minute), true},
	} {
		stamp := row.at.Format(time.RFC3339Nano)
		render, _ := json.Marshal(map[string]string{"title": "Fixture", "publishedAt": stamp})
		expires := "2099-01-01T00:00:00Z"
		if row.expired {
			expires = "1900-01-01T00:00:00Z"
		}
		if _, err := db.Writer().Exec(`INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at,created_at_us,indexed_at_us)
		 VALUES(?,'cid',?,'site.standard.document',?,?,?,?,?,?,?)`, uri(row.key), testAuthor, stamp, stamp, row.site, string(render), expires, sqlitedb.TimeMicroseconds(row.at), sqlitedb.TimeMicroseconds(row.at)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Writer().Exec(`INSERT INTO appview_publication_read_floors(viewer_did,publication_id,read_floor_at,read_floor_uri,generation,updated_at,read_floor_at_us)
	 VALUES(?,'publication',?,?,1,?,?)`, testViewer, floorRaw, uri("m"), floorRaw, floorUS); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer().Exec("INSERT INTO read_marks VALUES(?,?,?)", testViewer, uri("marked"), floorRaw); err != nil {
		t.Fatal(err)
	}
	scope := feedMatch{AuthorDID: testAuthor, PublicationID: "publication", Sites: []string{"inside"}}
	addOverride := func(key string) {
		t.Helper()
		if _, err := db.Writer().Exec("INSERT INTO appview_unread_overrides(viewer_did,subject_uri,created_at,created_at_us) VALUES(?,?,?,?)", testViewer, uri(key), floorRaw, floorUS); err != nil {
			t.Fatal(err)
		}
	}
	check := func(phase string, oldOverride bool) {
		t.Helper()
		for _, migrated := range []bool{false, true} {
			var cursor *string
			seen := map[string]bool{}
			for page := 0; page < 10; page++ {
				now := thinstore.CursorTimestamp(time.Now())
				query, args := jsonEntryBatch([]feedMatch{scope}, migrated, testViewer, now, thinstore.FilterUnread, cursor, 2)
				rows, err := db.Reader().QueryContext(t.Context(), query, args...)
				if err != nil {
					t.Fatal(err)
				}
				actual, next, count, err := readEntries(rows, true)
				if err != nil {
					t.Fatal(err)
				}
				oracle, oracleArgs := unboundedFeedBatch([]feedMatch{scope}, migrated, now, thinstore.FilterUnread, cursor, 2)
				rows, err = db.Reader().QueryContext(t.Context(), oracle, oracleArgs...)
				if err != nil {
					t.Fatal(err)
				}
				expected, expectedNext, expectedCount, err := readEntries(rows, true)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, expected) || !reflect.DeepEqual(next, expectedNext) || count != expectedCount {
					t.Fatalf("phase=%s migrated=%t page=%d differs from independent unread oracle", phase, migrated, page)
				}
				for _, item := range actual {
					seen[item.EntryID] = true
				}
				cursor = next
				if count < 2 {
					break
				}
			}
			wantBoundaryUnread := migrated || phase != "large stored floor"
			if seen[uri("z-critical")] != wantBoundaryUnread || seen[uri("a-new-micro")] != wantBoundaryUnread || seen[uri("marked")] || seen[uri("expired")] || seen[uri("outside-override")] {
				t.Fatalf("phase=%s changed rounded boundary, expiry, scope, or read-mark precedence", phase)
			}
			if !migrated && oldOverride && !seen[uri("old-override")] {
				t.Fatal("scoped fallback lost an older explicit unread override")
			}
		}
	}
	check("no overrides", false)
	addOverride("outside-override")
	check("override outside publication", false)
	addOverride("old-override")
	check("override inside publication", true)
	addOverride("marked")
	check("read mark wins over override", true)
	if _, err := db.Writer().Exec("UPDATE appview_publication_read_floors SET read_floor_at_us=?", int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	check("large stored floor", true)
	if _, err := db.Writer().Exec("DELETE FROM appview_publication_read_floors"); err != nil {
		t.Fatal(err)
	}
	check("no floor", true)
}
