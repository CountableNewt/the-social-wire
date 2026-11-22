package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupIncludesCommittedWALAndPortableTimestampIndexes(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source #1.sqlite")
	db, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Writer().Exec(`PRAGMA wal_autocheckpoint=0;
		CREATE TABLE parent (id INTEGER PRIMARY KEY);
		CREATE TABLE child (parent_id INTEGER REFERENCES parent(id), at TEXT, at_us INTEGER NOT NULL);
		CREATE INDEX child_at ON child(at);
		CREATE INDEX child_micros ON child(at_us);
		INSERT INTO parent VALUES (1);
		INSERT INTO child VALUES (1, '2026-11-21T10:00:00.000001Z', 1795255200000001);`)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(source + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("expected uncheckpointed WAL: %v", err)
	}
	// A separate uncommitted write must not escape into the snapshot.
	transaction, err := db.Writer().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec("INSERT INTO parent VALUES (2)"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "backup #1.sqlite")
	if err := Backup(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("backup must be private: %v %v", info, err)
	}
	copy, err := sql.Open("sqlite", backupDSN(target))
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var parents int
	if err := copy.QueryRow("SELECT COUNT(*) FROM parent").Scan(&parents); err != nil || parents != 1 {
		t.Fatalf("snapshot contains uncommitted data or lost WAL: %d %v", parents, err)
	}
	var timestamp string
	if err := copy.QueryRow("SELECT at FROM child ORDER BY at_us").Scan(&timestamp); err != nil || timestamp != "2026-11-21T10:00:00.000001Z" {
		t.Fatalf("timestamp index snapshot: %q %v", timestamp, err)
	}
	if _, err := copy.Exec("INSERT INTO parent VALUES (3)"); err == nil {
		t.Fatal("verification connections must be read-only")
	}
}

func TestBackupRefusesExistingDestinationAndSymlinks(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.sqlite")
	db, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	target := filepath.Join(directory, "target.sqlite")
	original := []byte("do not overwrite")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), source, target); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing target must be rejected: %v", err)
	}
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != string(original) {
		t.Fatalf("existing target changed: %s %v", contents, err)
	}
	link := filepath.Join(directory, "target-link.sqlite")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), source, link); !errors.Is(err, os.ErrExist) {
		t.Fatalf("symlink target must be rejected: %v", err)
	}
	linkSource := filepath.Join(directory, "source-link.sqlite")
	if err := os.Symlink(source, linkSource); err != nil {
		t.Fatal(err)
	}
	if err := Backup(context.Background(), linkSource, filepath.Join(directory, "new.sqlite")); err == nil {
		t.Fatal("symlink source must be rejected")
	}
}

func TestBackupFailureCleansOnlyReservedFile(t *testing.T) {
	directory := t.TempDir()
	corrupt := filepath.Join(directory, "corrupt.sqlite")
	if err := os.WriteFile(corrupt, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "backup.sqlite")
	if err := Backup(context.Background(), corrupt, target); err == nil {
		t.Fatal("corrupt source must fail")
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed backup left output behind: %v", err)
	}
	staging, err := filepath.Glob(filepath.Join(directory, ".sqlite-backup-*"))
	if err != nil || len(staging) != 0 {
		t.Fatalf("failed backup left staging data: %v %v", staging, err)
	}
	if err := os.WriteFile(target, []byte("reserved"), 0600); err != nil {
		t.Fatal(err)
	}
	reserved, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(target, target+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	removeReservedBackup(target, reserved)
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "replacement" {
		t.Fatalf("cleanup touched replacement: %q %v", contents, err)
	}
}

func TestBackupRejectsForeignKeyViolations(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "invalid.sqlite")
	db, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Writer().Exec(`PRAGMA foreign_keys=OFF;
		CREATE TABLE parent (id INTEGER PRIMARY KEY);
		CREATE TABLE child (parent_id INTEGER REFERENCES parent(id));
		INSERT INTO child VALUES (99);`)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "backup.sqlite")
	if err := Backup(context.Background(), source, target); err == nil {
		t.Fatal("foreign-key violation must fail verification")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid snapshot retained: %v", err)
	}
}

func TestBackupCancellationAndMissingSourceDoNotCreateFiles(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.sqlite")
	if err := os.WriteFile(source, nil, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := filepath.Join(directory, "backup.sqlite")
	if err := Backup(ctx, source, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled backup created target: %v", err)
	}
	missing := filepath.Join(directory, "missing.sqlite")
	if err := Backup(context.Background(), missing, target); err == nil {
		t.Fatal("missing source must fail")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup created missing source: %v", err)
	}
	if err := Backup(context.Background(), source, "relative.sqlite"); err == nil {
		t.Fatal("relative destination must fail")
	}
}
