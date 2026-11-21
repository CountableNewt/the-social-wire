package operations

import (
	"context"
	"database/sql"
	"time"
)

// AcquireLeaderLease takes or re-takes a named lease. It returns nil when
// another owner or generation holds an unexpired lease, or when another owner
// is inside a fenced operation on it. A takeover increments the fencing token;
// re-acquisition by the same owner and generation keeps it.
func (s *Store) AcquireLeaderLease(ctx context.Context, name, sourceGeneration, ownerID string, leaseUntil, at time.Time) (*LeaderLease, error) {
	if !leaseUntil.After(at) {
		return nil, ErrInvalidProgress
	}
	bounded := boundedLeaseName(name)
	s.mu.Lock()
	fence, fenced := s.fences[bounded]
	s.mu.Unlock()
	if fenced && fence.ownerID != ownerID {
		return nil, nil
	}
	var lease *LeaderLease
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		var (
			exists                        bool
			owner, generation, acquiredAt string
			expiresAt                     string
			released                      sql.NullString
			token                         int64
		)
		err := tx.QueryRowContext(ctx, `SELECT owner_id, source_generation, fencing_token, acquired_at,
  lease_expires_at, released_at FROM appview_ingestion_leases WHERE environment = ? AND lease_name = ?`,
			s.environment, bounded).Scan(&owner, &generation, &token, &acquiredAt, &expiresAt, &released)
		switch {
		case err == sql.ErrNoRows:
		case err != nil:
			return err
		default:
			exists = true
		}
		expiry, expiryOK := parseStoreTime(expiresAt)
		live := exists && !released.Valid && expiryOK && expiry.After(at)
		if live && (owner != ownerID || generation != sourceGeneration) {
			return nil
		}
		sameLease := live && owner == ownerID && generation == sourceGeneration
		newToken := token + 1
		newAcquired := storeTime(at)
		if sameLease {
			newToken = token
			newAcquired = acquiredAt
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO appview_ingestion_leases
  (environment, lease_name, source_generation, owner_id, fencing_token, acquired_at,
   lease_expires_at, released_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?)
ON CONFLICT (environment, lease_name) DO UPDATE SET
  source_generation = excluded.source_generation, owner_id = excluded.owner_id,
  fencing_token = excluded.fencing_token, acquired_at = excluded.acquired_at,
  lease_expires_at = excluded.lease_expires_at, released_at = NULL,
  updated_at = excluded.updated_at`,
			s.environment, bounded, sourceGeneration, ownerID, newToken, newAcquired,
			storeTime(leaseUntil), storeTime(at)); err != nil {
			return err
		}
		lease, err = s.readLease(ctx, tx, bounded)
		if err == nil && lease == nil {
			err = ErrMissingCreatedRecord
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

// RenewLeaderLease extends a live lease the caller still owns, else ErrLeaseConflict.
func (s *Store) RenewLeaderLease(ctx context.Context, name, ownerID string, fencingToken int64, leaseUntil, at time.Time) (*LeaderLease, error) {
	if !leaseUntil.After(at) {
		return nil, ErrInvalidProgress
	}
	bounded := boundedLeaseName(name)
	var lease *LeaderLease
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
UPDATE appview_ingestion_leases SET lease_expires_at = ?, updated_at = ?
WHERE environment = ? AND lease_name = ? AND owner_id = ? AND fencing_token = ?
  AND released_at IS NULL AND lease_expires_at >= ?`,
			storeTime(leaseUntil), storeTime(at), s.environment, bounded, ownerID, fencingToken, storeTime(at))
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return ErrLeaseConflict
		}
		lease, err = s.readLease(ctx, tx, bounded)
		if err == nil && lease == nil {
			err = ErrLeaseConflict
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

// ReleaseLeaderLease releases a lease the caller owns. It fails with
// ErrLeaseConflict while the same owner and token are inside a fenced operation,
// or when no unreleased matching row exists.
func (s *Store) ReleaseLeaderLease(ctx context.Context, name, ownerID string, fencingToken int64, at time.Time) error {
	bounded := boundedLeaseName(name)
	s.mu.Lock()
	fence, fenced := s.fences[bounded]
	s.mu.Unlock()
	if fenced && fence.ownerID == ownerID && fence.fencingToken == fencingToken {
		return ErrLeaseConflict
	}
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
UPDATE appview_ingestion_leases SET released_at = ?, updated_at = ?
WHERE environment = ? AND lease_name = ? AND owner_id = ? AND fencing_token = ?
  AND released_at IS NULL`,
			storeTime(at), storeTime(at), s.environment, bounded, ownerID, fencingToken)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return ErrLeaseConflict
		}
		return nil
	})
}

// WithLeaderLeaseFence verifies the caller still holds a live lease, then runs
// operation while the lease cannot be released or taken over by another owner
// in this process. A stale owner gets ErrLeaseConflict and never runs operation.
func (s *Store) WithLeaderLeaseFence(ctx context.Context, name, ownerID string, fencingToken int64, at time.Time, operation func(context.Context) error) error {
	bounded := boundedLeaseName(name)
	valid := false
	if err := s.db.Read(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM appview_ingestion_leases
  WHERE environment = ? AND lease_name = ? AND owner_id = ? AND fencing_token = ?
    AND released_at IS NULL AND lease_expires_at >= ?)`,
			s.environment, bounded, ownerID, fencingToken, storeTime(at)).Scan(&valid)
	}); err != nil {
		return err
	}
	if !valid {
		return ErrLeaseConflict
	}
	s.mu.Lock()
	if existing, ok := s.fences[bounded]; ok {
		if existing.ownerID != ownerID || existing.fencingToken != fencingToken {
			s.mu.Unlock()
			return ErrLeaseConflict
		}
		existing.count++
		s.fences[bounded] = existing
	} else {
		s.fences[bounded] = leaseFence{ownerID: ownerID, fencingToken: fencingToken, count: 1}
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if existing, ok := s.fences[bounded]; ok {
			if existing.count == 1 {
				delete(s.fences, bounded)
			} else {
				existing.count--
				s.fences[bounded] = existing
			}
		}
	}()
	return operation(ctx)
}

func (s *Store) readLease(ctx context.Context, tx *sql.Tx, bounded string) (*LeaderLease, error) {
	var (
		lease                          LeaderLease
		acquiredAt, expiresAt, updated string
	)
	err := tx.QueryRowContext(ctx, `SELECT environment, lease_name, source_generation, owner_id,
  fencing_token, acquired_at, lease_expires_at, updated_at
FROM appview_ingestion_leases WHERE environment = ? AND lease_name = ?`, s.environment, bounded).Scan(
		&lease.Environment, &lease.Name, &lease.SourceGeneration, &lease.OwnerID, &lease.FencingToken,
		&acquiredAt, &expiresAt, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ok [3]bool
	lease.AcquiredAt, ok[0] = parseStoreTime(acquiredAt)
	lease.ExpiresAt, ok[1] = parseStoreTime(expiresAt)
	lease.UpdatedAt, ok[2] = parseStoreTime(updated)
	if ok != [3]bool{true, true, true} {
		return nil, nil
	}
	return &lease, nil
}
