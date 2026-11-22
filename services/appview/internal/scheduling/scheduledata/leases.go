package scheduledata

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrLeaseLost = errors.New("scheduling lease is no longer owned")

// ClaimActorLease serializes a bounded operation across every scheduler instance.
func (s *Store) ClaimActorLease(ctx context.Context, purpose, actor, owner string, now, until int64, limit int) (bool, error) {
	if owner == "" || until <= now {
		return false, ErrLeaseLost
	}
	var held bool
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM scheduler_actor_leases WHERE expires_at_us<=?", now); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM scheduler_actor_leases WHERE purpose=?", purpose).Scan(&count); err != nil {
			return err
		}
		if limit > 0 && count >= limit {
			return nil
		}
		result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO scheduler_actor_leases(purpose,actor_did,owner,expires_at_us) VALUES(?,?,?,?)", purpose, actor, owner, until)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		held = n == 1
		return err
	})
	return held, err
}

func (s *Store) RenewActorLease(ctx context.Context, purpose, actor, owner string, now, until int64) error {
	if owner == "" || until <= now {
		return ErrLeaseLost
	}
	result, err := s.db.Writer().ExecContext(ctx, "UPDATE scheduler_actor_leases SET expires_at_us=? WHERE purpose=? AND actor_did=? AND owner=? AND expires_at_us>?", until, purpose, actor, owner, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrLeaseLost
	}
	return err
}

func (s *Store) CheckActorLease(ctx context.Context, purpose, actor, owner string, now int64) error {
	var held bool
	if err := s.db.Reader().QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM scheduler_actor_leases WHERE purpose=? AND actor_did=? AND owner=? AND expires_at_us>?)", purpose, actor, owner, now).Scan(&held); err != nil {
		return err
	}
	if !held {
		return ErrLeaseLost
	}
	return nil
}

func (s *Store) ReleaseActorLease(ctx context.Context, purpose, actor, owner string) error {
	_, err := s.db.Writer().ExecContext(ctx, "DELETE FROM scheduler_actor_leases WHERE purpose=? AND actor_did=? AND owner=?", purpose, actor, owner)
	return err
}

// Create-owned writes check the fence inside the same database transaction.
// A paused owner cannot resume and enqueue work after its lease was replaced.
func (s *Store) RememberScheduleSubmissionOwned(ctx context.Context, did, rkey, owner, stamp string) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := checkCreateOwner(ctx, tx, did, owner); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO pending_schedule_submissions(did,rkey,next_check_at,created_at) VALUES(?,?,?,?)", did, rkey, stamp, stamp)
		return err
	})
}
func (s *Store) InsertScheduleJobIfMissingOwned(ctx context.Context, job ScheduledJob, owner, stamp string) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := checkCreateOwner(ctx, tx, job.DID, owner); err != nil {
			return err
		}
		if err := saveJob(ctx, tx, job, stamp, false); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM pending_schedule_submissions WHERE did=? AND rkey=?", job.DID, job.RKey)
		return err
	})
}
func checkCreateOwner(ctx context.Context, tx *sql.Tx, did, owner string) error {
	var held bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM scheduler_actor_leases WHERE purpose='create' AND actor_did=? AND owner=? AND expires_at_us>?)", did, owner, time.Now().UnixMicro()).Scan(&held); err != nil {
		return err
	}
	if !held {
		return ErrLeaseLost
	}
	return nil
}
