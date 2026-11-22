package reader

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/readstate"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

func migrateRecoveryJobs(ctx context.Context, tx *sql.Tx) error {
	return sqlitedb.ExecAll(tx, `CREATE TABLE IF NOT EXISTS pds_recovery_jobs(
  viewer_did TEXT PRIMARY KEY,status TEXT NOT NULL CHECK(status IN ('pending','running','cooldown')),
  available_at_us INTEGER NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,
  lease_owner TEXT,lease_until_us INTEGER,
  CHECK((status='running' AND lease_owner IS NOT NULL AND lease_until_us IS NOT NULL)
   OR (status!='running' AND lease_owner IS NULL AND lease_until_us IS NULL)))`,
		`CREATE INDEX IF NOT EXISTS pds_recovery_jobs_due ON pds_recovery_jobs(status,available_at_us,lease_until_us)`)
}

func enqueueRecovery(ctx context.Context, db *sqlitedb.DB, viewer string, now time.Time) error {
	if did, err := atproto.NormalizeDID(viewer); err != nil || did != viewer || len(viewer) > 2048 {
		return errors.New("invalid recovery viewer")
	}
	return db.Write(ctx, func(tx *sql.Tx) error {
		var exists, active bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pds_recovery_jobs WHERE viewer_did=?),EXISTS(SELECT 1 FROM pds_recovery_jobs WHERE viewer_did=? AND (status!='cooldown' OR available_at_us>?))", viewer, viewer, now.UnixMicro()).Scan(&exists, &active); err != nil {
			return err
		}
		if !active {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pds_recovery_jobs WHERE status!='cooldown'").Scan(&count); err != nil {
				return err
			}
			if count >= 1000 {
				return errors.New("read-state recovery queue is full")
			}
		}
		if !exists {
			// Only expired cooldown evidence is disposable; never discard active work.
			if _, err := tx.ExecContext(ctx, `DELETE FROM pds_recovery_jobs WHERE viewer_did IN
    (SELECT viewer_did FROM pds_recovery_jobs WHERE status='cooldown' AND available_at_us<=?
     ORDER BY available_at_us DESC LIMIT -1 OFFSET 1000)`, now.UnixMicro()); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO pds_recovery_jobs(viewer_did,status,available_at_us) VALUES(?,'pending',?)
   ON CONFLICT(viewer_did) DO UPDATE SET status='pending',available_at_us=excluded.available_at_us,attempts=0
   WHERE pds_recovery_jobs.status='cooldown' AND pds_recovery_jobs.available_at_us<=excluded.available_at_us`, viewer, now.UnixMicro())
		return err
	})
}

type recoveryClaim struct {
	viewer, owner string
	attempts      int
}

func claimRecovery(ctx context.Context, db *sqlitedb.DB, now time.Time) (*recoveryClaim, error) {
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	owner := hex.EncodeToString(token[:])
	var claim *recoveryClaim
	err := db.Write(ctx, func(tx *sql.Tx) error {
		// Exhausted crashed claims wait for a fresh authenticated request.
		if _, err := tx.ExecContext(ctx, `UPDATE pds_recovery_jobs SET status='cooldown',available_at_us=?,lease_owner=NULL,lease_until_us=NULL
   WHERE status='running' AND lease_until_us<=? AND attempts>=5`, now.Add(30*time.Second).UnixMicro(), now.UnixMicro()); err != nil {
			return err
		}
		var viewer string
		var attempts int
		err := tx.QueryRowContext(ctx, `SELECT viewer_did,attempts FROM pds_recovery_jobs WHERE
   (status='pending' AND available_at_us<=?) OR (status='running' AND lease_until_us<=?)
   ORDER BY available_at_us,viewer_did LIMIT 1`, now.UnixMicro(), now.UnixMicro()).Scan(&viewer, &attempts)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pds_recovery_jobs SET status='running',lease_owner=?,lease_until_us=?,attempts=attempts+1
   WHERE viewer_did=?`, owner, now.Add(45*time.Second).UnixMicro(), viewer); err != nil {
			return err
		}
		claim = &recoveryClaim{viewer, owner, attempts + 1}
		return nil
	})
	return claim, err
}

func finishRecovery(ctx context.Context, db *sqlitedb.DB, claim *recoveryClaim, failure error, now time.Time) error {
	state, delay := "cooldown", readStateRefreshInterval
	if failure != nil {
		delay = readStateRetryInterval
		if claim.attempts < 5 {
			state = "pending"
		}
	}
	result, err := db.ExecContext(ctx, `UPDATE pds_recovery_jobs SET status=?,available_at_us=?,lease_owner=NULL,lease_until_us=NULL
  WHERE viewer_did=? AND status='running' AND lease_owner=? AND lease_until_us>?`, state, now.Add(delay).UnixMicro(), claim.viewer, claim.owner, now.UnixMicro())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return errors.New("read-state recovery lease lost")
	}
	return err
}

// RecoveryWorker repairs only public PDS generations already recorded for an
// authenticated viewer. It stores no viewer OAuth credential in the queue.
type RecoveryWorker struct{ service *service }

func NewRecoveryWorker(db *sqlitedb.DB, repo *atproto.RepoClient) *RecoveryWorker {
	s := &service{db: db, confirmationSlots: make(chan struct{}, 4), verifiedChunks: readstate.NewDefaultVerifiedRecordCache()}
	if repo != nil {
		s.generationFetcher = readstate.NewLiveRecordFetcher(readstate.NewHTTPRecordGetter(repo.HTTPClient(), strictPDSResolver{repo.Resolver()}))
	}
	return &RecoveryWorker{s}
}

func (w *RecoveryWorker) RunOnce(ctx context.Context) error {
	if w == nil || w.service.generationFetcher == nil {
		return errors.New("read-state recovery unavailable")
	}
	claim, err := claimRecovery(ctx, w.service.db, time.Now())
	if err != nil || claim == nil {
		return err
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	status, err := pdsStatus(work, w.service.db.Reader(), claim.viewer)
	if err == nil && status.ManifestCID != nil {
		record, fetchErr := w.service.generationFetcher.Fetch(work, claim.viewer, readstate.ManifestCollection, "self", nil)
		err = fetchErr
		if err == nil && record.CID == "" {
			err = readstate.ErrIncompleteGeneration
		}
		if err == nil && (!status.ProjectionReady || record.CID != *status.ManifestCID) {
			active := &service{db: w.service.db, generationFetcher: w.service.generationFetcher,
				confirmationSlots: w.service.confirmationSlots, verifiedChunks: w.service.verifiedChunks, recoveryClaim: claim}
			_, err = active.confirmGeneration(work, claim.viewer, record.CID, nil)
		}
	}
	cancel()
	finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	return errors.Join(err, finishRecovery(finish, w.service.db, claim, err, time.Now()))
}
