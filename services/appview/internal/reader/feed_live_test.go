package reader

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/analytics"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	turso "turso.tech/database/tursogo-serverless"
)

func TestLiveTursoLargeFeedQuery(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set TSW_TURSO_PROBE_DIR for isolated hosted-engine acceptance")
	}
	read := func(suffix string) string {
		path := filepath.Join(dir, "appview-"+suffix)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("probe credentials must be owner-only regular files")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("probe credentials could not be read")
		}
		return strings.TrimSpace(string(data))
	}
	endpoint, token := read("url"), read("token")
	parsed, err := url.Parse(endpoint)
	if err != nil || !strings.Contains(parsed.Hostname(), "-probe-") {
		t.Fatal("live acceptance requires an isolated probe database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	fail := func(phase string, err error) {
		var chain []string
		for err != nil {
			chain = append(chain, err.Error())
			err = errors.Unwrap(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "large-feed-diagnostic.log"), []byte(strings.Join(chain, "\n")), 0600)
		t.Fatalf("hosted probe failed during %s; private diagnostic saved", phase)
	}
	db, err := sqlitedb.OpenTurso(ctx, endpoint, token)
	if err != nil {
		fail("connection", err)
	}
	defer db.Close()
	scopes := make([]feedMatch, 628)
	for index := range scopes {
		scopes[index] = feedMatch{PublicationID: fmt.Sprintf("probe-publication-%d", index), AuthorDID: "did:plc:hosted-readonly-probe"}
		switch index % 3 {
		case 1:
			scopes[index].Sites = []string{"https://probe.example/one"}
		case 2:
			scopes[index].Sites = []string{"https://probe.example/one", "https://probe.example/two"}
		}
	}
	for _, filter := range []thinstore.EntryListFilter{thinstore.FilterAll, thinstore.FilterRead, thinstore.FilterUnread} {
		query, args := jsonEntryBatch(scopes, false, "did:plc:hosted-readonly-probe", thinstore.CursorTimestamp(time.Now()), filter, nil, 100)
		started := time.Now()
		err := db.Read(ctx, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			if rows.Next() {
				return errors.New("isolated read-only fixture unexpectedly contains matching entries")
			}
			return rows.Err()
		})
		if err != nil {
			fail("large bounded feed query", err)
		}
		t.Logf("628-scope constant-size %s hosted query completed in %s", filter, time.Since(started).Round(time.Millisecond))
	}
	for _, phase := range []string{"unread counts", "read-age inventory", "legacy read-state export"} {
		err := db.Read(ctx, func(tx *sql.Tx) error {
			requested := []thinstore.PublicationScope{{PublicationID: "probe", AuthorDID: "did:plc:hosted-readonly-probe"}}
			if phase == "unread counts" {
				_, err := counts(ctx, tx, "did:plc:hosted-readonly-probe", requested)
				return err
			}
			if phase == "legacy read-state export" {
				viewer := "did:plc:hosted-readonly-probe"
				if _, err := legacyRows(ctx, tx, viewer, 50, 0); err != nil {
					return err
				}
				var count int
				return tx.QueryRowContext(ctx, legacyReadStateCTE+"SELECT count(*) FROM baseline", viewer, viewer, viewer).Scan(&count)
			}
			return forEachUnread(ctx, tx, "did:plc:hosted-readonly-probe", requested, func(string, time.Time) error { return nil })
		})
		if err != nil {
			fail(phase, err)
		}
	}
	if _, err := analytics.NewStore(db, "synthetic-probe-only").Summary(ctx, "did:plc:hosted-readonly-probe", 30, 50, "", time.Now()); err != nil {
		fail("analytics summary including full CTE", err)
	}
}

func TestLiveTursoFeedQueryPhases(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set TSW_TURSO_PROBE_DIR for isolated hosted-engine acceptance")
	}
	endpointBytes, err := os.ReadFile(filepath.Join(dir, "appview-url"))
	if err != nil {
		t.Fatal("probe endpoint unavailable")
	}
	token, err := os.ReadFile(filepath.Join(dir, "appview-token"))
	if err != nil {
		t.Fatal("probe token unavailable")
	}
	endpoint, err := url.Parse(strings.TrimSpace(string(endpointBytes)))
	if err != nil || !strings.Contains(endpoint.Hostname(), "-probe-") {
		t.Fatal("live acceptance requires an isolated probe database")
	}
	endpoint.Scheme = "https"
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("probe redirect refused") }}
	connection, err := turso.NewConnector(endpoint.String(), strings.TrimSpace(string(token))).WithHTTPClient(client).WithSessionRetention().OpenConnection(ctx)
	if err != nil {
		t.Fatal("raw hosted probe connection failed")
	}
	defer connection.Close()
	for _, size := range []int{1, 32, 128, 628, 628} {
		scopes := make([]feedMatch, size)
		for index := range scopes {
			scopes[index] = feedMatch{PublicationID: fmt.Sprintf("probe-publication-%d", index), AuthorDID: "did:plc:hosted-readonly-probe"}
		}
		query, args := boundedEntryBatch(scopes, false, "did:plc:hosted-readonly-probe", thinstore.CursorTimestamp(time.Now()), thinstore.FilterAll, nil, 100)
		named := make([]driver.NamedValue, len(args))
		for index, value := range args {
			converted, err := driver.DefaultParameterConverter.ConvertValue(value)
			if err != nil {
				t.Fatal("raw hosted parameter conversion failed")
			}
			named[index] = driver.NamedValue{Ordinal: index + 1, Value: converted}
		}
		started := time.Now()
		description, err := connection.DescribeContext(ctx, query)
		describeTime := time.Since(started)
		if err != nil || description == nil || !description.IsReadOnly {
			t.Fatal("raw hosted read-only description failed")
		}
		started = time.Now()
		rows, err := connection.QueryContext(ctx, query, named)
		if err != nil {
			_ = os.WriteFile(filepath.Join(dir, "raw-phase-diagnostic.log"), []byte(err.Error()), 0600)
			t.Fatal("raw hosted query failed")
		}
		values := make([]driver.Value, len(rows.Columns()))
		for {
			err = rows.Next(values)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				rows.Close()
				t.Fatal("raw hosted query iteration failed")
			}
		}
		if rows.Close() != nil {
			t.Fatal("raw hosted query close failed")
		}
		t.Logf("scopes=%d SQL=%d bytes describe=%s query=%s", size, len(query), describeTime.Round(time.Millisecond), time.Since(started).Round(time.Millisecond))
	}
}

func TestLiveTursoPersistedReadStateGuards(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set TSW_TURSO_PROBE_DIR for isolated hosted-engine acceptance")
	}
	endpointBytes, err := os.ReadFile(filepath.Join(dir, "appview-url"))
	if err != nil {
		t.Fatal("probe endpoint unavailable")
	}
	token, err := os.ReadFile(filepath.Join(dir, "appview-token"))
	if err != nil {
		t.Fatal("probe token unavailable")
	}
	endpoint := strings.TrimSpace(string(endpointBytes))
	parsed, err := url.Parse(endpoint)
	if err != nil || !strings.Contains(parsed.Hostname(), "-probe-") {
		t.Fatal("live acceptance requires an isolated probe database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	db, err := sqlitedb.OpenTurso(ctx, endpoint, strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal("hosted probe connection failed")
	}
	defer db.Close()
	rollback := errors.New("intentional probe rollback")
	viewer := fmt.Sprintf("did:plc:probe%d", time.Now().UnixNano())
	parent := fmt.Sprintf("live_probe_parent_%d", time.Now().UnixNano())
	child := parent + "_child"
	err = db.Write(ctx, func(tx *sql.Tx) error {
		for _, query := range []string{
			"CREATE TABLE " + parent + "(id INTEGER PRIMARY KEY)",
			"CREATE TABLE " + child + "(id INTEGER PRIMARY KEY,parent_id INTEGER REFERENCES " + parent + "(id))",
			"INSERT INTO " + parent + " VALUES(1)",
			"INSERT INTO " + child + " VALUES(1,1)",
		} {
			if _, err := tx.ExecContext(ctx, query); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+child+" VALUES(2,999)"); err == nil {
			return errors.New("foreign-key violation was accepted")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO read_marks(viewer_did,subject_uri,created_at) VALUES(?,?,?)", viewer, "at://did:plc:probe/site.standard.document/one", "2026-11-21T00:00:00Z"); err != nil {
			return err
		}
		var revision int
		if err := tx.QueryRowContext(ctx, "SELECT legacy_revision FROM appview_pds_read_state_authority WHERE viewer_did=?", viewer).Scan(&revision); err != nil {
			return err
		}
		if revision != 1 {
			return errors.New("persisted revision trigger did not increment exactly once")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE appview_pds_read_state_authority SET manifest_cid='synthetic-probe' WHERE viewer_did=?", viewer); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO read_marks(viewer_did,subject_uri,created_at) VALUES(?,?,?)", viewer, "at://did:plc:probe/site.standard.document/two", "2026-11-21T00:00:00Z"); err == nil {
			return errors.New("persisted migrated-authority write fence was bypassed")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO appview_pds_read_state_boundaries(viewer_did,sequence,rule_key,is_read,acted_at,author_did,scope_keys,boundary_at) VALUES(?,1,'probe',1,'2026-11-21T00:00:00Z','did:plc:probe','[]','2026-11-21T00:00:00Z')`, viewer)
		if err == nil {
			return errors.New("persisted integer boundary guard accepted a missing timestamp")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO appview_pds_read_state_boundaries(viewer_did,sequence,rule_key,is_read,acted_at,author_did,scope_keys,boundary_at,boundary_at_us) VALUES(?,1,'probe',1,'2026-11-21T00:00:00Z','did:plc:probe','[]','2026-11-21T00:00:00Z',1795219200000000)`, viewer); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		var chain []string
		for err != nil {
			chain = append(chain, err.Error())
			err = errors.Unwrap(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "persisted-guards-diagnostic.log"), []byte(strings.Join(chain, "\n")), 0600)
		t.Fatal("hosted persisted guard acceptance failed; private diagnostic saved")
	}
	var count int
	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name IN (?,?)", parent, child).Scan(&count); err != nil || count != 0 {
		t.Fatal("transactional DDL did not roll back")
	}
	if err := db.Reader().QueryRowContext(ctx, "SELECT count(*) FROM appview_pds_read_state_authority WHERE viewer_did=?", viewer).Scan(&count); err != nil || count != 0 {
		t.Fatal("synthetic read-state writes did not roll back")
	}
}
