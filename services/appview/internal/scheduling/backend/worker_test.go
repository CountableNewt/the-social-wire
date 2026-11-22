package backend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/scheduledata"
)

func TestSchedulePublishesArticleOnceAndCannotBeCanceled(t *testing.T) {
	ctx := t.Context()
	store, err := scheduledata.Open(ctx, ":memory:", "test-secret", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const did = "did:plc:viewer"
	fixture := &repoFixture{records: map[string]storedRecord{}}
	service := &Service{
		store: store, pds: pds{rpc: fixture.call, isNotFound: func(err error) bool { return err == errMissing }},
		submissionSecret: sha256.Sum256([]byte("test-secret")), logger: slog.Default(),
	}
	future := time.Now().Add(30 * time.Minute).Truncate(15 * time.Minute)
	stamp := scheduling.ScheduleTimestamp(future)
	record := func() scheduling.Record {
		return scheduling.Record{
			"$type": "at.skej.schedule", "scheduledAt": stamp, "status": "scheduled",
			"recordType": "site.standard.document", "publishRkey": "stable-article",
			"shadowRecord": map[string]any{"$type": "site.standard.document", "site": "https://fixture.example",
				"title": "An article", "publishedAt": stamp},
		}
	}
	first, err := service.create(ctx, did, record())
	if err != nil || first.Status != scheduling.StatusScheduled {
		t.Fatalf("create returned %+v, %v", first, err)
	}
	retry, err := service.create(ctx, did, record())
	if err != nil || retry.RKey != first.RKey || fixture.creates != 2 {
		t.Fatalf("retry returned %+v with %d writes, %v", retry, fixture.creates, err)
	}
	due := scheduling.ScheduleTimestamp(future.Add(time.Second))
	jobs, err := store.ClaimDueJobs(ctx, due, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("claim got %v, %v", jobs, err)
	}
	if err := service.publish(ctx, jobs[0], future.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	job, err := store.ScheduleJob(ctx, did, first.RKey)
	if err != nil || job == nil || job.Status != scheduling.StatusPublished || job.PublishedURI == nil {
		t.Fatalf("job was not published: %+v %v", job, err)
	}
	if _, err := service.edit(ctx, did, first.RKey, nil); err == nil {
		t.Fatal("cancel unexpectedly accepted a published job")
	}
	jobs, err = store.ClaimDueJobs(ctx, due, 1)
	if err != nil || len(jobs) != 0 || fixture.creates != 3 {
		t.Fatalf("published record was requeued: %+v created=%d err=%v", jobs, fixture.creates, err)
	}
}

var errMissing = errors.New("record not found")

type repoFixture struct {
	records map[string]storedRecord
	creates int
}

func (f *repoFixture) call(_ context.Context, did, method, nsid string, query url.Values, body []byte) ([]byte, error) {
	if method == http.MethodGet {
		switch nsid {
		case "com.atproto.repo.getRecord":
			record, ok := f.records[atproto.BuildRecordURI(did, query.Get("collection"), query.Get("rkey"))]
			if !ok {
				return nil, errMissing
			}
			return json.Marshal(record)
		case "com.atproto.repo.listRecords":
			records := []storedRecord{}
			for _, record := range f.records {
				if record.Value.Text("$type") == query.Get("collection") {
					records = append(records, record)
				}
			}
			return json.Marshal(map[string]any{"records": records})
		}
	}
	if method != http.MethodPost || (nsid != "com.atproto.repo.createRecord" && nsid != "com.atproto.repo.putRecord") {
		return nil, errors.New("unexpected repo call")
	}
	var write struct {
		Collection string            `json:"collection"`
		RKey       string            `json:"rkey"`
		Swap       string            `json:"swapRecord"`
		Record     scheduling.Record `json:"record"`
	}
	if err := json.Unmarshal(body, &write); err != nil {
		return nil, err
	}
	uri := atproto.BuildRecordURI(did, write.Collection, write.RKey)
	previous, exists := f.records[uri]
	if nsid == "com.atproto.repo.putRecord" && (!exists || write.Swap != previous.CID) {
		return nil, errors.New("compare-and-swap failed")
	}
	if nsid == "com.atproto.repo.createRecord" {
		if exists {
			return nil, errors.New("cannot create existing record")
		}
		f.creates++
	}
	value, err := json.Marshal(write.Record)
	if err != nil {
		return nil, err
	}
	ref := recordRef{URI: uri, CID: atproto.EncodeBase64URL(value)}
	f.records[uri] = storedRecord{recordRef: ref, Value: write.Record}
	return json.Marshal(ref)
}
