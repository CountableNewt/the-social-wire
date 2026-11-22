package ingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

const historyLease = 90 * time.Second

var ErrAuthorHistoryLeaseLost = errors.New("author history claim expired or was replaced")
var ErrAuthorHistoryQueueFull = errors.New("author history queue is full")
var errHistoryCursorLoop = errors.New("author history cursor repeated")

func MigrateAuthorHistoryQueue(ctx context.Context, db *sqlitedb.DB) error {
	return db.Migrate(ctx, []sqlitedb.Migration{{ID: "durable-author-history-v1", Apply: func(tx *sql.Tx) error {
		return sqlitedb.ExecAll(tx, `CREATE TABLE IF NOT EXISTS author_history_jobs(
   author_did TEXT PRIMARY KEY,status TEXT NOT NULL CHECK(status IN ('pending','running','cooldown','failed')),
   collection_index INTEGER NOT NULL DEFAULT 0,cursor TEXT NOT NULL DEFAULT '',attempts INTEGER NOT NULL DEFAULT 0,
   available_at_us INTEGER NOT NULL,lease_owner TEXT,lease_until_us INTEGER,updated_at_us INTEGER NOT NULL,
   CHECK(collection_index>=0 AND collection_index<=2),
   CHECK((status='running' AND lease_owner IS NOT NULL AND lease_until_us IS NOT NULL) OR
    (status!='running' AND lease_owner IS NULL AND lease_until_us IS NULL)))`,
			`CREATE INDEX IF NOT EXISTS author_history_claim ON author_history_jobs(status,available_at_us,lease_until_us,author_did)`,
			`CREATE TABLE IF NOT EXISTS author_history_cursors(author_did TEXT NOT NULL REFERENCES author_history_jobs(author_did) ON DELETE CASCADE,
    collection_index INTEGER NOT NULL,cursor_hash TEXT NOT NULL,PRIMARY KEY(author_did,collection_index,cursor_hash))`)
	}}})
}

type historyQueue struct{ db *sqlitedb.DB }
type historyClaim struct {
	author, owner, cursor string
	collection            int
}

func (q historyQueue) enqueue(ctx context.Context, author string, now time.Time) error {
	if q.db == nil {
		return errors.New("durable author history queue is not configured")
	}
	stamp := sqlitedb.TimeMicroseconds(now)
	return q.db.Write(ctx, func(tx *sql.Tx) error {
		var status string
		var available int64
		err := tx.QueryRowContext(ctx, "SELECT status,available_at_us FROM author_history_jobs WHERE author_did=?", author).Scan(&status, &available)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (status == "pending" || status == "running" || available > stamp) {
			return nil
		}
		var active int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM author_history_jobs WHERE status IN ('pending','running')").Scan(&active); err != nil {
			return err
		}
		if active >= 32 {
			return ErrAuthorHistoryQueueFull
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM author_history_jobs WHERE author_did IN
   (SELECT author_did FROM author_history_jobs WHERE status IN ('cooldown','failed') ORDER BY updated_at_us DESC,author_did LIMIT -1 OFFSET 512)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM author_history_cursors WHERE author_did=?", author); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO author_history_jobs(author_did,status,available_at_us,updated_at_us) VALUES(?,'pending',?,?)
   ON CONFLICT(author_did) DO UPDATE SET status='pending',collection_index=0,cursor='',attempts=0,available_at_us=excluded.available_at_us,
   lease_owner=NULL,lease_until_us=NULL,updated_at_us=excluded.updated_at_us`, author, stamp, stamp)
		return err
	})
}

func (q historyQueue) claim(ctx context.Context, now time.Time) (*historyClaim, error) {
	if q.db == nil {
		return nil, errors.New("durable author history queue is not configured")
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	owner := hex.EncodeToString(token[:])
	stamp := sqlitedb.TimeMicroseconds(now)
	var result *historyClaim
	var invalid error
	err := q.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE author_history_jobs SET status='failed',lease_owner=NULL,lease_until_us=NULL,available_at_us=?,updated_at_us=?
   WHERE status='running' AND lease_until_us<=? AND attempts>=5`, sqlitedb.TimeMicroseconds(now.Add(time.Hour)), stamp, stamp); err != nil {
			return err
		}
		var claim historyClaim
		err := tx.QueryRowContext(ctx, `SELECT author_did,collection_index,cursor FROM author_history_jobs WHERE attempts<5 AND
   ((status='pending' AND available_at_us<=?) OR (status='running' AND lease_until_us<=?)) ORDER BY available_at_us,author_did LIMIT 1`, stamp, stamp).
			Scan(&claim.author, &claim.collection, &claim.cursor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if normalizeHistoryAuthor(claim.author) != claim.author || normalizeHistoryAuthor(claim.author) == "" || claim.collection < 0 || claim.collection > len(documentCollections) || len(claim.cursor) > 2048 {
			_, err := tx.ExecContext(ctx, `UPDATE author_history_jobs SET status='failed',attempts=5,lease_owner=NULL,lease_until_us=NULL,
    available_at_us=?,updated_at_us=? WHERE author_did=?`, sqlitedb.TimeMicroseconds(now.Add(time.Hour)), stamp, claim.author)
			if err == nil {
				invalid = errors.New("invalid persisted author history work")
			}
			return err
		}
		claim.owner = owner
		_, err = tx.ExecContext(ctx, `UPDATE author_history_jobs SET status='running',attempts=attempts+1,lease_owner=?,lease_until_us=?,updated_at_us=? WHERE author_did=?`, owner, sqlitedb.TimeMicroseconds(now.Add(historyLease)), stamp, claim.author)
		if err == nil {
			result = &claim
		}
		return err
	})
	return result, errors.Join(err, invalid)
}

// A checkpoint advances only after an entire page has been positively indexed.
// Replaying a page after a crash is safe; skipping a partially indexed page is not.
func (q historyQueue) checkpoint(ctx context.Context, claim *historyClaim, collection int, cursor string, now time.Time) error {
	if collection < 0 || collection > len(documentCollections) || len(cursor) > 2048 {
		return errors.New("invalid author history checkpoint")
	}
	stamp := sqlitedb.TimeMicroseconds(now)
	return q.db.Write(ctx, func(tx *sql.Tx) error {
		if err := historyOwned(ctx, tx, *claim, stamp); err != nil {
			return err
		}
		if cursor != "" {
			hash := sha256.Sum256([]byte(cursor))
			result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO author_history_cursors(author_did,collection_index,cursor_hash) VALUES(?,?,?)", claim.author, collection, hex.EncodeToString(hash[:]))
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return errHistoryCursorLoop
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE author_history_jobs SET collection_index=?,cursor=?,attempts=0,updated_at_us=? WHERE author_did=? AND lease_owner=?`, collection, cursor, stamp, claim.author, claim.owner)
		if err == nil {
			claim.collection, claim.cursor = collection, cursor
		}
		return err
	})
}

func historyOwned(ctx context.Context, tx *sql.Tx, claim historyClaim, now int64) error {
	var held bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM author_history_jobs WHERE author_did=? AND status='running' AND lease_owner=? AND lease_until_us>?)", claim.author, claim.owner, now).Scan(&held); err != nil {
		return err
	}
	if !held {
		return ErrAuthorHistoryLeaseLost
	}
	return nil
}

func (q historyQueue) finish(ctx context.Context, claim historyClaim, completed, failed bool, now time.Time) (bool, error) {
	stamp := sqlitedb.TimeMicroseconds(now)
	retry := false
	err := q.db.Write(ctx, func(tx *sql.Tx) error {
		if err := historyOwned(ctx, tx, claim, stamp); err != nil {
			return err
		}
		status, available := "pending", stamp
		if completed {
			status, available = "cooldown", sqlitedb.TimeMicroseconds(now.Add(6*time.Hour))
		}
		if failed {
			var attempts int
			if err := tx.QueryRowContext(ctx, "SELECT attempts FROM author_history_jobs WHERE author_did=?", claim.author).Scan(&attempts); err != nil {
				return err
			}
			attempts = max(1, attempts)
			status = "pending"
			delay := time.Minute * time.Duration(1<<min(attempts-1, 6))
			retry = attempts < 5
			if !retry {
				status, delay = "failed", time.Hour
			}
			available = sqlitedb.TimeMicroseconds(now.Add(delay))
			if _, err := tx.ExecContext(ctx, "UPDATE author_history_jobs SET attempts=? WHERE author_did=?", attempts, claim.author); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE author_history_jobs SET status=?,available_at_us=?,lease_owner=NULL,lease_until_us=NULL,updated_at_us=? WHERE author_did=? AND lease_owner=?`, status, available, stamp, claim.author, claim.owner)
		return err
	})
	return retry, err
}
