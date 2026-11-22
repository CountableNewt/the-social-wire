package sqlitedb

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	turso "turso.tech/database/tursogo-serverless"
)

func TestTursoForeignKeySequenceFailureNeverAdmitsOrRetries(t *testing.T) {
	setters, cursors := 0, 0
	client := &http.Client{Transport: tursoRoundTrip(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Requests []struct {
				Type string `json:"type"`
				SQL  string `json:"sql"`
			} `json:"requests"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path == "/v3/cursor" {
			cursors++
			t.Error("failed initializer admitted SQL cursor")
		}
		results := []map[string]any{}
		for _, q := range request.Requests {
			if q.Type == "sequence" {
				setters++
				if q.SQL != "PRAGMA foreign_keys=ON; SELECT 1" {
					t.Error("unexpected initializer sequence")
				}
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private SQL provider message"))}, nil
			}
			results = append(results, map[string]any{"type": "ok", "response": map[string]any{"type": q.Type, "is_autocommit": true}})
		}
		body, _ := json.Marshal(map[string]any{"baton": "retained", "results": results})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	db, err := openTurso(context.Background(), "https://database.turso.io", "private-token", client)
	if db != nil {
		db.Close()
		t.Fatal("failed FK setup admitted database")
	}
	if err == nil || strings.Contains(err.Error(), "private") || setters != 1 || cursors != 0 {
		t.Fatalf("initializer failure must fail closed without leaking or retrying: setters=%d cursors=%d has_error=%t", setters, cursors, err != nil)
	}
}

func TestTursoCommitSequenceProbeFailureDoesNotReplayCommittedStatement(t *testing.T) {
	commits := 0
	auto := true
	client := &http.Client{Transport: tursoRoundTrip(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Requests []struct {
				Type string `json:"type"`
				SQL  string `json:"sql"`
			} `json:"requests"`
		}
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Fatal(e)
		}
		results := []map[string]any{}
		failedProbe := false
		for _, q := range request.Requests {
			if q.Type == "sequence" {
				switch q.SQL {
				case "BEGIN IMMEDIATE; SELECT 1":
					auto = false
				case "COMMIT; SELECT 1":
					commits++
					auto = true
					failedProbe = true
				default:
					t.Error("unexpected control sequence")
				}
			}
			if q.Type == "get_autocommit" && failedProbe {
				results = append(results, map[string]any{"type": "error", "error": map[string]any{"code": "PROBE_FAILED", "message": "private provider SQL"}})
				continue
			}
			results = append(results, map[string]any{"type": "ok", "response": map[string]any{"type": q.Type, "is_autocommit": auto}})
		}
		body, _ := json.Marshal(map[string]any{"baton": "retained", "results": results})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	raw, err := turso.NewConnector("https://database.turso.io", "private-token").WithHTTPClient(client).WithSessionRetention().OpenConnection(context.Background())
	if err != nil {
		t.Fatal("cannot open fake session")
	}
	connection := &tursoConnection{remoteSession: raw}
	tx, err := connection.BeginTx(context.Background(), driver.TxOptions{})
	if err != nil {
		t.Fatal("cannot begin fake transaction")
	}
	err = tx.Commit()
	if err == nil || errors.Is(err, driver.ErrBadConn) || strings.Contains(err.Error(), "private") || connection.IsValid() || commits != 1 {
		t.Fatal("post-commit state-probe failure must invalidate without replay or disclosure")
	}
}
