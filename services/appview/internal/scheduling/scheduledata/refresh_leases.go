package scheduledata

import (
	"context"
	"database/sql"
	"errors"
)

var ErrRefreshUncertain = errors.New("refresh token already used or its outcome is unknown")

// ClaimOAuthRefresh requires both the actor lease and a never-started token
// intent. Started intents survive crashes and successful rotations: the same
// single-use token is never sent again, even after lease expiry or reconnect.
func (s *Store) ClaimOAuthRefresh(ctx context.Context, did, owner, tokenHash string, now, until int64) (bool, error) {
	var held bool
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if owner == "" || tokenHash == "" || until <= now {
			return ErrLeaseLost
		}
		var busy bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM scheduler_actor_leases WHERE purpose='oauth-refresh' AND actor_did=? AND expires_at_us>?)", did, now).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return nil
		}
		var started int
		err := tx.QueryRowContext(ctx, "SELECT request_started FROM oauth_refresh_leases WHERE did=? AND token_hash=?", did, tokenHash).Scan(&started)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if started != 0 {
			return ErrRefreshUncertain
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM scheduler_actor_leases WHERE purpose='oauth-refresh' AND actor_did=? AND expires_at_us<=?", did, now); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO scheduler_actor_leases(purpose,actor_did,owner,expires_at_us) VALUES('oauth-refresh',?,?,?)", did, owner, until)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO oauth_refresh_leases(did,token_hash,owner,expires_at_us,request_started) VALUES(?,?,?,?,0)
    ON CONFLICT(did,token_hash) DO UPDATE SET owner=excluded.owner,expires_at_us=excluded.expires_at_us WHERE request_started=0`, did, tokenHash, owner, until)
		held = err == nil
		return err
	})
	return held, err
}

func (s *Store) BeginOAuthRefresh(ctx context.Context, did, owner, tokenHash string, now int64) error {
	result, err := s.db.Writer().ExecContext(ctx, `UPDATE oauth_refresh_leases SET request_started=1 WHERE did=? AND token_hash=? AND owner=? AND request_started=0 AND expires_at_us>?
  AND EXISTS(SELECT 1 FROM scheduler_actor_leases WHERE purpose='oauth-refresh' AND actor_did=? AND owner=? AND expires_at_us>?)`, did, tokenHash, owner, now, did, owner, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrLeaseLost
	}
	return err
}

// CompleteOAuthRefresh commits rotation while the same lease still fences it.
// The token intent remains permanently started, including on uncertain failures.
func (s *Store) CompleteOAuthRefresh(ctx context.Context, replacement OAuthSession, previousTokenJSON, owner, tokenHash, stamp string, now int64) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		var held bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scheduler_actor_leases l JOIN oauth_refresh_leases r
   ON r.did=l.actor_did AND r.owner=l.owner WHERE l.purpose='oauth-refresh' AND l.actor_did=? AND l.owner=? AND l.expires_at_us>?
   AND r.token_hash=? AND r.request_started=1)`, replacement.DID, owner, now, tokenHash).Scan(&held); err != nil {
			return err
		}
		if !held {
			return ErrLeaseLost
		}
		current, err := s.readSession(ctx, tx, replacement.DID)
		if err != nil {
			return err
		}
		if current == nil {
			return ErrLeaseLost
		}
		if previousTokenJSON != "" && current.TokenJSON == previousTokenJSON && current.DPoPKeyJSON == replacement.DPoPKeyJSON {
			if err := s.writeSession(ctx, tx, replacement, stamp); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM scheduler_actor_leases WHERE purpose='oauth-refresh' AND actor_did=? AND owner=?", replacement.DID, owner)
		return err
	})
}
