package drafts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

type ownerLease struct{ owner, token string }
type ownerLeaseKey struct{}

// A request's120s maximum deadline is shorter than its durable180s lease.
// Every destructive object operation and transfer commit checks this ownership.
func (s *Service) acquire(ctx context.Context, owner string) (context.Context, func(), error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ctx, nil, err
	}
	token := hex.EncodeToString(raw[:])
	now := time.Now().UnixMicro()
	held := false
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM private_social_draft_owner_leases WHERE expires_at_us<=?", now); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM private_social_draft_owner_leases").Scan(&count); err != nil {
			return err
		}
		if count >= 8 {
			return errUnavailable
		}
		result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO private_social_draft_owner_leases(owner,token,expires_at_us) VALUES(?,?,?)", owner, token, time.Now().Add(180*time.Second).UnixMicro())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		held = n == 1
		return err
	})
	if err != nil {
		return ctx, nil, err
	}
	if !held {
		return ctx, nil, errUnavailable
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = s.db.Writer().ExecContext(cleanup, "DELETE FROM private_social_draft_transfer_leases WHERE owner=? AND token=?", owner, token)
		_, _ = s.db.Writer().ExecContext(cleanup, "DELETE FROM private_social_draft_owner_leases WHERE owner=? AND token=?", owner, token)
	}
	return context.WithValue(ctx, ownerLeaseKey{}, ownerLease{owner, token}), release, nil
}

type leaseQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Service) checkOwner(ctx context.Context, db leaseQueryer, owner string) error {
	lease, ok := ctx.Value(ownerLeaseKey{}).(ownerLease)
	if !ok || lease.owner != owner {
		return errUnavailable
	}
	var valid bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM private_social_draft_owner_leases WHERE owner=? AND token=? AND expires_at_us>?)", owner, lease.token, time.Now().UnixMicro()).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errUnavailable
	}
	return ctx.Err()
}

// Two whole-file transfers bound temporary storage below two200MiB upload spools.
func (s *Service) acquireTransfer(ctx context.Context, owner string) error {
	lease, ok := ctx.Value(ownerLeaseKey{}).(ownerLease)
	if !ok || lease.owner != owner {
		return errUnavailable
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		now := time.Now().UnixMicro()
		if _, err := tx.ExecContext(ctx, "DELETE FROM private_social_draft_transfer_leases WHERE expires_at_us<=?", now); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM private_social_draft_transfer_leases").Scan(&count); err != nil {
			return err
		}
		if count >= 2 {
			return errUnavailable
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO private_social_draft_transfer_leases(owner,token,expires_at_us) VALUES(?,?,?)", owner, lease.token, time.Now().Add(180*time.Second).UnixMicro())
		return err
	})
}

// Internal tests/helpers can call a transfer directly; runtime routes already
// carry a lease and never reacquire it or bypass its fence.
func (s *Service) ensureTransfer(ctx context.Context, owner string) (context.Context, func(), error) {
	if _, ok := ctx.Value(ownerLeaseKey{}).(ownerLease); ok {
		return ctx, func() {}, s.checkOwner(ctx, s.db.Reader(), owner)
	}
	ctx, release, err := s.acquire(ctx, owner)
	if err != nil {
		return ctx, nil, err
	}
	if err := s.acquireTransfer(ctx, owner); err != nil {
		release()
		return ctx, nil, err
	}
	return ctx, release, nil
}
