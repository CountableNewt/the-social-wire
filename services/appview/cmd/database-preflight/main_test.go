package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsageAndMissingSnapshotNeverPrintPrivatePaths(t *testing.T) {
	for _, args := range [][]string{nil, {"--kind", "wrong", "--snapshot", "/private-path"}, {"--kind", "appview", "--snapshot", "/missing-private-path.sqlite"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code == 0 || stdout.Len() != 0 || strings.Contains(stderr.String(), "missing-private-path") {
			t.Fatalf("unexpected output: %d %q %q", code, stdout.String(), stderr.String())
		}
	}
}

func TestCommandEmitsMetadataJSONWithoutStoredContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE content_items(id TEXT,body TEXT); INSERT INTO content_items VALUES('entry','secret-token-value'); CREATE TABLE grdb_migrations(identifier TEXT); INSERT INTO grdb_migrations VALUES('migration-v1');`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--kind", "appview", "--snapshot", path}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("command failed: %d %q", code, stderr.String())
	}
	var report struct {
		Kind         string
		SchemaSHA256 string `json:"schemaSha256"`
		Tables       []struct {
			Name string
			Rows int64
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Kind != "appview" || len(report.SchemaSHA256) != 64 || len(report.Tables) != 2 {
		t.Fatalf("bad inventory: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "secret-token-value") || strings.Contains(stdout.String(), path) {
		t.Fatal("command exposed private data")
	}
}
