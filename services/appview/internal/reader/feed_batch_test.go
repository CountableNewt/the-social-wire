package reader

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestBoundedFeedBatchesPreserveFilterAndCursorAtScale(t *testing.T) {
	db := testReaderDB(t)
	// Independent author histories make an unbounded aggregate sort expensive,
	// while each author's newest page can stop at its timestamp index.
	if _, err := db.Writer().Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<20000)
 INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at,created_at_us,indexed_at_us)
 SELECT 'at://did:plc:author'||(i%20)||'/site.standard.document/'||i,'cid','did:plc:author'||(i%20),'site.standard.document',
 strftime('%Y-%m-%dT%H:%M:%SZ',1795000000+i,'unixepoch'),'2026-11-18T00:00:00Z','publication-'||(i%20),
 '{"title":"Fixture","publishedAt":"2026-11-18T00:00:00Z"}','2099-01-01T00:00:00Z',(1795000000+i)*1000000,1794960000000000 FROM n;
 INSERT INTO read_marks(viewer_did,subject_uri,created_at) SELECT 'did:plc:viewer',uri,'2026-11-18T00:00:00Z' FROM content_items WHERE rowid%3=0;
 UPDATE content_items SET expires_at='2020-01-01T00:00:00Z' WHERE rowid%17=0;
 INSERT INTO appview_publication_read_floors(viewer_did,publication_id,read_floor_at,generation,updated_at,read_floor_at_us)
 VALUES('did:plc:viewer','publication-0','2026-11-18T16:31:40Z',1,'2026-11-18T00:00:00Z',1795019500000000);
 INSERT INTO appview_unread_overrides(viewer_did,subject_uri,created_at,created_at_us)
 SELECT 'did:plc:viewer',uri,'2026-11-18T00:00:00Z',1794960000000000 FROM content_items WHERE rowid%7=0;`); err != nil {
		t.Fatal(err)
	}
	var scopes []feedMatch
	for i := range 20 {
		scopes = append(scopes, feedMatch{AuthorDID: fmt.Sprintf("did:plc:author%d", i), PublicationID: fmt.Sprintf("publication-%d", i)})
	}
	// A shared-author publication tests scope matching and publication tie order.
	scopes = append(scopes, feedMatch{AuthorDID: "did:plc:author0", PublicationID: "overlap", Sites: []string{"publication-0"}})
	scopes = append(scopes, feedMatch{AuthorDID: "did:plc:author1", PublicationID: "multi-site", Sites: []string{"publication-1", "missing"}})
	const limit = 100
	now := thinstore.CursorTimestamp(time.Now())
	var totalGeneral, totalBounded time.Duration
	for _, migrated := range []bool{false, true} {
		for _, filter := range []thinstore.EntryListFilter{thinstore.FilterAll, thinstore.FilterRead, thinstore.FilterUnread} {
			var cursor *string
			for page := range 3 {
				query, args := boundedEntryBatch(scopes, migrated, testViewer, now, filter, cursor, limit)
				started := time.Now()
				rows, err := db.Reader().QueryContext(context.Background(), query, args...)
				if err != nil {
					t.Fatal(err)
				}
				actual, next, scanned, err := readEntries(rows, true)
				if err != nil {
					t.Fatal(err)
				}
				totalBounded += time.Since(started)
				general, generalArgs := unboundedFeedBatch(scopes, migrated, now, filter, cursor, limit)
				started = time.Now()
				rows, err = db.Reader().QueryContext(context.Background(), general, generalArgs...)
				if err != nil {
					t.Fatal(err)
				}
				expected, expectedNext, expectedScanned, err := readEntries(rows, true)
				if err != nil {
					t.Fatal(err)
				}
				totalGeneral += time.Since(started)
				if !reflect.DeepEqual(actual, expected) || !reflect.DeepEqual(next, expectedNext) || scanned != expectedScanned {
					t.Fatalf("migrated=%t filter %s page %d differs from unbounded merge", migrated, filter, page)
				}
				cursor = next
			}
		}
	}
	t.Logf("20,000 rows, 22 scopes, eighteen batches: unbounded=%s bounded=%s", totalGeneral, totalBounded)
}

func unboundedFeedBatch(scopes []feedMatch, migrated bool, now string, filter thinstore.EntryListFilter, cursor *string, limit int) (string, []any) {
	encoded, _ := json.Marshal(scopes)
	timestamp := thinstore.FeedTimestampSQL("ci.created_at")
	query := readerCTEStart + "SELECT ci.uri,ci.render_json,ci.created_at,scopes.pub," + timestamp + " AS position," + readExpression + " AS is_read" + readerJoins
	args := []any{string(encoded), migrated, testViewer, testViewer, testViewer, now}
	if cursor != nil {
		at, uri, _ := thinstore.DecodeCursor(*cursor)
		query += " AND (" + timestamp + ",ci.uri)<(?,?)"
		args = append(args, at.UTC().Format("2006-01-02T15:04:05.000000000Z"), uri)
	}
	query += ") SELECT uri,render_json,created_at,pub,is_read FROM candidates"
	if filter == thinstore.FilterRead {
		query += " WHERE is_read=1"
	}
	if filter == thinstore.FilterUnread {
		query += " WHERE is_read=0"
	}
	return query + " ORDER BY position DESC,uri DESC,pub ASC LIMIT ?", append(args, limit)
}

func TestBoundedFeedHandlesLargeRSSSubscriptionSets(t *testing.T) {
	db := testReaderDB(t)
	if _, err := db.Writer().Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<62000)
 INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at,created_at_us,indexed_at_us)
 SELECT 'at://did:web:rss/site.standard.document/'||i,'cid','did:web:rss','site.standard.document',
 strftime('%Y-%m-%dT%H:%M:%SZ',1795000000+i,'unixepoch'),'2026-11-18T00:00:00Z','publication-'||(i%628),
 '{"title":"Fixture","publishedAt":"2026-11-18T00:00:00Z"}','2099-01-01T00:00:00Z',(1795000000+i)*1000000,1794960000000000 FROM n;`); err != nil {
		t.Fatal(err)
	}
	scopes := make([]feedMatch, 628)
	for i := range scopes {
		scopes[i] = feedMatch{AuthorDID: "did:web:rss", PublicationID: fmt.Sprintf("publication-%d", i), Sites: []string{fmt.Sprintf("publication-%d", i)}}
	}
	now := thinstore.CursorTimestamp(time.Now())
	var cursor *string
	for page := range 2 {
		query, args := boundedEntryBatch(scopes, false, testViewer, now, thinstore.FilterAll, cursor, 100)
		if len(query) > 2*1024*1024 || len(args) > 9000 {
			t.Fatalf("query budget exceeded: %d bytes, %d bindings", len(query), len(args))
		}
		started := time.Now()
		rows, err := db.Reader().QueryContext(t.Context(), query, args...)
		if err != nil {
			t.Fatal(err)
		}
		actual, next, _, err := readEntries(rows, true)
		if err != nil {
			t.Fatal(err)
		}
		bounded := time.Since(started)
		if len(actual) != 100 || next == nil {
			t.Fatalf("large feed page %d has %d entries", page, len(actual))
		}
		for i, item := range actual {
			position := 62000 - page*100 - i
			if item.EntryID != fmt.Sprintf("at://did:web:rss/site.standard.document/%d", position) || item.PublicationID == nil || *item.PublicationID != fmt.Sprintf("publication-%d", position%628) || item.IsRead {
				t.Fatalf("large feed page %d row %d incorrect", page, i)
			}
		}
		if *next != thinstore.EncodeCursor(actual[99].FeedPositionAt, actual[99].EntryID) {
			t.Fatal("large feed cursor skipped the returned boundary")
		}
		t.Logf("62,000 RSS rows,628 scopes,page %d: bounded=%s", page, bounded)
		cursor = next
	}
}
