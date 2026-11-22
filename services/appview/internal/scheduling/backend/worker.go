package backend

import (
	"context"
	"errors"
	"maps"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/oauthclient"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/scheduledata"
)

// Run processes due schedules serially on the SQLite writer's host.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := s.tick(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("Scheduling tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunWorkerOnce performs a bounded scheduler sweep; callers coordinate a shared job lease.
func (s *Service) RunWorkerOnce(ctx context.Context) error { return s.tick(ctx) }

func (s *Service) tick(ctx context.Context) error {
	now := time.Now()
	stamp := scheduling.ScheduleTimestamp(now)
	if pending, err := s.store.NextPendingScheduleSubmission(ctx, stamp,
		scheduling.ScheduleTimestamp(now.Add(5*time.Minute)),
		scheduling.ScheduleTimestamp(now.Add(-24*time.Hour))); err != nil {
		return err
	} else if pending != nil {
		if record, err := s.pds.get(ctx, pending.DID, scheduling.ScheduleCollection, pending.RKey); err == nil && record != nil {
			if err := s.repairSubmission(ctx, pending.DID, pending.RKey, record.Value, stamp); err != nil {
				return err
			}
		}
	}
	if err := s.unblockDependencies(ctx, now); err != nil {
		return err
	}
	for range 10 {
		jobs, err := s.store.ClaimDueJobs(ctx, scheduling.ScheduleTimestamp(time.Now()), 1)
		if err != nil || len(jobs) == 0 {
			return err
		}
		job := jobs[0]
		attempt, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err = s.publish(attempt, job, time.Now())
		cancel()
		if err != nil && ctx.Err() == nil {
			if failure := s.publishFailed(ctx, job, err, time.Now()); failure != nil {
				s.logger.Warn("Could not record scheduling failure", "error", failure)
			}
		}
	}
	return nil
}

func (s *Service) publish(ctx context.Context, job scheduledata.ScheduledJob, now time.Time) error {
	stamp := scheduling.ScheduleTimestamp(now)
	stored, err := s.pds.get(ctx, job.DID, scheduling.ScheduleCollection, job.RKey)
	if err != nil {
		return err
	}
	if stored == nil {
		return scheduleError(scheduling.CodeRecordInvalid, "Schedule record missing from PDS")
	}
	value := maps.Clone(stored.Value)
	if value.Status() != scheduling.StatusScheduled || value.HasPublishedEvidence() {
		projection := jobFrom(job.DID, job.RKey, value)
		if projection.Status == scheduling.StatusScheduled {
			projection.Status = scheduling.StatusPublishing
		}
		return s.store.UpsertScheduleJob(ctx, projection, stamp)
	}
	if value.Text("$type") != scheduling.ScheduleCollection || !scheduling.ValidRecordKey(value.Text("publishRkey")) {
		return scheduleError(scheduling.CodeRecordInvalid, "Invalid schedule record")
	}
	if value.Text("calendarEventUri") != "" {
		if err := s.reconcileCalendar(ctx, job, value); err != nil {
			var invalid *scheduling.ScheduleError
			if errors.As(err, &invalid) {
				return s.block(ctx, job, stored, invalid, stamp)
			}
			return err
		}
	}
	scheduled, err := time.Parse(time.RFC3339Nano, value.ScheduledAt())
	if err != nil {
		return scheduleError(scheduling.CodeRecordInvalid, "Invalid scheduled publish time")
	}
	if scheduled.After(now) {
		if value.ScheduledAt() != stored.Value.ScheduledAt() {
			value["updatedAt"] = stamp
			if _, err := s.pds.write(ctx, job.DID, scheduling.ScheduleCollection, job.RKey, value, stored); err != nil {
				return err
			}
		}
		return s.store.UpsertScheduleJob(ctx, jobFrom(job.DID, job.RKey, value), stamp)
	}
	if err := scheduling.ValidateRecord(value); err != nil {
		return scheduleError(scheduling.CodeRecordInvalid, "The schedule record could not be verified.")
	}
	if dependency := value.Object("dependency"); dependency != nil {
		parent, err := s.parentPublication(ctx, dependency)
		if err != nil {
			return err
		}
		if parent == nil {
			return s.block(ctx, job, stored, scheduleError(scheduling.CodeParentUnavailable, "Parent schedule has not published yet."), stamp)
		}
		value["dependency"] = withParent(dependency, parent)
	}
	if value.Collection() == scheduling.SocialCollection {
		assignPublishKeys(value, now)
		s.hydratePreviews(ctx, job.DID, value)
	}
	retry := value.DecodeRetry()
	retry.AttemptCount = job.Attempts + 1
	retry.LastAttemptAt, retry.NextAttemptAt = &stamp, nil
	value["retry"], value["status"], value["updatedAt"] = retry, scheduling.StatusPublishing, stamp
	delete(value, "lastError")
	claim, err := s.pds.write(ctx, job.DID, scheduling.ScheduleCollection, job.RKey, value, stored)
	if err != nil {
		return err
	}
	if err := s.store.InsertAuditEvent(ctx, job.DID, &job.RKey, "publish_attempt_started", "Publishing a due schedule", stamp); err != nil {
		return err
	}
	published, err := s.pds.publish(ctx, job.DID, value, now)
	if err != nil {
		return err
	}
	value["status"] = scheduling.StatusPublished
	value["publishedUri"], value["publishedCid"], value["publishedPosts"] = published[0].URI, published[0].CID, published
	if _, err := s.pds.write(ctx, job.DID, scheduling.ScheduleCollection, job.RKey, value, &storedRecord{recordRef: claim}); err != nil {
		return err
	}
	if err := s.store.UpsertScheduleJob(ctx, jobFrom(job.DID, job.RKey, value), stamp); err != nil {
		return err
	}
	return s.store.InsertAuditEvent(ctx, job.DID, &job.RKey, "publish_succeeded", "Scheduled record published", stamp)
}

func assignPublishKeys(record scheduling.Record, now time.Time) {
	assigned := map[string]bool{}
	for index, raw := range record.Array("posts") {
		post := scheduling.Record(raw.(map[string]any))
		key := post.Text("publishRkey")
		if index == 0 && !scheduling.ValidTID(key) {
			key = record.Text("publishRkey")
		}
		if !scheduling.ValidTID(key) || assigned[key] {
			key = scheduling.NewTID(now)
		}
		post["publishRkey"] = key
		assigned[key] = true
		if index == 0 {
			record["publishRkey"] = key
		}
	}
}

func (s *Service) reconcileCalendar(ctx context.Context, job scheduledata.ScheduledJob, record scheduling.Record) error {
	event, err := s.pds.get(ctx, job.DID, scheduling.CalendarCollection, job.RKey)
	if err != nil {
		return err
	}
	if event == nil {
		return scheduleError(scheduling.CodeRecordInvalid, "Authoritative calendar event is missing.")
	}
	scheduled := event.Value.Text("startsAt")
	_, parseErr := time.Parse(time.RFC3339Nano, scheduled)
	if event.URI != record.Text("calendarEventUri") || event.Value.Text("$type") != scheduling.CalendarCollection ||
		parseErr != nil || !containsScheduleURI(event.Value, atproto.BuildRecordURI(job.DID, scheduling.ScheduleCollection, job.RKey)) {
		return scheduleError(scheduling.CodeRecordInvalid, "Authoritative calendar event is invalid.")
	}
	switch event.Value.Text("status") {
	case scheduling.CalendarCollection + "#scheduled", scheduling.CalendarCollection + "#rescheduled":
		record["scheduledAt"] = scheduled
		if shadow := record.Object("shadowRecord"); record.Collection() == scheduling.DocumentCollection && shadow != nil {
			updated := maps.Clone(shadow)
			updated["publishedAt"] = scheduled
			if updated["updatedAt"] != nil {
				updated["updatedAt"] = scheduled
			}
			record["shadowRecord"] = updated
		}
		return nil
	default:
		return scheduleError(scheduling.CodeRecordInvalid, "Authoritative calendar event is not publishable.")
	}
}

func containsScheduleURI(record scheduling.Record, target string) bool {
	for _, raw := range record.Array("uris") {
		if item, ok := raw.(map[string]any); ok && item["uri"] == target {
			return true
		}
	}
	return false
}

func (s *Service) parentPublication(ctx context.Context, dependency scheduling.Record) (*recordRef, error) {
	uri, ok := atproto.ParseRecordURI(dependency.Text("dependsOnScheduleUri"))
	if !ok || uri.Collection != scheduling.ScheduleCollection || !scheduling.ValidRecordKey(uri.RKey) {
		return nil, nil
	}
	job, err := s.store.ScheduleJob(ctx, uri.Repo, uri.RKey)
	if err != nil || job == nil || job.Status != scheduling.StatusPublished || job.PublishedURI == nil {
		return nil, err
	}
	cid := ""
	if job.PublishedCID != nil {
		cid = *job.PublishedCID
	}
	if dependency.Text("relationship") != "after" && cid == "" {
		return nil, nil
	}
	return &recordRef{URI: *job.PublishedURI, CID: cid}, nil
}

func withParent(dependency scheduling.Record, parent *recordRef) scheduling.Record {
	copy := maps.Clone(dependency)
	copy["parentPublishedUri"] = parent.URI
	if parent.CID != "" {
		copy["parentPublishedCid"] = parent.CID
	}
	return copy
}

func (s *Service) block(ctx context.Context, job scheduledata.ScheduledJob, stored *storedRecord, failure *scheduling.ScheduleError, stamp string) error {
	value := maps.Clone(stored.Value)
	value["status"], value["updatedAt"], value["lastError"] = scheduling.StatusBlocked, stamp, failure
	if _, err := s.pds.write(ctx, job.DID, scheduling.ScheduleCollection, job.RKey, value, stored); err != nil {
		return err
	}
	projection := jobFrom(job.DID, job.RKey, value)
	return s.store.UpsertScheduleJob(ctx, projection, stamp)
}

func (s *Service) unblockDependencies(ctx context.Context, now time.Time) error {
	jobs, err := s.store.BlockedJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.DependsOnScheduleURI == nil || job.LastError == nil ||
			job.LastError.Code != scheduling.CodeParentUnavailable {
			continue
		}
		if err := s.unblock(ctx, job, now); err != nil {
			s.logger.Debug("Schedule dependency still blocked", "error", err)
		}
	}
	return nil
}

func (s *Service) unblock(ctx context.Context, job scheduledata.ScheduledJob, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	id, stamp := randomToken(), scheduling.ScheduleTimestamp(now)
	held, err := s.store.ClaimScheduleEdit(ctx, job.DID, job.RKey, id, stamp, scheduling.ScheduleTimestamp(now.Add(5*time.Minute)))
	if err != nil || !held {
		return err
	}
	defer s.releaseEdit(ctx, job.DID, job.RKey, id)
	stored, err := s.pds.get(ctx, job.DID, scheduling.ScheduleCollection, job.RKey)
	if err != nil || stored == nil || stored.Value.Status() != scheduling.StatusBlocked {
		return err
	}
	dependency := scheduling.Record(stored.Value.Object("dependency"))
	parent, err := s.parentPublication(ctx, dependency)
	if err != nil || parent == nil {
		return err
	}
	value := maps.Clone(stored.Value)
	value["dependency"] = withParent(dependency, parent)
	value["status"], value["updatedAt"] = scheduling.StatusScheduled, stamp
	delete(value, "lastError")
	if _, err := s.pds.write(ctx, job.DID, scheduling.ScheduleCollection, job.RKey, value, stored); err != nil {
		return err
	}
	return s.store.UpsertScheduleJob(ctx, jobFrom(job.DID, job.RKey, value), stamp)
}

func (s *Service) publishFailed(ctx context.Context, job scheduledata.ScheduledJob, original error, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	failure := classify(original)
	stamp := scheduling.ScheduleTimestamp(now)
	stored, lookupErr := s.pds.get(ctx, job.DID, scheduling.ScheduleCollection, job.RKey)
	if lookupErr == nil && stored != nil && stored.Value.HasPublishedEvidence() {
		return s.store.UpsertScheduleJob(ctx, jobFrom(job.DID, job.RKey, stored.Value), stamp)
	}
	job.Attempts++
	job.LastAttemptAt, job.LastError, job.NextAttemptAt = &stamp, failure, nil
	job.Status = scheduling.StatusFailed
	if job.Attempts < 8 && (failure.Code == scheduling.CodeRateLimited || failure.Code == scheduling.CodeTransientNetwork || failure.Code == scheduling.CodeUnknown) {
		delay := min(time.Duration(1<<max(1, job.Attempts))*time.Minute, time.Hour) + time.Duration(rand.IntN(31))*time.Second
		next := scheduling.ScheduleTimestamp(now.Add(delay))
		job.Status, job.NextAttemptAt = scheduling.StatusScheduled, &next
	}
	if stored != nil && (stored.Value.Status() == scheduling.StatusPublishing || stored.Value.Status() == scheduling.StatusScheduled) {
		value := maps.Clone(stored.Value)
		retry := value.DecodeRetry()
		retry.AttemptCount, retry.LastAttemptAt, retry.NextAttemptAt = job.Attempts, job.LastAttemptAt, job.NextAttemptAt
		value["status"], value["updatedAt"], value["retry"], value["lastError"] = job.Status, stamp, retry, failure
		if _, err := s.pds.write(ctx, job.DID, scheduling.ScheduleCollection, job.RKey, value, stored); err != nil &&
			stored.Value.Status() == scheduling.StatusPublishing {
			// An uncertain remote attempt stays fenced until it is reconciled.
			return err
		}
	} else if stored != nil {
		return s.store.UpsertScheduleJob(ctx, jobFrom(job.DID, job.RKey, stored.Value), stamp)
	}
	return s.store.UpsertScheduleJob(ctx, job, stamp)
}

func classify(err error) *scheduling.ScheduleError {
	var schedule *scheduling.ScheduleError
	if errors.As(err, &schedule) {
		return schedule
	}
	if oauthclient.IsAuthFailure(err) {
		return scheduleError(scheduling.CodeAuthInvalid, "Reconnect this account before publishing.")
	}
	var response *oauthclient.XRPCError
	if errors.As(err, &response) {
		switch {
		case response.Status == http.StatusUnauthorized || response.Status == http.StatusForbidden:
			return scheduleError(scheduling.CodeAuthInvalid, "Reconnect this account before publishing.")
		case response.Status == http.StatusTooManyRequests:
			rateLimited := scheduleError(scheduling.CodeRateLimited, "PDS rate limited this publish attempt.")
			rateLimited.RetryAfter = optional(response.RetryAfter)
			return rateLimited
		case response.Status >= 500 || response.Code == "invalid_client_metadata":
			return scheduleError(scheduling.CodeTransientNetwork, "PDS is temporarily unavailable.")
		case response.Status == http.StatusBadRequest || response.Status == http.StatusNotFound:
			return scheduleError(scheduling.CodeRecordInvalid, "PDS rejected the scheduled record.")
		}
	}
	return scheduleError(scheduling.CodeUnknown, "The publish request could not be completed.")
}
