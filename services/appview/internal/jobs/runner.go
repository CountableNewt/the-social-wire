// Package jobs runs bounded maintenance work under database-owned leases.
package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
	"unicode"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

type Mode string

const (
	Internal Mode = "internal"
	External Mode = "external"
)

func ParseMode(value string) (Mode, error) {
	if value == "" || value == string(Internal) {
		return Internal, nil
	}
	if value == string(External) {
		return External, nil
	}
	return "", errors.New("JOBS_RUNNER_MODE must be internal or external")
}

func Migrate(ctx context.Context, db *sqlitedb.DB) error {
	return db.Migrate(ctx, []sqlitedb.Migration{{ID: "http-job-leases-v1", Apply: func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS http_job_leases (
			name TEXT PRIMARY KEY, owner TEXT NOT NULL, expires_at_us INTEGER NOT NULL)`)
		return err
	}}})
}

type Action struct {
	Run     func(context.Context) error
	Timeout time.Duration
}

type Result struct {
	Job       string `json:"job"`
	Claimed   bool   `json:"claimed"`
	Completed bool   `json:"completed"`
}

type Runner struct {
	db      *sqlitedb.DB
	secret  string
	actions map[string]Action
}

func New(db *sqlitedb.DB, secret string, actions map[string]Action) (*Runner, error) {
	if db == nil {
		return nil, errors.New("maintenance database required")
	}
	if secret != "" && !ValidSecret(secret) {
		return nil, errors.New("JOBS_SECRET must contain 32 to 512 non-whitespace characters")
	}
	copied := make(map[string]Action, len(actions))
	for name, action := range actions {
		if !Known(name) {
			return nil, fmt.Errorf("unknown maintenance job %q", name)
		}
		if action.Run == nil || action.Timeout <= 0 || action.Timeout > 5*time.Minute {
			return nil, fmt.Errorf("invalid maintenance job %q", name)
		}
		copied[name] = action
	}
	return &Runner{db: db, secret: secret, actions: copied}, nil
}

// Known limits job names to the server-defined actions, never arbitrary SQL or paths.
func Known(name string) bool {
	switch name {
	case "scheduling", "drafts-cleanup", "discovery", "ttl-cleanup", "rss-poll", "analytics-cleanup", "author-history", "read-state-recovery":
		return true
	}
	return false
}

func ValidSecret(secret string) bool {
	if len(secret) < 32 || len(secret) > 512 {
		return false
	}
	for _, char := range secret {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return false
		}
	}
	return true
}

var ErrUnavailable = errors.New("maintenance job unavailable")

// Run is shared by interval runners and HTTP triggers. Its lease outlives the
// bounded action, so another instance cannot begin while cancellation unwinds.
func (r *Runner) Run(ctx context.Context, name string) (Result, error) {
	result := Result{Job: name}
	if r == nil {
		return result, ErrUnavailable
	}
	action, ok := r.actions[name]
	if !ok {
		return result, ErrUnavailable
	}
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return result, err
	}
	owner := hex.EncodeToString(token[:])
	now := time.Now()
	claimed, err := r.claim(ctx, name, owner, now, now.Add(action.Timeout+30*time.Second))
	if err != nil || !claimed {
		logFailure(name, "claim", err)
		return result, err
	}
	result.Claimed = true
	defer func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = r.release(release, name, owner)
	}()
	work, cancel := context.WithTimeout(ctx, action.Timeout)
	defer cancel()
	if err := action.Run(work); err != nil {
		logFailure(name, "action", err)
		return result, err
	}
	if err := work.Err(); err != nil {
		logFailure(name, "context", err)
		return result, err
	}
	result.Completed = true
	return result, nil
}

func (r *Runner) claim(ctx context.Context, name, owner string, now, expires time.Time) (bool, error) {
	var acquired bool
	err := r.db.Write(ctx, func(tx *sql.Tx) error {
		var claimed string
		err := tx.QueryRowContext(ctx, `INSERT INTO http_job_leases(name,owner,expires_at_us) VALUES(?,?,?)
			ON CONFLICT(name) DO UPDATE SET owner=excluded.owner,expires_at_us=excluded.expires_at_us
			WHERE http_job_leases.expires_at_us<=? RETURNING owner`, name, owner, expires.UnixMicro(), now.UnixMicro()).Scan(&claimed)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		acquired = err == nil && claimed == owner
		return err
	})
	return acquired, err
}

func (r *Runner) release(ctx context.Context, name, owner string) error {
	return r.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DELETE FROM http_job_leases WHERE name=? AND owner=?", name, owner)
		return err
	})
}
