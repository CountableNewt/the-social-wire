package backend

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/scheduledata"
)

type summary struct {
	RKey          string                    `json:"rkey"`
	DID           string                    `json:"did"`
	ScheduleURI   string                    `json:"scheduleUri"`
	ScheduledAt   string                    `json:"scheduledAt"`
	Status        scheduling.ScheduleStatus `json:"status"`
	Record        scheduling.Record         `json:"record"`
	Attempts      int                       `json:"attempts"`
	LastError     *scheduling.ScheduleError `json:"lastError,omitempty"`
	NextAttemptAt *string                   `json:"nextAttemptAt,omitempty"`
	PublishedURI  *string                   `json:"publishedUri,omitempty"`
	PublishedCID  *string                   `json:"publishedCid,omitempty"`
}

func jobFrom(did, key string, value scheduling.Record) scheduledata.ScheduledJob {
	retry := value.DecodeRetry()
	dependency := scheduling.Record(value.Object("dependency"))
	return scheduledata.ScheduledJob{
		DID: did, RKey: key, Status: value.Status(), Attempts: retry.AttemptCount,
		ScheduledAt: normalizeTime(value.ScheduledAt()), PublishRKey: value.Text("publishRkey"),
		RecordType: value.Collection(), LastError: value.DecodeError(),
		LastAttemptAt: retry.LastAttemptAt, NextAttemptAt: retry.NextAttemptAt,
		PublishedURI: optional(value.Text("publishedUri")), PublishedCID: optional(value.Text("publishedCid")),
		DependsOnScheduleURI: optional(dependency.Text("dependsOnScheduleUri")),
		ParentPublishedURI:   optional(dependency.Text("parentPublishedUri")),
	}
}

func summarize(job scheduledata.ScheduledJob, value scheduling.Record) summary {
	publishedURI, publishedCID := job.PublishedURI, job.PublishedCID
	if publishedURI == nil {
		publishedURI = optional(value.Text("publishedUri"))
	}
	if publishedCID == nil {
		publishedCID = optional(value.Text("publishedCid"))
	}
	return summary{
		RKey: job.RKey, DID: job.DID, ScheduleURI: atproto.BuildRecordURI(job.DID, scheduling.ScheduleCollection, job.RKey),
		ScheduledAt: value.ScheduledAt(), Status: job.Status, Record: value, Attempts: job.Attempts,
		LastError: job.LastError, NextAttemptAt: job.NextAttemptAt, PublishedURI: publishedURI, PublishedCID: publishedCID,
	}
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func normalizeTime(raw string) string {
	if value, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return scheduling.ScheduleTimestamp(value)
	}
	return raw
}

func scheduleError(code scheduling.ScheduleErrorCode, message string) *scheduling.ScheduleError {
	return &scheduling.ScheduleError{Code: code, Classification: code, Message: message}
}

func notEditable() error {
	return httpx.Named(http.StatusConflict, "ScheduleNotEditable", "This schedule is publishing, changed, or already finished")
}

func (s *Service) loadSchedules(ctx context.Context, did string) ([]summary, error) {
	records, err := s.pds.schedules(ctx, did)
	if err != nil {
		return nil, err
	}
	if err := s.repairAndImport(ctx, did, records); err != nil {
		return nil, err
	}
	jobs, err := s.store.ListScheduleJobs(ctx, did)
	if err != nil {
		return nil, err
	}
	result := make([]summary, 0, len(jobs))
	for _, job := range jobs {
		if record, ok := records[job.RKey]; ok {
			result = append(result, summarize(job, record.Value))
		}
	}
	return result, nil
}

func (s *Service) repairAndImport(ctx context.Context, did string, records map[string]storedRecord) error {
	now := scheduling.ScheduleTimestamp(time.Now())
	if s.importLegacy {
		imported, err := s.store.HasImportedLegacyScheduleQueue(ctx, did)
		if err != nil {
			return err
		}
		if !imported {
			jobs := make([]scheduledata.ScheduledJob, 0, len(records))
			for key, record := range records {
				if job, ok := legacyJob(did, key, record.Value); ok {
					jobs = append(jobs, job)
				}
			}
			if err := s.store.ImportLegacyScheduleQueue(ctx, did, jobs, now); err != nil {
				return err
			}
		}
	}
	for key, record := range records {
		if err := s.repairSubmission(ctx, did, key, record.Value, now); err != nil {
			return err
		}
	}
	return nil
}

func legacyJob(did, key string, record scheduling.Record) (scheduledata.ScheduledJob, bool) {
	job := jobFrom(did, key, record)
	if _, err := time.Parse(time.RFC3339Nano, record.ScheduledAt()); err != nil ||
		!scheduling.ValidRecordKey(key) || !scheduling.ValidRecordKey(job.PublishRKey) ||
		record.Text("$type") != scheduling.ScheduleCollection ||
		(job.RecordType != scheduling.SocialCollection && job.RecordType != scheduling.DocumentCollection) {
		return scheduledata.ScheduledJob{}, false
	}
	if job.Status == scheduling.StatusPublishing ||
		((job.Status == scheduling.StatusScheduled || job.Status == scheduling.StatusBlocked) &&
			(job.Attempts > 0 || record.HasPublishedEvidence())) {
		job.Status = scheduling.StatusPublishing
		job.LastError = scheduleError(scheduling.CodeUnknown, "The previous publisher may have attempted this schedule. Review the published content before scheduling again.")
	}
	return job, true
}

func (s *Service) repairSubmission(ctx context.Context, did, key string, record scheduling.Record, now string) error {
	originalKey := record.Text("socialWireSubmissionKey")
	proof, err := base64.StdEncoding.DecodeString(record.Text("socialWireSubmissionProof"))
	if err != nil || !strings.HasPrefix(key, "sw-") || len(key) != 67 || !scheduling.ValidRecordKey(originalKey) ||
		!hmac.Equal(proof, s.proof(did, key, originalKey)) {
		return nil
	}
	job, ok := legacyJob(did, key, record)
	if !ok {
		return nil
	}
	if job.Status == scheduling.StatusScheduled && scheduling.ValidateRecord(record) != nil {
		job.Status = scheduling.StatusPublishing
		job.LastError = scheduleError(scheduling.CodeRecordInvalid, "The schedule record could not be verified.")
	}
	return s.store.InsertScheduleJobIfMissing(ctx, job, now)
}

func (s *Service) proof(did, key, publishKey string) []byte {
	mac := hmac.New(sha256.New, s.submissionSecret[:])
	mac.Write([]byte(did + "\n" + key + "\n" + publishKey))
	return mac.Sum(nil)
}

func (s *Service) create(ctx context.Context, did string, value scheduling.Record) (summary, error) {
	if !s.acquireCreate(did) {
		return summary{}, httpx.Named(http.StatusConflict, "ScheduleChanged", "This account is saving another schedule")
	}
	defer s.releaseCreate(did)
	now := time.Now()
	scheduled, parseErr := time.Parse(time.RFC3339Nano, value.ScheduledAt())
	if err := scheduling.ValidateRecord(value); err != nil {
		return summary{}, httpx.Named(http.StatusBadRequest, "InvalidSchedule", err.Error())
	}
	if parseErr != nil || !scheduled.After(now) || value.Status() != scheduling.StatusScheduled {
		return summary{}, httpx.Named(http.StatusBadRequest, "InvalidSchedule", "Choose a future schedule time")
	}
	publishKey := value.Text("publishRkey")
	schedules, err := s.loadSchedules(ctx, did)
	if err != nil {
		return summary{}, err
	}
	var found *summary
	var canceled *summary
	for _, candidate := range schedules {
		if candidate.RKey != scheduling.RecordKey(publishKey, "") &&
			candidate.Record.Text("publishRkey") != publishKey &&
			candidate.Record.Text("socialWireSubmissionKey") != publishKey {
			continue
		}
		if candidate.Record.Status() == scheduling.StatusCanceled {
			if canceled == nil || candidate.Record.Text("updatedAt") > canceled.Record.Text("updatedAt") {
				canceled = &candidate
			}
			continue
		}
		if found != nil {
			return summary{}, httpx.Named(http.StatusConflict, "ScheduleChanged", "This content has multiple schedules")
		}
		found = &candidate
	}
	if found != nil {
		if err := scheduling.PrepareRecord(value, did, found.Record.Text("updatedAt"), found.Record); err != nil {
			return summary{}, httpx.Named(http.StatusBadRequest, "InvalidSchedule", err.Error())
		}
		if found.Record.ScheduledAt() != value.ScheduledAt() || found.Record.Collection() != value.Collection() ||
			!equalJSON(found.Record["posts"], value["posts"]) || !equalJSON(found.Record["shadowRecord"], value["shadowRecord"]) {
			return summary{}, httpx.Named(http.StatusConflict, "ScheduleChanged", "This content already has a different schedule")
		}
		return *found, nil
	}
	previousCancellation := ""
	if canceled != nil {
		previousCancellation = canceled.RKey
	}
	key := scheduling.RecordKey(publishKey, previousCancellation)
	stamp := scheduling.ScheduleTimestamp(now)
	if err := scheduling.PrepareRecord(value, did, stamp, nil); err != nil {
		return summary{}, httpx.Named(http.StatusBadRequest, "InvalidSchedule", err.Error())
	}
	value["socialWireSubmissionProof"] = base64.StdEncoding.EncodeToString(s.proof(did, key, publishKey))
	if err := s.store.RememberScheduleSubmission(ctx, did, key, stamp); err != nil {
		return summary{}, err
	}
	if err := s.pds.writeCalendar(ctx, did, key, value); err != nil {
		return summary{}, err
	}
	if _, err := s.pds.write(ctx, did, scheduling.ScheduleCollection, key, value, nil); err != nil {
		return summary{}, err
	}
	job := jobFrom(did, key, value)
	if err := s.store.InsertScheduleJobIfMissing(ctx, job, stamp); err != nil {
		return summary{}, err
	}
	return summarize(job, value), nil
}

func (s *Service) edit(ctx context.Context, did, key string, update scheduling.Record) (summary, error) {
	notFound := httpx.Named(http.StatusNotFound, "ScheduleNotFound", "Schedule not found")
	if !scheduling.ValidRecordKey(key) {
		return summary{}, notFound
	}
	job, err := s.store.ScheduleJob(ctx, did, key)
	if err != nil || job == nil {
		if err != nil {
			return summary{}, err
		}
		return summary{}, notFound
	}
	now := time.Now()
	stamp := scheduling.ScheduleTimestamp(now)
	lease := randomToken()
	held, err := s.store.ClaimScheduleEdit(ctx, did, key, lease, stamp, scheduling.ScheduleTimestamp(now.Add(5*time.Minute)))
	if err != nil {
		return summary{}, err
	}
	if !held {
		return summary{}, notEditable()
	}
	defer s.releaseEdit(ctx, did, key, lease)
	existing, err := s.pds.get(ctx, did, scheduling.ScheduleCollection, key)
	if err != nil {
		return summary{}, err
	}
	if existing == nil || existing.Value.HasPublishedEvidence() || !slices.Contains([]scheduling.ScheduleStatus{
		scheduling.StatusDraft, scheduling.StatusScheduled, scheduling.StatusBlocked, scheduling.StatusFailed,
	}, existing.Value.Status()) {
		return summary{}, notEditable()
	}
	value := maps.Clone(existing.Value)
	value["updatedAt"] = stamp
	if update == nil {
		value["status"] = scheduling.StatusCanceled
	} else {
		date, err := time.Parse(time.RFC3339Nano, update.ScheduledAt())
		if err != nil || !date.After(now) || update.Text("publishRkey") != existing.Value.Text("publishRkey") ||
			update.Collection() != existing.Value.Collection() {
			return summary{}, httpx.Named(http.StatusBadRequest, "InvalidSchedule", "Choose a future time for this schedule")
		}
		value["scheduledAt"], value["status"] = update.ScheduledAt(), scheduling.StatusScheduled
		if document := value.Object("shadowRecord"); value.Collection() == scheduling.DocumentCollection && document != nil {
			document = maps.Clone(document)
			document["publishedAt"] = update.ScheduledAt()
			if document["updatedAt"] != nil {
				document["updatedAt"] = update.ScheduledAt()
			}
			value["shadowRecord"] = document
		}
		retry := value.DecodeRetry()
		retry.NextAttemptAt = nil
		value["retry"] = retry
		delete(value, "lastError")
	}
	if err := s.pds.writeCalendar(ctx, did, key, value); err != nil {
		return summary{}, err
	}
	if _, err := s.pds.write(ctx, did, scheduling.ScheduleCollection, key, value, existing); err != nil {
		return summary{}, err
	}
	result := jobFrom(did, key, value)
	result.Attempts = job.Attempts
	if err := s.store.UpsertScheduleJob(ctx, result, stamp); err != nil {
		return summary{}, err
	}
	return summarize(result, value), nil
}

func (s *Service) releaseEdit(ctx context.Context, did, key, lease string) {
	release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.ReleaseScheduleEdit(release, did, key, lease); err != nil {
		s.logger.Warn("Scheduling edit lease release failed", "error", err)
	}
}

func equalJSON(a, b any) bool {
	first, err1 := json.Marshal(a)
	second, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(first) == string(second)
}
