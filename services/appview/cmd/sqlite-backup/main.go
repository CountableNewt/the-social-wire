// sqlite-backup creates a verified private snapshot of the standalone database.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func main() {
	os.Exit(runForBackend(os.Getenv("DATABASE_BACKEND"), os.Args[1:], os.Getenv("SQLITE_DB_PATH"), os.Stdout, os.Stderr))
}

func runForBackend(backend string, args []string, source string, stdout, stderr io.Writer) int {
	if backend != "" && backend != "sqlite" {
		fmt.Fprintln(stderr, "sqlite-backup is only for local SQLite; use the remote database backup procedure.")
		return 2
	}
	return run(args, source, stdout, stderr)
}

func run(args []string, source string, stdout, stderr io.Writer) int {
	if len(args) != 1 || !filepath.IsAbs(args[0]) {
		fmt.Fprintln(stderr, "Usage: sqlite-backup /absolute/path/to/new-backup.sqlite")
		return 2
	}
	if source == "" {
		source = "/data/social-wire.sqlite"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := sqlitedb.Backup(ctx, source, args[0]); err != nil {
		if errors.Is(err, os.ErrExist) {
			fmt.Fprintln(stderr, "Backup destination already exists.")
		} else {
			// SQLite and filesystem errors may contain private values.
			fmt.Fprintln(stderr, "Backup integrity verification failed.")
		}
		return 1
	}
	fmt.Fprintf(stdout, "Verified SQLite backup: %s\n", args[0])
	return 0
}
