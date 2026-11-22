package backend

import (
	"crypto/sha256"
	"log/slog"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/scheduledata"
)

func TestScheduleTimeSelectionAndHistoricalWorkerCompatibility(t *testing.T) {
	ctx := t.Context()
	store, err := scheduledata.Open(ctx, ":memory:", "test-secret", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const did = "did:plc:viewer"
	fixture := &repoFixture{records: map[string]storedRecord{}}
	service := &Service{store: store, pds: pds{rpc: fixture.call, isNotFound: func(err error) bool { return err == errMissing }},
		submissionSecret: sha256.Sum256([]byte("test-secret")), logger: slog.Default()}
	future := time.Now().Add(time.Hour).Truncate(15 * time.Minute)
	record := func(date time.Time) scheduling.Record {
		stamp := scheduling.ScheduleTimestamp(date)
		return scheduling.Record{"$type": "at.skej.schedule", "scheduledAt": stamp, "status": "scheduled",
			"recordType": "site.standard.document", "publishRkey": "stable-article",
			"shadowRecord": map[string]any{"$type": "site.standard.document", "site": "https://fixture.example", "title": "An article", "publishedAt": stamp}}
	}
	if _, err := service.create(ctx, did, record(future.Add(time.Minute))); err == nil || fixture.creates != 0 {
		t.Fatal("off-grid creation reached the PDS")
	}
	created, err := service.create(ctx, did, record(future))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.edit(ctx, did, created.RKey, record(future.Add(time.Minute))); err == nil {
		t.Fatal("off-grid rescheduling was accepted")
	}
	// The worker must still publish off-grid schedules already written by older clients.
	legacy := record(future.Add(time.Minute))
	for uri, stored := range fixture.records {
		if stored.Value.Text("$type") == scheduling.ScheduleCollection {
			stored.Value = legacy
			fixture.records[uri] = stored
		}
	}
	job := jobFrom(did, created.RKey, legacy)
	if err := store.UpsertScheduleJob(ctx, job, scheduling.ScheduleTimestamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	due := future.Add(2 * time.Minute)
	jobs, err := store.ClaimDueJobs(ctx, scheduling.ScheduleTimestamp(due), 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("historical schedule was not claimed: %v", err)
	}
	if err := service.publish(ctx, jobs[0], due); err != nil {
		t.Fatal(err)
	}
	stored, err := store.ScheduleJob(ctx, did, created.RKey)
	if err != nil || stored == nil || stored.Status != scheduling.StatusPublished {
		t.Fatal("historical off-grid schedule did not publish")
	}
}
