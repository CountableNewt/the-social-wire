package ingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

const (
	discoveryQueueLimit  = 64
	discoveryMaxAttempts = 5
	discoveryLease       = 30 * time.Second
)

var (
	ErrDiscoveryQueueFull = errors.New("discovery queue is full")
	ErrDiscoveryLeaseLost = errors.New("discovery queue claim expired or was replaced")
)

// MigrateDiscoveryQueue installs the durable queue before any scope is scheduled.
func MigrateDiscoveryQueue(ctx context.Context, db *sqlitedb.DB) error {
	return db.Migrate(ctx, []sqlitedb.Migration{{ID: "durable-public-discovery-queue-v1", Apply: func(tx *sql.Tx) error {
		return sqlitedb.ExecAll(tx, `CREATE TABLE IF NOT EXISTS public_discovery_jobs (
			id TEXT PRIMARY KEY, scope_json TEXT NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('pending','running','failed')),
			attempts INTEGER NOT NULL DEFAULT 0, available_at_us INTEGER NOT NULL,
			lease_owner TEXT, lease_until_us INTEGER, updated_at_us INTEGER NOT NULL,
			CHECK((status='running' AND lease_owner IS NOT NULL AND lease_until_us IS NOT NULL)
				OR (status!='running' AND lease_owner IS NULL AND lease_until_us IS NULL))
		)`, `CREATE INDEX IF NOT EXISTS public_discovery_jobs_claim
			ON public_discovery_jobs(status,available_at_us,lease_until_us,id)`)
	}}})
}

type discoveryQueue struct{ db *sqlitedb.DB }

type discoveryClaim struct {
	id, owner string
	scope     DiscoveryScope
	attempts  int
}

func queueID(scope DiscoveryScope) string {
	digest := sha256.Sum256([]byte(discoveryID(scope)))
	return hex.EncodeToString(digest[:])
}

func (q *discoveryQueue) enqueue(ctx context.Context, scope DiscoveryScope, now time.Time) error {
	if q == nil || q.db == nil {
		return errors.New("discovery queue is not configured")
	}
	encoded, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	nowUS := sqlitedb.TimeMicroseconds(now)
	return q.db.Write(ctx, func(tx *sql.Tx) error {
		var status string
		var available int64
		err := tx.QueryRowContext(ctx, "SELECT status,available_at_us FROM public_discovery_jobs WHERE id=?", queueID(scope)).Scan(&status, &available)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (status != "failed" || available > nowUS) {
			return nil
		}
		var active int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM public_discovery_jobs WHERE status IN ('pending','running')").Scan(&active); err != nil {
			return err
		}
		if active >= discoveryQueueLimit {
			return ErrDiscoveryQueueFull
		}
		// Retain bounded terminal evidence, without allowing exhausted scopes
		// to crowd out new work forever. A failed scope can be requested again
		// only after its one-hour cooldown; no worker automatically revives it.
		if _, err := tx.ExecContext(ctx, `DELETE FROM public_discovery_jobs WHERE id IN (
			SELECT id FROM public_discovery_jobs WHERE status='failed'
			ORDER BY updated_at_us DESC,id LIMIT -1 OFFSET 256)`); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO public_discovery_jobs
			(id,scope_json,status,attempts,available_at_us,updated_at_us) VALUES(?,?,'pending',0,?,?)
			ON CONFLICT(id) DO UPDATE SET scope_json=excluded.scope_json,status='pending',attempts=0,
			available_at_us=excluded.available_at_us,lease_owner=NULL,lease_until_us=NULL,updated_at_us=excluded.updated_at_us`,
			queueID(scope), string(encoded), nowUS, nowUS)
		return err
	})
}

func (q *discoveryQueue) claim(ctx context.Context, now time.Time) (*discoveryClaim, error) {
	if q == nil || q.db == nil {
		return nil, errors.New("discovery queue is not configured")
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	owner := hex.EncodeToString(token[:])
	nowUS := sqlitedb.TimeMicroseconds(now)
	var claimed *discoveryClaim
	var invalidScope error
	err := q.db.Write(ctx, func(tx *sql.Tx) error {
		// A worker that crashed on its final attempt must not leave a claim
		// permanently active or get an unbounded sixth attempt.
		if _, err := tx.ExecContext(ctx, `UPDATE public_discovery_jobs SET status='failed',lease_owner=NULL,
			lease_until_us=NULL,available_at_us=?,updated_at_us=?
			WHERE status='running' AND lease_until_us<=? AND attempts>=?`,
			sqlitedb.TimeMicroseconds(now.Add(time.Hour)), nowUS, nowUS, discoveryMaxAttempts); err != nil {
			return err
		}
		var claim discoveryClaim
		var scopeJSON string
		err := tx.QueryRowContext(ctx, `SELECT id,scope_json,attempts FROM public_discovery_jobs
			WHERE attempts<? AND ((status='pending' AND available_at_us<=?) OR (status='running' AND lease_until_us<=?))
			ORDER BY available_at_us,id LIMIT 1`, discoveryMaxAttempts, nowUS, nowUS).Scan(&claim.id, &scopeJSON, &claim.attempts)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		normalized := DiscoveryScope{}
		if json.Unmarshal([]byte(scopeJSON), &claim.scope) == nil {
			normalized = normalizeDiscoveryScope(claim.scope)
		}
		if normalized.Author == "" || queueID(normalized) != claim.id {
			// Quarantine malformed persisted input without poisoning the head
			// of the queue or attempting discovery outside a validated scope.
			_, err := tx.ExecContext(ctx, `UPDATE public_discovery_jobs SET status='failed',attempts=?,
				lease_owner=NULL,lease_until_us=NULL,available_at_us=?,updated_at_us=? WHERE id=?`,
				discoveryMaxAttempts, sqlitedb.TimeMicroseconds(now.Add(time.Hour)), nowUS, claim.id)
			if err == nil {
				invalidScope = errors.New("invalid persisted discovery scope")
			}
			return err
		}
		claim.scope = normalized
		claim.owner = owner
		claim.attempts++
		_, err = tx.ExecContext(ctx, `UPDATE public_discovery_jobs SET status='running',attempts=?,lease_owner=?,lease_until_us=?,updated_at_us=? WHERE id=?`,
			claim.attempts, owner, sqlitedb.TimeMicroseconds(now.Add(discoveryLease)), nowUS, claim.id)
		if err == nil {
			claimed = &claim
		}
		return err
	})
	return claimed, errors.Join(err, invalidScope)
}

func (q *discoveryQueue) finish(ctx context.Context, claim discoveryClaim, failed bool, now time.Time) (bool, error) {
	nowUS := sqlitedb.TimeMicroseconds(now)
	var result sql.Result
	var err error
	retry := failed && claim.attempts < discoveryMaxAttempts
	if !failed {
		result, err = q.db.Writer().ExecContext(ctx, `DELETE FROM public_discovery_jobs
			WHERE id=? AND status='running' AND lease_owner=? AND lease_until_us>?`, claim.id, claim.owner, nowUS)
	} else {
		status, delay := "pending", time.Second*time.Duration(1<<min(claim.attempts, 8))
		if !retry {
			status, delay = "failed", time.Hour
		}
		result, err = q.db.Writer().ExecContext(ctx, `UPDATE public_discovery_jobs SET status=?,available_at_us=?,
			lease_owner=NULL,lease_until_us=NULL,updated_at_us=?
			WHERE id=? AND status='running' AND lease_owner=? AND lease_until_us>?`,
			status, sqlitedb.TimeMicroseconds(now.Add(delay)), nowUS, claim.id, claim.owner, nowUS)
	}
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count != 1 {
		return false, ErrDiscoveryLeaseLost
	}
	return retry, nil
}
