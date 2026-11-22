package scheduledata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
)

type ScheduledJob struct {
	DID                  string                    `json:"did"`
	RKey                 string                    `json:"rkey"`
	ScheduledAt          string                    `json:"scheduledAt"`
	Status               scheduling.ScheduleStatus `json:"status"`
	Attempts             int                       `json:"attempts"`
	LastError            *scheduling.ScheduleError `json:"lastError,omitempty"`
	NextAttemptAt        *string                   `json:"nextAttemptAt,omitempty"`
	LastAttemptAt        *string                   `json:"lastAttemptAt,omitempty"`
	PublishRKey          string                    `json:"publishRkey"`
	RecordType           string                    `json:"recordType"`
	PublishedURI         *string                   `json:"publishedUri,omitempty"`
	PublishedCID         *string                   `json:"publishedCid,omitempty"`
	DependsOnScheduleURI *string                   `json:"dependsOnScheduleUri,omitempty"`
	ParentPublishedURI   *string                   `json:"parentPublishedUri,omitempty"`
}

type JobIdentity struct{ DID, RKey string }

const jobColumns = `did,rkey,COALESCE(scheduled_at,scheduled_for) AS scheduled_at,status,attempts,
	last_error,next_attempt_at,last_attempt_at,COALESCE(publish_rkey,rkey),COALESCE(record_type,'app.bsky.feed.post'),
	published_uri,published_cid,depends_on_schedule_uri,parent_published_uri`

type rowScanner interface{ Scan(...any) error }

func scanJob(row rowScanner) (ScheduledJob, error) {
	var job ScheduledJob
	var rawStatus string
	var lastError *string
	err := row.Scan(&job.DID, &job.RKey, &job.ScheduledAt, &rawStatus, &job.Attempts, &lastError, &job.NextAttemptAt,
		&job.LastAttemptAt, &job.PublishRKey, &job.RecordType, &job.PublishedURI, &job.PublishedCID,
		&job.DependsOnScheduleURI, &job.ParentPublishedURI)
	if err != nil {
		return job, err
	}
	job.Status = scheduling.ScheduleStatus(rawStatus)
	if rawStatus == "cancelled" {
		job.Status = scheduling.StatusCanceled
	}
	if lastError != nil {
		var value scheduling.ScheduleError
		if json.Unmarshal([]byte(*lastError), &value) == nil {
			job.LastError = &value
		}
	}
	return job, nil
}

type jobsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func queryJobs(ctx context.Context, db jobsQueryer, predicate string, args ...any) ([]ScheduledJob, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+jobColumns+" FROM scheduled_jobs "+predicate, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []ScheduledJob{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

const insertJob = `INSERT INTO scheduled_jobs
	(did,rkey,scheduled_for,scheduled_at,status,attempts,last_error,next_attempt_at,last_attempt_at,publish_rkey,
	record_type,published_uri,published_cid,depends_on_schedule_uri,parent_published_uri,updated_at)
	VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

func saveJob(ctx context.Context, db storeExecer, job ScheduledJob, now string, replace bool) error {
	query := insertJob + " ON CONFLICT(did,rkey) DO NOTHING"
	if replace {
		query = insertJob + ` ON CONFLICT(did,rkey) DO UPDATE SET
			scheduled_for=excluded.scheduled_for,scheduled_at=excluded.scheduled_at,status=excluded.status,
			attempts=excluded.attempts,last_error=excluded.last_error,next_attempt_at=excluded.next_attempt_at,
			last_attempt_at=excluded.last_attempt_at,publish_rkey=excluded.publish_rkey,record_type=excluded.record_type,
			published_uri=excluded.published_uri,published_cid=excluded.published_cid,
			depends_on_schedule_uri=excluded.depends_on_schedule_uri,parent_published_uri=excluded.parent_published_uri,
			updated_at=excluded.updated_at`
	}
	var lastError *string
	if job.LastError != nil {
		raw, err := json.Marshal(job.LastError)
		if err != nil {
			return err
		}
		encoded := string(raw)
		lastError = &encoded
	}
	if job.PublishRKey == "" {
		job.PublishRKey = job.RKey
	}
	if job.RecordType == "" {
		job.RecordType = "app.bsky.feed.post"
	}
	_, err := db.ExecContext(ctx, query, job.DID, job.RKey, job.ScheduledAt, job.ScheduledAt, job.Status, job.Attempts,
		lastError, job.NextAttemptAt, job.LastAttemptAt, job.PublishRKey, job.RecordType, job.PublishedURI,
		job.PublishedCID, job.DependsOnScheduleURI, job.ParentPublishedURI, now)
	return err
}

func (s *Store) UpsertScheduleJob(ctx context.Context, job ScheduledJob, now string) error {
	return saveJob(ctx, s.db, job, now, true)
}

func (s *Store) InsertScheduleJobIfMissing(ctx context.Context, job ScheduledJob, now string) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := saveJob(ctx, tx, job, now, false); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM pending_schedule_submissions WHERE did=? AND rkey=?", job.DID, job.RKey)
		return err
	})
}

func (s *Store) ListScheduleJobs(ctx context.Context, did string) ([]ScheduledJob, error) {
	return queryJobs(ctx, s.db.Reader(), "WHERE did=? ORDER BY julianday(COALESCE(scheduled_at,scheduled_for)), rkey", did)
}

func (s *Store) ScheduleJob(ctx context.Context, did, rkey string) (*ScheduledJob, error) {
	job, err := scanJob(s.db.Reader().QueryRowContext(ctx, "SELECT "+jobColumns+" FROM scheduled_jobs WHERE did=? AND rkey=?", did, rkey))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *Store) DeleteScheduleJob(ctx context.Context, did, rkey string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM scheduled_jobs WHERE did=? AND rkey=?", did, rkey)
	return err
}

func (s *Store) ClaimScheduleEdit(ctx context.Context, did, rkey, leaseID, now, expiresAt string) (bool, error) {
	var claimed bool
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM schedule_edit_leases WHERE julianday(expires_at)<=julianday(?)", now); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schedule_edit_leases(did,rkey,lease_id,expires_at)
			SELECT did,rkey,?,? FROM scheduled_jobs WHERE did=? AND rkey=?
				AND status IN ('draft','scheduled','blocked','failed') AND published_uri IS NULL`, leaseID, expiresAt, did, rkey)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		claimed = count == 1
		return err
	})
	return claimed, err
}

func (s *Store) ReleaseScheduleEdit(ctx context.Context, did, rkey, leaseID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM schedule_edit_leases WHERE did=? AND rkey=? AND lease_id=?", did, rkey, leaseID)
	return err
}

// ClaimDueJobs returns the pre-claim attempt count used by the worker backoff
// while atomically incrementing the durable count and setting publishing.
func (s *Store) ClaimDueJobs(ctx context.Context, now string, limit int) ([]ScheduledJob, error) {
	if limit <= 0 {
		limit = 10
	}
	var claimed []ScheduledJob
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		var err error
		claimed, err = queryJobs(ctx, tx, `WHERE status='scheduled'
			AND NOT EXISTS (SELECT 1 FROM schedule_edit_leases lease WHERE lease.did=scheduled_jobs.did AND lease.rkey=scheduled_jobs.rkey
				AND julianday(lease.expires_at)>julianday(?))
			AND julianday(COALESCE(scheduled_at,scheduled_for))<=julianday(?)
			AND (next_attempt_at IS NULL OR julianday(next_attempt_at)<=julianday(?))
			ORDER BY julianday(COALESCE(scheduled_at,scheduled_for)), did, rkey LIMIT ?`, now, now, now, limit)
		if err != nil {
			return err
		}
		for _, job := range claimed {
			if _, err := tx.ExecContext(ctx, `UPDATE scheduled_jobs SET status='publishing',attempts=attempts+1,
				last_attempt_at=?,updated_at=? WHERE did=? AND rkey=?`, now, now, job.DID, job.RKey); err != nil {
				return err
			}
		}
		return nil
	})
	return claimed, err
}

func (s *Store) BlockedJobs(ctx context.Context) ([]ScheduledJob, error) {
	return queryJobs(ctx, s.db.Reader(), "WHERE status='blocked' ORDER BY julianday(COALESCE(scheduled_at,scheduled_for)), did, rkey")
}

func (s *Store) MarkJobScheduled(ctx context.Context, did, rkey string, parentPublishedURI *string, now string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scheduled_jobs SET status='scheduled',parent_published_uri=?,
		last_error=NULL,updated_at=? WHERE did=? AND rkey=?`, parentPublishedURI, now, did, rkey)
	return err
}

func (s *Store) MarkJobPublished(ctx context.Context, did, rkey, uri, cid, now string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scheduled_jobs SET status='published',published_uri=?,
		published_cid=?,last_error=NULL,next_attempt_at=NULL,updated_at=? WHERE did=? AND rkey=?`, uri, cid, now, did, rkey)
	return err
}

func (s *Store) UpdatePublishRKey(ctx context.Context, did, rkey, publishRKey, now string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE scheduled_jobs SET publish_rkey=?,updated_at=? WHERE did=? AND rkey=?", publishRKey, now, did, rkey)
	return err
}

func (s *Store) MarkJobFailed(ctx context.Context, did, rkey string, failure scheduling.ScheduleError, now string) error {
	return s.markJobError(ctx, did, rkey, scheduling.StatusFailed, failure, nil, now)
}

func (s *Store) MarkJobRetry(ctx context.Context, did, rkey string, failure scheduling.ScheduleError, nextAttemptAt, now string) error {
	return s.markJobError(ctx, did, rkey, scheduling.StatusScheduled, failure, &nextAttemptAt, now)
}

func (s *Store) MarkJobBlocked(ctx context.Context, did, rkey string, failure scheduling.ScheduleError, now string) error {
	return s.markJobError(ctx, did, rkey, scheduling.StatusBlocked, failure, nil, now)
}

func (s *Store) markJobError(ctx context.Context, did, rkey string, status scheduling.ScheduleStatus, failure scheduling.ScheduleError, nextAttemptAt *string, now string) error {
	raw, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE scheduled_jobs SET status=?,last_error=?,
		next_attempt_at=COALESCE(?,next_attempt_at),updated_at=? WHERE did=? AND rkey=?`, status, string(raw), nextAttemptAt, now, did, rkey)
	return err
}

func (s *Store) RememberScheduleSubmission(ctx context.Context, did, rkey, now string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO pending_schedule_submissions(did,rkey,next_check_at,created_at)
		VALUES(?,?,?,?)`, did, rkey, now, now)
	return err
}

func (s *Store) NextPendingScheduleSubmission(ctx context.Context, now, retryAt, expireBefore string) (*JobIdentity, error) {
	var next *JobIdentity
	err := s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM pending_schedule_submissions WHERE julianday(created_at)<julianday(?)", expireBefore); err != nil {
			return err
		}
		var value JobIdentity
		err := tx.QueryRowContext(ctx, `SELECT did,rkey FROM pending_schedule_submissions WHERE julianday(next_check_at)<=julianday(?)
			ORDER BY julianday(next_check_at),did,rkey LIMIT 1`, now).Scan(&value.DID, &value.RKey)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE pending_schedule_submissions SET next_check_at=? WHERE did=? AND rkey=?", retryAt, value.DID, value.RKey)
		next = &value
		return err
	})
	return next, err
}

func (s *Store) HasImportedLegacyScheduleQueue(ctx context.Context, did string) (bool, error) {
	return imported(ctx, s.db.Reader(), did)
}

func imported(ctx context.Context, db sessionQuerier, did string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM imported_schedule_queues WHERE did=?)", did).Scan(&exists)
	return exists, err
}

func (s *Store) ImportLegacyScheduleQueue(ctx context.Context, did string, jobs []ScheduledJob, now string) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		exists, err := imported(ctx, tx, did)
		if err != nil || exists {
			return err
		}
		for _, job := range jobs {
			if job.DID == did {
				if err := saveJob(ctx, tx, job, now, false); err != nil {
					return err
				}
			}
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO imported_schedule_queues(did) VALUES(?)", did)
		return err
	})
}
