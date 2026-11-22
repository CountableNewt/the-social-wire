package reader

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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
	var totalGeneral, totalBounded, totalJSON time.Duration
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
				compact, compactArgs := jsonEntryBatch(scopes, migrated, testViewer, now, filter, cursor, limit)
				started = time.Now()
				rows, err = db.Reader().QueryContext(context.Background(), compact, compactArgs...)
				if err != nil {
					t.Fatal(err)
				}
				compactRows, compactNext, compactScanned, err := readEntries(rows, true)
				if err != nil {
					t.Fatal(err)
				}
				totalJSON += time.Since(started)
				if !reflect.DeepEqual(compactRows, expected) || !reflect.DeepEqual(compactNext, expectedNext) || compactScanned != expectedScanned {
					t.Fatalf("compact migrated=%t filter %s page %d differs from unbounded merge", migrated, filter, page)
				}
				cursor = next
			}
		}
		encoded, _ := json.Marshal(scopes)
		compactCounts, compactArgs := unreadCountsQuery(encoded, migrated, testViewer, now)
		legacyCounts := readerCTEStart + "SELECT scopes.pub," + readExpression + " AS is_read" + readerJoins + ") SELECT pub,count(*) FROM candidates WHERE is_read=0 GROUP BY pub"
		readCounts := func(query string, args []any) map[string]int {
			result := map[string]int{}
			for _, scope := range scopes {
				result[scope.PublicationID] = 0
			}
			rows, err := db.Reader().QueryContext(t.Context(), query, args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var publication string
				var count int
				if err := rows.Scan(&publication, &count); err != nil {
					t.Fatal(err)
				}
				result[publication] = count
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return result
		}
		actualCounts := readCounts(compactCounts, compactArgs)
		expectedCounts := readCounts(legacyCounts, []any{string(encoded), migrated, testViewer, testViewer, testViewer, now})
		if !reflect.DeepEqual(actualCounts, expectedCounts) {
			t.Fatalf("compact migrated=%t unread counts differ from legacy", migrated)
		}
	}
	t.Logf("20,000 rows, 22 scopes, eighteen batches: unbounded=%s bounded=%s compact=%s", totalGeneral, totalBounded, totalJSON)
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
		compactQuery, compactArgs := jsonEntryBatch(scopes, false, testViewer, now, thinstore.FilterAll, cursor, 100)
		if len(compactQuery) > 10000 || len(compactArgs) > 32 {
			t.Fatalf("compact query budget exceeded: %d bytes, %d bindings", len(compactQuery), len(compactArgs))
		}
		started = time.Now()
		rows, err = db.Reader().QueryContext(t.Context(), compactQuery, compactArgs...)
		if err != nil {
			t.Fatal(err)
		}
		compactRows, compactNext, _, err := readEntries(rows, true)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(compactRows, actual) || !reflect.DeepEqual(compactNext, next) {
			t.Fatalf("compact large feed page %d differs from bounded merge", page)
		}
		t.Logf("62,000 RSS rows,628 scopes,page %d: bounded=%s compact=%s", page, bounded, time.Since(started))
		cursor = next
	}
}

// Previous bounded-union plan remains a test-only equivalence and profiling baseline.
const readerCTEStart = `WITH scopes AS MATERIALIZED (
		SELECT json_extract(value,'$.author') AS author, json_extract(value,'$.publication') AS pub,
			json_extract(value,'$.sites') AS sites FROM json_each(?)
	), candidates AS (`

func boundedEntryBatch(scopes []feedMatch, migrated bool, viewer, now string, filter thinstore.EntryListFilter, cursor *string, limit int) (string, []any) {
	// Small groups bound compound-select parser depth as subscription sets grow.
	const groupSize = 32
	branches := make([]string, 0, min(len(scopes), groupSize))
	groups := make([]string, 0, (len(scopes)+groupSize-1)/groupSize)
	var args []any
	timestamp := thinstore.FeedTimestampSQL("ci.created_at")
	for i, scope := range scopes {
		sites, _ := json.Marshal(scope.Sites)
		index := "idx_content_items_author_timestamp_uri"
		sitePredicate := "(ci.publication_site IN (SELECT value FROM json_each(?)) OR ?)"
		siteArgs := []any{string(sites), scope.Sites == nil}
		if len(scope.Sites) == 1 {
			index = "idx_content_items_author_site_timestamp_uri"
			sitePredicate = "ci.publication_site=?"
			siteArgs = []any{scope.Sites[0]}
		}
		// The author/time index permits each scope to stop after its own limit;
		// the expiry index would instead scan and sort its complete live history.
		branch := "SELECT ci.uri,ci.render_json,ci.created_at,? AS pub," + timestamp + " AS position," + readExpression + ` AS is_read
 FROM content_items ci INDEXED BY ` + index + `
 LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
 LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri
 LEFT JOIN appview_publication_read_floors floor ON floor.viewer_did=? AND floor.publication_id=?
 WHERE ci.author_did=? AND ` + sitePredicate + ` AND ci.expires_at>?`
		args = append(args, scope.PublicationID, migrated, viewer, viewer, viewer, scope.PublicationID, scope.AuthorDID)
		args = append(args, siteArgs...)
		args = append(args, now)
		if cursor != nil {
			at, uri, _ := thinstore.DecodeCursor(*cursor)
			branch += " AND (" + timestamp + ",ci.uri) < (?,?)"
			args = append(args, at.UTC().Format("2006-01-02T15:04:05.000000000Z"), uri)
		}
		if filter == thinstore.FilterRead {
			branch += " AND (" + readExpression + ")=1"
			args = append(args, migrated)
		} else if filter == thinstore.FilterUnread {
			branch += " AND (" + readExpression + ")=0"
			args = append(args, migrated)
		}
		branch += " ORDER BY position DESC,uri DESC LIMIT ?"
		args = append(args, limit)
		branches = append(branches, "SELECT * FROM ("+branch+")")
		if len(branches) == groupSize || i == len(scopes)-1 {
			group := "SELECT uri,render_json,created_at,pub,position,is_read FROM (" + strings.Join(branches, " UNION ALL ") + ") ORDER BY position DESC,uri DESC,pub ASC LIMIT ?"
			groups = append(groups, "SELECT * FROM ("+group+")")
			args = append(args, limit)
			branches = branches[:0]
		}
	}
	args = append(args, limit)
	return "SELECT uri,render_json,created_at,pub,is_read FROM (" + strings.Join(groups, " UNION ALL ") + ") ORDER BY position DESC,uri DESC,pub ASC LIMIT ?", args
}
