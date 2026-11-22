package sqlitedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	turso "turso.tech/database/tursogo-serverless"
)

type fakeTursoSession struct {
	remoteSession
	queries   []string
	described []string
	readable  bool
	failure   error
	closed    bool
}

func (s *fakeTursoSession) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.queries = append(s.queries, query)
	return driver.RowsAffected(1), s.failure
}
func (s *fakeTursoSession) DescribeContext(_ context.Context, query string) (*turso.Description, error) {
	s.described = append(s.described, query)
	return &turso.Description{IsReadOnly: s.readable}, nil
}
func (s *fakeTursoSession) Close() error  { s.closed = true; return nil }
func (s *fakeTursoSession) IsValid() bool { return !s.closed }
func (s *fakeTursoSession) PrepareContext(context.Context, string) (driver.Stmt, error) {
	return fakeTursoStatement{}, nil
}

type fakeTursoStatement struct{}

func (fakeTursoStatement) Close() error  { return nil }
func (fakeTursoStatement) NumInput() int { return -1 }
func (fakeTursoStatement) Exec([]driver.Value) (driver.Result, error) {
	panic("must use guarded statement execution")
}
func (fakeTursoStatement) Query([]driver.Value) (driver.Rows, error) {
	panic("must use guarded statement execution")
}

func TestTursoTransactionsKeepImmediateWritesAndRejectAmbiguousReads(t *testing.T) {
	session := &fakeTursoSession{readable: true}
	connection := &tursoConnection{remoteSession: session}
	tx, err := connection.BeginTx(context.Background(), driver.TxOptions{})
	if err != nil || session.queries[0] != "BEGIN IMMEDIATE; SELECT 1" {
		t.Fatalf("write admission: %v %v", session.queries, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = connection.BeginTx(context.Background(), driver.TxOptions{ReadOnly: true})
	if err != nil || session.queries[2] != "BEGIN; SELECT 1" {
		t.Fatalf("read snapshot: %v %v", session.queries, err)
	}
	for _, query := range []string{"SELECT 1; DELETE FROM private_drafts", "COMMIT", "PRAGMA query_only=OFF", "", "SELECT ';'"} {
		before := len(session.queries)
		if _, err := connection.ExecContext(context.Background(), query, nil); err == nil {
			t.Fatalf("ambiguous read accepted: %q", query)
		}
		if len(session.queries) != before {
			t.Fatal("rejected SQL reached server")
		}
	}
	if _, err := connection.ExecContext(context.Background(), "SELECT 1", nil); err != nil {
		t.Fatal(err)
	}
	session.readable = false
	if _, err := connection.ExecContext(context.Background(), "WITH value AS (SELECT 1) DELETE FROM entries", nil); err == nil {
		t.Fatal("server-declared write accepted")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.BeginTx(context.Background(), driver.TxOptions{Isolation: driver.IsolationLevel(sql.LevelReadUncommitted)}); err == nil {
		t.Fatal("unsupported isolation silently ignored")
	}
}

func TestTursoPreparedStatementsRemainGuardedInsideReadonlyTransaction(t *testing.T) {
	session := &fakeTursoSession{readable: false}
	connection := &tursoConnection{remoteSession: session}
	statement, err := connection.PrepareContext(context.Background(), "INSERT INTO entries VALUES (?)")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := connection.BeginTx(context.Background(), driver.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := statement.(driver.StmtExecContext).ExecContext(context.Background(), []driver.NamedValue{{Ordinal: 1, Value: "private-value"}}); err == nil {
		t.Fatal("prepared write bypassed readonly guard")
	}
	if len(session.queries) != 1 {
		t.Fatal("prepared write was executed")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestTursoUnknownCommitIsNotReplayedAndErrorsDoNotExposeServerContent(t *testing.T) {
	session := &fakeTursoSession{}
	connection := &tursoConnection{remoteSession: session}
	tx, err := connection.BeginTx(context.Background(), driver.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	session.failure = errors.New("private-token-and-row-content")
	err = tx.Commit()
	if err == nil || strings.Contains(err.Error(), "private-token") || errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("unsafe commit error: %v", err)
	}
	if !session.closed || connection.IsValid() {
		t.Fatal("uncertain commit connection was reusable")
	}
	if len(session.queries) != 2 {
		t.Fatalf("commit was retried: %v", session.queries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := connection.ExecContext(ctx, "INSERT INTO entries VALUES (?)", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

type tursoRoundTrip func(*http.Request) (*http.Response, error)

func (f tursoRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenTursoUsesSinglePoolSeparateCredentialsAndVerifiedForeignKeys(t *testing.T) {
	for _, foreignKeys := range []string{"1", "0"} {
		t.Run(foreignKeys, func(t *testing.T) {
			var queries []string
			client := &http.Client{Transport: tursoRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "database.turso.io" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer private-token" {
					t.Error("credential or origin mismatch")
				}
				var request struct {
					Requests []struct {
						Type string `json:"type"`
						SQL  string `json:"sql"`
					}
					Batch struct {
						Steps []struct {
							Stmt struct {
								SQL string `json:"sql"`
							} `json:"stmt"`
						} `json:"steps"`
					} `json:"batch"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				body := ""
				if r.URL.Path == "/v3/pipeline" {
					results := []map[string]any{}
					for _, q := range request.Requests {
						if q.Type == "sequence" {
							queries = append(queries, q.SQL)
						}
						response := map[string]any{"type": q.Type}
						if q.Type == "get_autocommit" {
							response["is_autocommit"] = true
						}
						results = append(results, map[string]any{"type": "ok", "response": response})
					}
					encoded, _ := json.Marshal(map[string]any{"baton": "retained", "results": results})
					body = string(encoded)
				} else {
					query := request.Batch.Steps[0].Stmt.SQL
					queries = append(queries, query)
					value := "1"
					if query == "PRAGMA foreign_keys" {
						value = foreignKeys
					}
					body = `{"baton":"retained"}` + "\n" + `{"type":"step_begin","step":0,"cols":[{"name":"value","decltype":"INTEGER"}]}` + "\n" + `{"type":"row","row":[{"type":"integer","value":"` + value + `"}]}` + "\n" + `{"type":"step_end","affected_row_count":0}` + "\n" + `{"type":"step_begin","step":1,"cols":[]}` + "\n" + `{"type":"step_end","affected_row_count":0}` + "\n"
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			suppliedTransport := &tursoCustomTransport{RoundTripper: client.Transport}
			client.Transport = suppliedTransport
			db, err := openTurso(context.Background(), "turso://database.turso.io", "private-token", client)
			if client.Transport != suppliedTransport || client.CheckRedirect != nil {
				t.Fatal("caller-supplied HTTP client was mutated")
			}
			if foreignKeys == "0" {
				if err == nil {
					db.Close()
					t.Fatal("missing FK enforcement accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if !db.IsRemote() || db.Reader() != db.Writer() || db.Path != "" {
				t.Fatal("remote store retained a local file/read pool")
			}
			if len(queries) < 3 || queries[0] != "PRAGMA foreign_keys=ON; SELECT 1" || queries[1] != "PRAGMA foreign_keys" {
				t.Fatalf("FK setup missing: %v", queries)
			}
		})
	}
}

func TestTursoEndpointAndTransportFailClosed(t *testing.T) {
	for _, endpoint := range []string{"http://database.turso.io", "https://user:token@database.turso.io", "https://database.turso.io?token=secret", "https://database.turso.io/path", "libsql://old.turso.io"} {
		if db, err := OpenTurso(context.Background(), endpoint, "secret"); err == nil {
			db.Close()
			t.Fatalf("invalid endpoint accepted: %s", endpoint)
		}
	}
	called := false
	transport := &tursoTransport{origin: "database.turso.io", next: tursoRoundTrip(func(*http.Request) (*http.Response, error) { called = true; return nil, nil })}
	request, _ := http.NewRequest(http.MethodPost, "https://another.turso.io/v3/pipeline", nil)
	if _, err := transport.RoundTrip(request); err == nil || called {
		t.Fatal("credential-bearing request crossed origins")
	}
}

func TestExpiredRetainedSessionIsReplacedBeforeMutation(t *testing.T) {
	var opened, staleSQL, mutations int
	expiredSession := ""
	client := &http.Client{Transport: tursoRoundTrip(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Baton    *string `json:"baton"`
			Requests []struct {
				Type string `json:"type"`
			} `json:"requests"`
			Batch struct {
				Steps []struct {
					Stmt struct {
						SQL string `json:"sql"`
					} `json:"stmt"`
				} `json:"steps"`
			} `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		baton := ""
		if request.Baton == nil {
			opened++
			baton = fmt.Sprintf("session-%d", opened)
		} else {
			baton = *request.Baton
		}
		closing := len(request.Requests) > 0 && request.Requests[0].Type == "close"
		if !closing && baton == expiredSession {
			staleSQL++
			return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"expired synthetic session"}`))}, nil
		}
		var body string
		if r.URL.Path == "/v3/pipeline" {
			results := []map[string]any{}
			for _, q := range request.Requests {
				response := map[string]any{"type": q.Type}
				if q.Type == "get_autocommit" {
					response["is_autocommit"] = true
				}
				results = append(results, map[string]any{"type": "ok", "response": response})
			}
			encoded, _ := json.Marshal(map[string]any{"baton": baton, "results": results})
			body = string(encoded)
		} else {
			if strings.HasPrefix(request.Batch.Steps[0].Stmt.SQL, "INSERT") {
				mutations++
			}
			body = fmt.Sprintf(`{"baton":%q}`, baton) + "\n" + `{"type":"step_begin","step":0,"cols":[{"name":"value","decltype":"INTEGER"}]}` + "\n" + `{"type":"row","row":[{"type":"integer","value":"1"}]}` + "\n" + `{"type":"step_end","affected_row_count":1}` + "\n" + `{"type":"step_begin","step":1,"cols":[]}` + "\n" + `{"type":"step_end","affected_row_count":0}` + "\n"
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	clock := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	db, err := openTursoWithIdlePolicy(t.Context(), "https://database.turso.io", "private-token", client, 15*time.Second, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Less than the idle-cleaner minimum tick: replacement must happen on checkout.
	clock = clock.Add(16 * time.Second)
	expiredSession = fmt.Sprintf("session-%d", opened)
	if _, err := db.Writer().ExecContext(t.Context(), "INSERT INTO synthetic VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	if staleSQL != 0 || mutations != 1 || opened < 2 || db.Writer().Stats().MaxLifetimeClosed != 0 {
		t.Fatalf("stale session reached SQL or mutation replayed: stale=%d mutations=%d opened=%d", staleSQL, mutations, opened)
	}
}
