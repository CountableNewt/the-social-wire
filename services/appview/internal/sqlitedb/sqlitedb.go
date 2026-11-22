// Package sqlitedb opens the standalone backend's shared SQLite file.
//
// The Swift backend used GRDB DatabasePools: WAL, a five second busy timeout,
// foreign keys on, one writer and concurrent readers. DB keeps those semantics
// with a single-connection writer pool and a multi-connection reader pool so
// every store shares one file without SQLITE_BUSY churn between writers.
package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"runtime"

	_ "modernc.org/sqlite"
)

// DB is a writer/reader pair over one SQLite file.
type DB struct {
	Path   string
	write  *sql.DB
	read   *sql.DB
	remote bool
}

// IsRemote reports whether the store uses Turso HTTP rather than a local file.
func (db *DB) IsRemote() bool { return db.remote }

// Open opens path (creating it when missing) in WAL mode using only builtin SQLite features.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("sqlitedb: empty path")
	}
	write, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxLifetime(0)
	if _, err := write.Exec("PRAGMA journal_mode=WAL"); err != nil {
		write.Close()
		return nil, fmt.Errorf("sqlitedb: enable WAL: %w", err)
	}
	read, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		write.Close()
		return nil, err
	}
	readers := max(4, runtime.NumCPU())
	read.SetMaxOpenConns(readers)
	read.SetMaxIdleConns(readers)
	return &DB{Path: path, write: write, read: read}, nil
}

// OpenMemory opens a private in-memory database for tests. Reads and writes
// share one connection because each in-memory connection is its own database.
func OpenMemory() (*DB, error) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	return &DB{Path: ":memory:", write: db, read: db}, nil
}

func dsn(path string, readOnly bool) string {
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	if readOnly {
		query.Add("_pragma", "query_only(1)")
	} else {
		// BEGIN IMMEDIATE takes the write lock up front, matching GRDB's writer
		// transactions, so a read-then-write transaction cannot deadlock.
		query.Set("_txlock", "immediate")
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

// Writer returns the single-connection pool for statements that modify data.
func (db *DB) Writer() *sql.DB { return db.write }

// Reader returns the read-intent pool. Remote stores share a pool with Writer;
// use Read for an enforced read-only transaction and consistent snapshot.
func (db *DB) Reader() *sql.DB { return db.read }

// Write runs fn inside an immediate write transaction, committing when fn returns nil.
func (db *DB) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	return runTx(ctx, db.write, nil, fn)
}

// Read runs fn inside a read transaction so it sees one consistent snapshot.
func (db *DB) Read(ctx context.Context, fn func(*sql.Tx) error) error {
	return runTx(ctx, db.read, &sql.TxOptions{ReadOnly: true}, fn)
}

func runTx(ctx context.Context, pool *sql.DB, options *sql.TxOptions, fn func(*sql.Tx) error) (err error) {
	tx, err := pool.BeginTx(ctx, options)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Ping verifies both pools can reach the file.
func (db *DB) Ping(ctx context.Context) error {
	var one int
	if err := db.read.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return err
	}
	return db.write.PingContext(ctx)
}

// Close closes both pools.
func (db *DB) Close() error {
	readErr := db.read.Close()
	if db.write == db.read {
		return readErr
	}
	return errors.Join(readErr, db.write.Close())
}
