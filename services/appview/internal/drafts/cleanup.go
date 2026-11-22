package drafts

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CleanupOnce retires one bounded batch under the same owner lease as draft
// mutations and transfers. The job runner serializes invocations across replicas.
func (s *Service) CleanupOnce(ctx context.Context) error {
	return errors.Join(s.cleanup(ctx, 100), s.cleanupChunks(ctx, 100))
}

func (s *Service) cleanup(ctx context.Context, limit int) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var failure error
	for range limit {
		var owner, id string
		err := s.db.Reader().QueryRowContext(ctx, `SELECT owner,asset_id FROM private_social_draft_cleanup
			WHERE available_at<=? ORDER BY available_at,owner,asset_id LIMIT 1`, nowSeconds()).Scan(&owner, &id)
		if errors.Is(err, sql.ErrNoRows) {
			return failure
		}
		if err != nil {
			return err
		}
		owned, release, err := s.acquire(ctx, owner)
		if err != nil {
			return err
		}
		err = s.cleanupOne(owned, owner, id)
		release()
		if err != nil {
			failure = err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return failure
}

func (s *Service) cleanupOne(ctx context.Context, owner, id string) error {
	var key string
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `UPDATE private_social_draft_assets SET state='deleted'
			WHERE owner=? AND asset_id=?
			AND EXISTS(SELECT 1 FROM private_social_draft_cleanup WHERE owner=? AND asset_id=? AND available_at<=?)
			AND NOT EXISTS(SELECT 1 FROM private_social_draft_bindings WHERE owner=? AND asset_id=?)
			AND NOT EXISTS(SELECT 1 FROM private_article_draft_bindings WHERE owner=? AND asset_id=?)
			RETURNING object_key`, owner, id, owner, id, nowSeconds(), owner, id, owner, id).Scan(&key)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `DELETE FROM private_social_draft_cleanup WHERE owner=? AND asset_id=? AND available_at<=?`, owner, id, nowSeconds())
		}
		return err
	})
	if err != nil || key == "" {
		return err
	}
	if s.objects == nil {
		return errUnavailable
	}
	if err := s.checkOwner(ctx, s.db.Reader(), owner); err != nil {
		return err
	}
	if err = s.objects.Delete(ctx, key); err != nil {
		updateErr := s.db.Write(ctx, func(tx *sql.Tx) error {
			if err := s.checkOwner(ctx, tx, owner); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE private_social_draft_cleanup
				SET available_at=?+min(60,(1 << min(attempts,6)))*60,attempts=min(attempts+1,30)
				WHERE owner=? AND asset_id=?`, nowSeconds(), owner, id)
			return err
		})
		return errors.Join(err, updateErr)
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := s.checkOwner(ctx, tx, owner); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE private_social_draft_assets SET state='purged',metadata=x''
			WHERE owner=? AND asset_id=?`, owner, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM private_social_draft_cleanup WHERE owner=? AND asset_id=?`, owner, id)
		return err
	})
}
