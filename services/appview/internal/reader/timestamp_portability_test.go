package reader

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func insertTimestampItem(t *testing.T, db *sqlitedb.DB, uri, raw string) {
	t.Helper()
	micros, err := sqlitedb.TimestampMicroseconds(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer().Exec(`INSERT INTO content_items
		(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at,created_at_us,indexed_at_us)
		VALUES(?, 'cid', ?, 'site.standard.document', ?, ?, ?, '{}', '4001-01-01T00:00:00Z', ?, ?)`,
		uri, testAuthor, raw, raw, testPub, micros, micros)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBoundaryMicrosecondBackfillAndTriggerReplacement(t *testing.T) {
	db := testReaderDB(t)
	// Recreate the exact pre-portability boundary table and an obsolete trigger.
	_, err := db.Writer().Exec(`DROP TABLE appview_pds_read_state_boundaries;
		CREATE TABLE appview_pds_read_state_boundaries(
			viewer_did TEXT NOT NULL,sequence INTEGER NOT NULL,rule_key TEXT NOT NULL,
			is_read INTEGER NOT NULL,acted_at TEXT NOT NULL,author_did TEXT NOT NULL,
			scope_keys TEXT NOT NULL,boundary_at TEXT NOT NULL,boundary_uri TEXT,
			PRIMARY KEY(viewer_did,sequence,rule_key));
		INSERT INTO appview_pds_read_state_boundaries VALUES('viewer',1,'rule',1,'2026-01-01T00:00:00Z','author','[]','2026-01-01T01:00:00.999999+01:00',NULL);
		DROP TRIGGER pds_content_INSERT;
		CREATE TRIGGER pds_content_INSERT AFTER INSERT ON content_items BEGIN SELECT tsw_timestamp_us(NEW.created_at); END;`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	var micros int64
	if err := db.Reader().QueryRow("SELECT boundary_at_us FROM appview_pds_read_state_boundaries").Scan(&micros); err != nil {
		t.Fatal(err)
	}
	want, _ := sqlitedb.TimestampMicroseconds("2026-01-01T00:00:00.999999Z")
	if micros != want {
		t.Fatalf("backfilled %d, want %d", micros, want)
	}
	var trigger string
	if err := db.Reader().QueryRow("SELECT sql FROM sqlite_schema WHERE name='pds_content_INSERT'").Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(trigger, "tsw_timestamp") || !strings.Contains(trigger, "NEW.created_at_us") {
		t.Fatal("obsolete content trigger survived migration")
	}
	if _, err := db.Writer().Exec(`INSERT INTO appview_pds_read_state_boundaries(viewer_did,sequence,rule_key,is_read,acted_at,author_did,scope_keys,boundary_at)
		VALUES('viewer',2,'bad',1,'2026-01-01T00:00:00Z','author','[]','2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("boundary without stored micros was accepted")
	}
}

func TestStoredMicrosecondReadFloorsPreserveURITiesAndOverrides(t *testing.T) {
	db := testReaderDB(t)
	at := "2026-01-01T00:00:00.999999Z"
	micros, _ := sqlitedb.TimestampMicroseconds(at)
	ids := []string{"at://did:plc:author/site.standard.document/a", "at://did:plc:author/site.standard.document/b", "at://did:plc:author/site.standard.document/c"}
	for _, id := range ids {
		insertTimestampItem(t, db, id, "2026-01-01T01:00:00.999999+01:00")
	}
	if _, err := db.Writer().Exec(`INSERT INTO appview_publication_read_floors
		(viewer_did,publication_id,read_floor_at,read_floor_uri,generation,updated_at,read_floor_at_us) VALUES(?,?,?,?,1,?,?)`,
		testViewer, testPub, at, ids[1], at, micros); err != nil {
		t.Fatal(err)
	}
	assertRead := func(id string, want bool) {
		t.Helper()
		var got bool
		err := db.Reader().QueryRow(`SELECT `+readExpression+` FROM content_items ci
			LEFT JOIN read_marks rm ON rm.viewer_did=? AND rm.subject_uri=ci.uri
			LEFT JOIN appview_unread_overrides uo ON uo.viewer_did=? AND uo.subject_uri=ci.uri
			LEFT JOIN appview_publication_read_floors floor ON floor.viewer_did=? AND floor.publication_id=? WHERE ci.uri=?`,
			false, testViewer, testViewer, testViewer, testPub, id).Scan(&got)
		if err != nil || got != want {
			t.Fatalf("read(%s)=%t, want %t: %v", id, got, want, err)
		}
	}
	assertRead(ids[0], true)
	assertRead(ids[1], true)
	assertRead(ids[2], false)
	if err := changeReadMark(context.Background(), db, testViewer, ids[0], time.Now(), false); err != nil {
		t.Fatal(err)
	}
	assertRead(ids[0], false)
	if _, err := db.Writer().Exec("UPDATE appview_publication_read_floors SET read_floor_uri=NULL WHERE viewer_did=?", testViewer); err != nil {
		t.Fatal(err)
	}
	assertRead(ids[2], true)
}

func TestProjectionMicrosecondsPreserveSequenceAndContentTriggers(t *testing.T) {
	db := testReaderDB(t)
	at := "2026-01-01T00:00:00.999999Z"
	ids := []string{"at://did:plc:author/site.standard.document/a", "at://did:plc:author/site.standard.document/b", "at://did:plc:author/site.standard.document/c"}
	for _, id := range ids {
		insertTimestampItem(t, db, id, at)
	}
	projection := &readstate.Projection{Operations: []readstate.Operation{
		readstate.NewBoundaryOperation("boundary", 1, readstate.StateRead, at, []readstate.Boundary{{Scope: readstate.Scope{PublicationID: testPub, AuthorDID: testAuthor}, CreatedAt: "2026-01-01T01:00:00.999999+01:00", EntryID: &ids[1]}}),
		readstate.NewExactOperation("exact", 2, readstate.StateUnread, at, []string{ids[0]}, nil),
	}}
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		return installProjection(context.Background(), tx, testViewer, "{}", "manifest", projection)
	}); err != nil {
		t.Fatal(err)
	}
	assertMark := func(id string, want bool) {
		t.Helper()
		var got bool
		if err := db.Reader().QueryRow("SELECT EXISTS(SELECT 1 FROM read_marks WHERE viewer_did=? AND subject_uri=?)", testViewer, id).Scan(&got); err != nil || got != want {
			t.Fatalf("mark(%s)=%t want %t: %v", id, got, want, err)
		}
	}
	assertMark(ids[0], false)
	assertMark(ids[1], true)
	assertMark(ids[2], false)
	newID := "at://did:plc:author/site.standard.document/aa"
	insertTimestampItem(t, db, newID, at)
	assertMark(newID, true)
	later := "2026-01-01T00:00:01Z"
	laterMicros, _ := sqlitedb.TimestampMicroseconds(later)
	if _, err := db.Writer().Exec("UPDATE content_items SET created_at=?,created_at_us=? WHERE uri=?", later, laterMicros, newID); err != nil {
		t.Fatal(err)
	}
	assertMark(newID, false)
}

func TestMarkAllReadKeepsNewerUnreadOverride(t *testing.T) {
	db := testReaderDB(t)
	at := "2026-01-01T00:00:00.999999Z"
	now, _ := time.Parse(time.RFC3339Nano, "2026-01-02T00:00:00.999999Z")
	ids := []string{"at://did:plc:author/site.standard.document/a", "at://did:plc:author/site.standard.document/b", "at://did:plc:author/site.standard.document/c"}
	for i, id := range ids {
		insertTimestampItem(t, db, id, at)
		if err := changeReadMark(context.Background(), db, testViewer, id, now.Add(time.Duration(i-1)*time.Microsecond), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := setReadFloor(context.Background(), tx, testViewer, thinstore.PublicationScope{PublicationID: testPub, AuthorDID: testAuthor}, feedMatch{AuthorDID: testAuthor}, now, 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		var remains bool
		if err := db.Reader().QueryRow("SELECT EXISTS(SELECT 1 FROM appview_unread_overrides WHERE viewer_did=? AND subject_uri=?)", testViewer, id).Scan(&remains); err != nil || remains != (i == 2) {
			t.Fatalf("override %d remains=%t: %v", i, remains, err)
		}
	}
}
