package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/richtext"
)

type rpcFunc func(context.Context, string, string, string, url.Values, []byte) ([]byte, error)

type pds struct {
	rpc        rpcFunc
	isNotFound func(error) bool
}

type recordRef struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

type storedRecord struct {
	recordRef
	Value scheduling.Record `json:"value"`
}

type publishedPost struct {
	RKey string `json:"rkey"`
	recordRef
}

func (p pds) query(ctx context.Context, did, nsid string, query url.Values, result any) error {
	data, err := p.rpc(ctx, did, http.MethodGet, nsid, query, nil)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, result) != nil {
		return errors.New("invalid scheduling PDS reply")
	}
	return nil
}

func (p pds) get(ctx context.Context, did, collection, rkey string) (*storedRecord, error) {
	var record storedRecord
	err := p.query(ctx, did, "com.atproto.repo.getRecord", url.Values{
		"repo": {did}, "collection": {collection}, "rkey": {rkey},
	}, &record)
	if err != nil {
		if p.isNotFound != nil && p.isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if record.URI != atproto.BuildRecordURI(did, collection, rkey) || record.CID == "" || record.Value == nil {
		return nil, errors.New("unexpected scheduling PDS record")
	}
	return &record, nil
}

func (p pds) connection(ctx context.Context, did string) error {
	_, err := p.rpc(ctx, did, http.MethodGet, "com.atproto.repo.listRecords",
		url.Values{"repo": {did}, "collection": {scheduling.ScheduleCollection}, "limit": {"1"}}, nil)
	return err
}

func (p pds) schedules(ctx context.Context, did string) (map[string]storedRecord, error) {
	query := url.Values{"repo": {did}, "collection": {scheduling.ScheduleCollection}, "limit": {"100"}}
	records := map[string]storedRecord{}
	cursors := map[string]bool{}
	for range 100 {
		var page struct {
			Records []storedRecord `json:"records"`
			Cursor  string         `json:"cursor"`
		}
		if err := p.query(ctx, did, "com.atproto.repo.listRecords", query, &page); err != nil {
			return nil, err
		}
		if page.Records == nil || len(page.Records) > 100 {
			return nil, errors.New("invalid scheduling PDS page")
		}
		for _, record := range page.Records {
			uri, ok := atproto.ParseRecordURI(record.URI)
			if !ok || uri.Repo != did || uri.Collection != scheduling.ScheduleCollection ||
				!scheduling.ValidRecordKey(uri.RKey) || record.CID == "" || record.Value == nil {
				return nil, errors.New("unexpected PDS schedule")
			}
			records[uri.RKey] = record
		}
		if page.Cursor == "" {
			return records, nil
		}
		if len(page.Cursor) > 2048 || cursors[page.Cursor] {
			break
		}
		cursors[page.Cursor] = true
		query.Set("cursor", page.Cursor)
	}
	return nil, errors.New("PDS schedule pagination did not finish")
}

func (p pds) write(ctx context.Context, did, collection, rkey string, value scheduling.Record, previous *storedRecord) (recordRef, error) {
	method := "com.atproto.repo.putRecord"
	input := map[string]any{"repo": did, "collection": collection, "rkey": rkey, "record": value, "validate": true}
	if previous != nil {
		input["swapRecord"] = previous.CID
	} else {
		method = "com.atproto.repo.createRecord"
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return recordRef{}, err
	}
	body, err := p.rpc(ctx, did, http.MethodPost, method, nil, encoded)
	if err != nil {
		return recordRef{}, err
	}
	var result recordRef
	if json.Unmarshal(body, &result) != nil || result.URI != atproto.BuildRecordURI(did, collection, rkey) || result.CID == "" {
		return recordRef{}, errors.New("unexpected PDS write response")
	}
	return result, nil
}

// createPublished accepts a preexisting deterministic record only if it matches
// the intended publication. A retry never replaces published data.
func (p pds) createPublished(ctx context.Context, did, collection, rkey string, value scheduling.Record) (recordRef, error) {
	created, err := p.write(ctx, did, collection, rkey, value, nil)
	if err == nil {
		return created, nil
	}
	existing, lookupErr := p.get(ctx, did, collection, rkey)
	if lookupErr == nil && existing != nil && samePublication(collection, existing.Value, value) {
		return existing.recordRef, nil
	}
	return recordRef{}, err
}

func samePublication(collection string, first, second scheduling.Record) bool {
	if collection == scheduling.SocialCollection {
		first, second = maps.Clone(first), maps.Clone(second)
		delete(first, "createdAt")
		delete(second, "createdAt")
	}
	a, err1 := json.Marshal(first)
	b, err2 := json.Marshal(second)
	return err1 == nil && err2 == nil && string(a) == string(b)
}

func (p pds) publish(ctx context.Context, did string, schedule scheduling.Record, now time.Time) ([]publishedPost, error) {
	collection := schedule.Collection()
	if collection == scheduling.DocumentCollection {
		rkey := schedule.Text("publishRkey")
		created, err := p.createPublished(ctx, did, collection, rkey, canonicalDocument(schedule.Object("shadowRecord")))
		if err != nil {
			return nil, err
		}
		return []publishedPost{{rkey, created}}, nil
	}
	plans := schedule.Array("posts")
	published := make([]publishedPost, 0, len(plans))
	for index, raw := range plans {
		typed, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("invalid scheduled post")
		}
		plan := scheduling.Record(typed)
		rkey := plan.Text("publishRkey")
		if !scheduling.ValidTID(rkey) {
			return nil, errors.New("missing stable publish key")
		}
		post, err := postRecord(plan, now)
		if err != nil {
			return nil, err
		}
		if index == 0 {
			if err := applyParent(post, scheduling.Record(schedule.Object("dependency"))); err != nil {
				return nil, err
			}
		} else {
			post["reply"] = reply(published[0].recordRef, published[index-1].recordRef)
		}
		created, err := p.createPublished(ctx, did, collection, rkey, post)
		if err != nil {
			return nil, err
		}
		published = append(published, publishedPost{rkey, created})
	}
	if len(published) == 0 {
		return nil, errors.New("empty scheduled post")
	}
	return published, nil
}

func postRecord(plan scheduling.Record, now time.Time) (scheduling.Record, error) {
	plan = maps.Clone(plan)
	if err := scheduling.CanonicalizePost(plan); err != nil {
		return nil, err
	}
	post := scheduling.Record{"$type": scheduling.SocialCollection, "text": plan.Text("text"), "createdAt": scheduling.ScheduleTimestamp(now)}
	for _, key := range []string{"facets", "reply", "embed", "langs", "tags"} {
		if value := plan[key]; value != nil {
			post[key] = value
		}
	}
	if labels := plan.Array("labels"); len(labels) > 0 {
		values := make([]map[string]string, 0, len(labels))
		for _, label := range labels {
			if value, ok := label.(string); ok {
				values = append(values, map[string]string{"val": value})
			}
		}
		post["labels"] = map[string]any{"$type": "com.atproto.label.defs#selfLabels", "values": values}
	}
	return post, nil
}

func applyParent(post, dependency scheduling.Record) error {
	relationship := dependency.Text("relationship")
	if relationship == "" || relationship == "after" {
		return nil
	}
	parent := recordRef{URI: dependency.Text("parentPublishedUri"), CID: dependency.Text("parentPublishedCid")}
	if parent.URI == "" || parent.CID == "" {
		return errors.New("scheduled parent is unavailable")
	}
	switch relationship {
	case "reply":
		post["reply"] = reply(parent, parent)
	case "quote":
		quote := scheduling.Record{"$type": "app.bsky.embed.record", "record": parent}
		media := scheduling.Record(post.Object("embed"))
		if media.Text("$type") != "" && !strings.HasPrefix(media.Text("$type"), "app.bsky.embed.record") {
			post["embed"] = scheduling.Record{"$type": "app.bsky.embed.recordWithMedia", "record": quote, "media": media}
		} else {
			post["embed"] = quote
		}
	default:
		return fmt.Errorf("unsupported schedule dependency %q", relationship)
	}
	return nil
}

func reply(root, parent recordRef) map[string]recordRef {
	return map[string]recordRef{"root": root, "parent": parent}
}

func (p pds) writeCalendar(ctx context.Context, did, rkey string, record scheduling.Record) error {
	existing, err := p.get(ctx, did, scheduling.CalendarCollection, rkey)
	if err != nil {
		return err
	}
	date, err := time.Parse(time.RFC3339Nano, record.ScheduledAt())
	if err != nil {
		return errors.New("invalid calendar event date")
	}
	title := strings.TrimSpace(record.Text("title"))
	if title == "" {
		title = "Scheduled post"
	}
	status := "scheduled"
	switch {
	case record.Status() == scheduling.StatusCanceled:
		status = "cancelled"
	case record.Status() == scheduling.StatusDraft:
		status = "planned"
	case existing != nil && existing.Value.Text("startsAt") != record.ScheduledAt():
		status = "rescheduled"
	}
	scheduleURI := atproto.BuildRecordURI(did, scheduling.ScheduleCollection, rkey)
	event := scheduling.Record{
		"$type": scheduling.CalendarCollection, "name": title,
		"uris": []map[string]string{
			{"uri": scheduleURI, "name": "Skej schedule"},
			{"uri": "https://skej.at/app/calendar?account=" + url.QueryEscape(did) + "&schedule=" + url.QueryEscape(rkey), "name": "Open in Skej"},
		},
		"startsAt": record.ScheduledAt(), "endsAt": scheduling.ScheduleTimestamp(date.Add(30 * time.Minute)),
		"status": scheduling.CalendarCollection + "#" + status, "mode": scheduling.CalendarCollection + "#virtual",
		"createdAt": record.Text("createdAt"),
	}
	created, err := p.write(ctx, did, scheduling.CalendarCollection, rkey, event, existing)
	if err != nil {
		return err
	}
	record["calendarEventUri"], record["calendarEventCid"] = created.URI, created.CID
	return nil
}

func canonicalDocument(value scheduling.Record) scheduling.Record {
	result, ok := richtext.CanonicalizeFeedPost(map[string]any(value)).(map[string]any)
	if !ok {
		return value
	}
	return result
}
