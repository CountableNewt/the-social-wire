package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"modernc.org/sqlite"
)

// Backup creates a new private snapshot without opening the source for writes.
// The pager API includes committed WAL pages and observes one consistent SQLite
// snapshot. The destination must be absolute and must not already exist.
func Backup(ctx context.Context, source, destination string) (err error) {
	if !filepath.IsAbs(destination) {
		return errors.New("backup destination must be absolute")
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	original, err := os.Lstat(source)
	if err != nil || !original.Mode().IsRegular() {
		return errors.New("source must be an existing regular file, not a symlink")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	reserved, err := output.Stat()
	if err != nil {
		output.Close()
		return err
	}
	verified := false
	defer func() {
		err = errors.Join(err, output.Close())
		if !verified || err != nil {
			removeReservedBackup(destination, reserved)
		}
	}()

	// SQLite opens its destination by pathname. A private staging directory keeps
	// that open separate from the user-visible destination, whose descriptor stays
	// held throughout. Replacing the public path never redirects our writes.
	staging, err := os.MkdirTemp(filepath.Dir(destination), ".sqlite-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	snapshot := filepath.Join(staging, "snapshot.sqlite")
	if err := backupSnapshot(ctx, source, original, snapshot); err != nil {
		return err
	}
	if err := verifyBackup(ctx, snapshot); err != nil {
		return err
	}
	input, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, backupReader{ctx, input})
	if err := errors.Join(copyErr, input.Close()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := output.Chmod(0600); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	current, err := os.Lstat(destination)
	if err != nil || !os.SameFile(current, reserved) {
		return errors.New("backup destination was replaced")
	}
	verified = true
	return nil
}

func backupSnapshot(ctx context.Context, source string, original os.FileInfo, destination string) error {
	db, err := sql.Open("sqlite", backupDSN(source))
	if err != nil {
		return err
	}
	defer db.Close()
	connection, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	current, err := os.Lstat(source)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(original, current) {
		return errors.New("backup source was replaced")
	}
	return connection.Raw(func(raw any) (err error) {
		backup, err := raw.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		}).NewBackup(destination)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, backup.Finish()) }()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(256)
			if err != nil {
				var sqliteError *sqlite.Error
				if !errors.As(err, &sqliteError) || (sqliteError.Code()&255 != 5 && sqliteError.Code()&255 != 6) {
					return err
				}
				// A concurrent writer may briefly hold the source. Retry only
				// BUSY/LOCKED and keep the overall deadline authoritative.
				timer := time.NewTimer(100 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
				continue
			}
			if !more {
				return nil
			}
		}
	})
}

func verifyBackup(ctx context.Context, snapshot string) error {
	db, err := sql.Open("sqlite", backupDSN(snapshot))
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil || result != "ok" {
			rows.Close()
			return errors.New("backup integrity verification failed")
		}
		count++
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("backup integrity verification failed")
	}
	rows, err = db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("backup foreign-key verification failed")
	}
	return rows.Err()
}

func backupDSN(path string) string {
	uri := url.URL{Scheme: "file", Path: path}
	query := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(100)", "query_only(1)"}}
	uri.RawQuery = query.Encode()
	return uri.String()
}

func removeReservedBackup(path string, reserved os.FileInfo) {
	if current, err := os.Lstat(path); err == nil && os.SameFile(current, reserved) {
		_ = os.Remove(path)
	}
}

type backupReader struct {
	ctx context.Context
	io.Reader
}

func (r backupReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(buffer)
}
