package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func TestCommandUsageAndPrivateErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"relative.sqlite"}, {"/one", "/two"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, "private-source", &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "Usage:") {
			t.Fatalf("usage result: %d %q", code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{filepath.Join(t.TempDir(), "new.sqlite")}, "private-secret-source", &stdout, &stderr); code != 1 {
		t.Fatalf("missing source result: %d", code)
	}
	if strings.Contains(stderr.String(), "private-secret-source") || stderr.String() != "Backup integrity verification failed.\n" {
		t.Fatalf("error leaked details: %q", stderr.String())
	}
}

func TestCommandCreatesVerifiedSnapshotWithoutOverwrite(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.sqlite")
	db, err := sqlitedb.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Writer().Exec("CREATE TABLE example (value TEXT); INSERT INTO example VALUES ('snapshot')"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "backup.sqlite")
	var stdout, stderr bytes.Buffer
	if code := run([]string{target}, source, &stdout, &stderr); code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "Verified SQLite backup:") {
		t.Fatalf("backup result: %d %q %q", code, stdout.String(), stderr.String())
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{target}, source, &stdout, &stderr); code != 1 || stderr.String() != "Backup destination already exists.\n" {
		t.Fatalf("existing result: %d %q", code, stderr.String())
	}
	after, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing snapshot changed")
	}
}

func TestRemoteBackendCannotBackUpStaleLocalFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join(t.TempDir(), "must-not-exist.sqlite")
	if code := runForBackend("turso", []string{target}, "stale-local.sqlite", &stdout, &stderr); code != 2 {
		t.Fatalf("remote backup result: %d", code)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("remote backup created local output: %v", err)
	}
}
