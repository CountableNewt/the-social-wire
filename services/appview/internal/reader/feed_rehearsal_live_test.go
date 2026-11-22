package reader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
	turso "turso.tech/database/tursogo-serverless"
)

type rehearsalReadTransport func(*http.Request) (*http.Response, error)

func (f rehearsalReadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// This acceptance test deliberately uses a database-scoped provider READ-ONLY
// token, not the production connector which must enable foreign-key enforcement
// before any writes. No write fixture or migration can run through this pool.
func TestLiveTursoImportedReaderAcceptance(t *testing.T) {
	runImportedReaderAcceptance(t, false, "")
}

func TestLiveTursoImportedReaderPhaseProfile(t *testing.T) {
	if os.Getenv("TSW_TURSO_REHEARSAL_PROFILE") != "1" {
		t.Skip("set TSW_TURSO_REHEARSAL_PROFILE for diagnostic timing only")
	}
	runImportedReaderAcceptance(t, true, "")
}

func TestLiveTursoImportedReaderCostProbe(t *testing.T) {
	operation := os.Getenv("TSW_TURSO_REHEARSAL_COST_OPERATION")
	if operation == "" {
		t.Skip("set an explicit cost-probe operation for one isolated query bundle")
	}
	if operation != "feed" && operation != "counts" && operation != "read-age" {
		t.Fatal("cost operation must be feed, counts or read-age")
	}
	runImportedReaderAcceptance(t, false, operation)
}

func runImportedReaderAcceptance(t *testing.T, profile bool, costOperation string) {
	t.Helper()
	dir, snapshot := os.Getenv("TSW_TURSO_REHEARSAL_DIR"), os.Getenv("TSW_SQLITE_REHEARSAL_SNAPSHOT")
	if dir == "" || snapshot == "" {
		t.Skip("set rehearsal directory and offline snapshot for read-only import acceptance")
	}
	read := func(name string) string {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("rehearsal credentials must be owner-only regular files")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("rehearsal credentials unavailable")
		}
		return strings.TrimSpace(string(data))
	}
	endpoint, err := url.Parse(read("database-url"))
	if err != nil || !strings.Contains(endpoint.Hostname(), "rehearsal") || endpoint.User != nil || endpoint.RawQuery != "" {
		t.Fatal("read-only import acceptance requires an explicit rehearsal endpoint")
	}
	endpoint.Scheme = "https"
	token := read("database-token")
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("rehearsal redirect refused") },
		Transport: rehearsalReadTransport(func(request *http.Request) (*http.Response, error) {
			if request.URL.Scheme != "https" || !strings.EqualFold(request.URL.Host, endpoint.Host) || request.URL.User != nil {
				return nil, errors.New("rehearsal origin refused")
			}
			return http.DefaultTransport.RoundTrip(request)
		})}
	remote := sql.OpenDB(rehearsalDescribedConnector{turso.NewConnector(endpoint.String(), token).WithHTTPClient(client).WithSessionRetention()})
	defer remote.Close()
	localURL := (&url.URL{Scheme: "file", Path: snapshot, RawQuery: "mode=ro"}).String()
	local, err := sql.Open("sqlite", localURL)
	if err != nil {
		t.Fatal("offline reference snapshot unavailable")
	}
	defer local.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	fail := func(phase string, err error) {
		var chain []string
		for err != nil {
			chain = append(chain, err.Error())
			err = errors.Unwrap(err)
		}
		_ = os.WriteFile(filepath.Join(dir, "reader-acceptance-diagnostic.log"), []byte(strings.Join(chain, "\n")), 0600)
		t.Fatalf("read-only rehearsal failed during %s; private diagnostic saved", phase)
	}
	var viewer, kind, id string
	var scopeCount int
	if err := local.QueryRowContext(ctx, `SELECT viewer_did,feed_kind,feed_id,count(*) FROM appview_feed_publications GROUP BY viewer_did,feed_kind,feed_id ORDER BY count(*) DESC LIMIT 1`).Scan(&viewer, &kind, &id, &scopeCount); err != nil {
		fail("largest reference feed", err)
	}
	t.Logf("largest reference feed scopes=%d", scopeCount)
	if err := remote.PingContext(ctx); err != nil {
		fail("startup-equivalent connection warmup", err)
	}
	if costOperation != "" {
		budgetDuration := 15 * time.Second
		if costOperation == "feed" {
			budgetDuration = readerQueryBudget(true)
		}
		budget, stop := context.WithTimeout(ctx, budgetDuration)
		defer stop()
		started := time.Now()
		tx, err := remote.BeginTx(budget, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			fail("cost-probe transaction", err)
		}
		defer tx.Rollback()
		counter := &readerQueryCounter{Tx: tx}
		var requested []thinstore.PublicationScope
		var migrated bool
		var matched []feedMatch
		if costOperation == "feed" {
			_, requested, matched, migrated, err = feedSnapshotWithMatches(budget, counter, viewer, kind, id)
		} else {
			requested, err = scopes(budget, counter, viewer, kind, id)
		}
		returned := 0
		if err == nil {
			switch costOperation {
			case "feed":
				var result thinstore.EntryListResponse
				result, err = listMatchedEntries(budget, counter, viewer, matched, thinstore.FilterAll, nil, 50, true, migrated)
				returned = len(result.Entries)
			case "counts":
				var result map[string]int
				result, err = counts(budget, counter, viewer, requested)
				returned = len(result)
			case "read-age":
				err = forEachUnread(budget, counter, viewer, requested, func(string, time.Time) error { returned++; return nil })
			}
		}
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			fail("isolated "+costOperation+" query bundle", err)
		}
		t.Logf("operation=%s scopes=%d dataQueries=%d returned=%d elapsed=%s; fresh client connection; HTTP auth/startup/Cron excluded", costOperation, scopeCount, counter.count, returned, time.Since(started).Round(time.Millisecond))
		return
	}
	if profile {
		started := time.Now()
		tx, err := remote.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			fail("profile transaction", err)
		}
		defer tx.Rollback()
		t.Logf("profile BEGIN=%s", time.Since(started).Round(time.Millisecond))
		started = time.Now()
		requested, err := scopes(ctx, tx, viewer, kind, id)
		if err != nil {
			fail("profile scopes", err)
		}
		t.Logf("profile scopes=%s rows=%d", time.Since(started).Round(time.Millisecond), len(requested))
		started = time.Now()
		migrated, err := readAuthority(ctx, tx, viewer)
		if err != nil {
			fail("profile authority", err)
		}
		t.Logf("profile authority=%s", time.Since(started).Round(time.Millisecond))
		started = time.Now()
		encoded, err := matches(ctx, tx, requested)
		if err != nil {
			fail("profile matches", err)
		}
		t.Logf("profile matches=%s", time.Since(started).Round(time.Millisecond))
		var matched []feedMatch
		if err := json.Unmarshal(encoded, &matched); err != nil {
			fail("profile matches decode", err)
		}
		query, args := jsonEntryBatch(matched, migrated, viewer, thinstore.CursorTimestamp(time.Now()), thinstore.FilterAll, nil, 100)
		started = time.Now()
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			fail("profile feed query", err)
		}
		_, _, scanned, err := readEntries(rows, true)
		if err != nil {
			fail("profile feed rows", err)
		}
		t.Logf("profile feed=%s rows=%d", time.Since(started).Round(time.Millisecond), scanned)
		return
	}
	for _, filter := range []thinstore.EntryListFilter{thinstore.FilterAll, thinstore.FilterUnread} {
		var cursor *string
		for page := range 2 {
			readPage := func(ctx context.Context, pool *sql.DB) (thinstore.EntryListResponse, error) {
				var result thinstore.EntryListResponse
				tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
				if err != nil {
					return result, err
				}
				defer tx.Rollback()
				_, _, matched, migrated, err := feedSnapshotWithMatches(ctx, tx, viewer, kind, id)
				if err == nil {
					result, err = listMatchedEntries(ctx, tx, viewer, matched, filter, cursor, 50, true, migrated)
				}
				if err != nil {
					return result, err
				}
				return result, tx.Commit()
			}
			expected, err := readPage(ctx, local)
			if err != nil {
				fail("offline feed reference", err)
			}
			budget, stop := readerQueryContext(ctx, true)
			started := time.Now()
			actual, err := readPage(budget, remote)
			elapsed := time.Since(started)
			stop()
			if err != nil {
				fail("bounded remote feed budget", err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal("hosted page/cursor differs from offline reference; values withheld")
			}
			t.Logf("scopes=%d filter=%s page=%d rows=%d hosted=%s", scopeCount, filter, page, len(actual.Entries), elapsed.Round(time.Millisecond))
			cursor = expected.Cursor
			if cursor == nil {
				break
			}
		}
	}
	for _, phase := range []string{"unread counts", "read-age inventory"} {
		var results [2]any
		for index, pool := range []*sql.DB{local, remote} {
			budget, stop := context.WithTimeout(ctx, 15*time.Second)
			started := time.Now()
			tx, err := pool.BeginTx(budget, &sql.TxOptions{ReadOnly: true})
			if err != nil {
				stop()
				fail(phase, err)
			}
			requested, err := scopes(budget, tx, viewer, kind, id)
			if err == nil && phase == "unread counts" {
				results[index], err = counts(budget, tx, viewer, requested)
			} else if err == nil {
				digest, count := sha256.New(), 0
				err = forEachUnread(budget, tx, viewer, requested, func(uri string, date time.Time) error {
					fmt.Fprintf(digest, "%s\x00%s\n", uri, date.UTC().Format(time.RFC3339Nano))
					count++
					return nil
				})
				results[index] = fmt.Sprintf("%d:%x", count, digest.Sum(nil))
			}
			if err == nil {
				err = tx.Commit()
			} else {
				tx.Rollback()
			}
			stop()
			if err != nil {
				fail(phase, err)
			}
			if index == 1 {
				t.Logf("scopes=%d %s hosted=%s", scopeCount, phase, time.Since(started).Round(time.Millisecond))
			}
		}
		if !reflect.DeepEqual(results[0], results[1]) {
			t.Fatalf("hosted %s differs from offline reference; values withheld", phase)
		}
	}
}
