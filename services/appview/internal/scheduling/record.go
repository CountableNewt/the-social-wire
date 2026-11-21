package scheduling

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/richtext"
)

const ScheduleCollection = "at.skej.schedule"
const CalendarCollection = "community.lexicon.calendar.event"
const SocialCollection = "app.bsky.feed.post"
const DocumentCollection = "site.standard.document"

// Record keeps the complete PDS schedule object, including unknown fields.
type Record map[string]any

func (r Record) Text(key string) string {
	switch value := r[key].(type) {
	case string:
		return value
	case ScheduleStatus:
		return string(value)
	default:
		return ""
	}
}

func (r Record) Object(key string) map[string]any {
	value, _ := r[key].(map[string]any)
	return value
}

func (r Record) Array(key string) []any {
	value, _ := r[key].([]any)
	return value
}

func (r Record) ScheduledAt() string {
	if value := r.Text("scheduledAt"); value != "" {
		return value
	}
	return r.Text("scheduledFor")
}

func (r Record) Collection() string {
	if value := r.Text("recordType"); value != "" {
		return value
	}
	return SocialCollection
}

func (r Record) Status() ScheduleStatus {
	if status, ok := knownStatus(r.Text("status")); ok {
		return status
	}
	return StatusFailed
}

func (r Record) HasPublishedEvidence() bool {
	return r.Text("publishedUri") != "" || r.Text("publishedCid") != "" || len(r.Array("publishedPosts")) != 0
}

func (r Record) DecodeRetry() RetryState {
	state := newRetryState()
	if value := r["retry"]; value != nil {
		if data, err := json.Marshal(value); err == nil {
			_ = json.Unmarshal(data, &state)
		}
	}
	return state
}

func (r Record) DecodeError() *ScheduleError {
	var state *ScheduleError
	if value := r["lastError"]; value != nil {
		if data, err := json.Marshal(value); err == nil {
			_ = json.Unmarshal(data, &state)
		}
	}
	return state
}

var recordKey = regexp.MustCompile(`^[a-zA-Z0-9._~:-]{1,512}$`)

func ValidRecordKey(value string) bool {
	return value != "." && value != ".." && recordKey.MatchString(value)
}

func RecordKey(publishKey, previousCancellation string) string {
	if previousCancellation != "" {
		publishKey += "/" + previousCancellation
	}
	digest := sha256.Sum256([]byte(publishKey))
	return "sw-" + hex.EncodeToString(digest[:])
}

func ScheduleTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func ValidateRecord(r Record) error {
	invalid := errors.New("invalid scheduled content")
	if r.Text("$type") != ScheduleCollection || !ValidRecordKey(r.Text("publishRkey")) ||
		r.HasPublishedEvidence() || richtext.GraphemeCount(r.Text("title")) > 120 {
		return invalid
	}
	posts := r.Array("posts")
	switch r.Collection() {
	case DocumentCollection:
		document := Record(r.Object("shadowRecord"))
		site := document.Text("site")
		if len(posts) != 0 || strings.TrimSpace(document.Text("title")) == "" ||
			(!strings.HasPrefix(site, "https://") && !strings.Contains(site, "/site.standard.publication/")) ||
			document.Text("$type") != DocumentCollection || document.Text("publishedAt") != r.ScheduledAt() {
			return invalid
		}
	case SocialCollection:
		if r["shadowRecord"] != nil || len(posts) < 1 || len(posts) > 100 {
			return invalid
		}
		for _, raw := range posts {
			post, ok := raw.(map[string]any)
			if !ok {
				return invalid
			}
			text := Record(post).Text("text")
			if text == "" || len(text) > 3000 || richtext.GraphemeCount(text) > 300 {
				return invalid
			}
			if raw := post["facets"]; raw != nil {
				facets, ok := raw.([]any)
				if !ok || richtext.ValidateFacets(facets, text) != nil {
					return errors.New("invalid post facets")
				}
			}
		}
	default:
		return invalid
	}
	return nil
}

func PrepareRecord(r Record, did, now string, existing Record) error {
	for _, key := range []string{"teamUri", "calendarEventUri", "calendarEventCid", "lastError", "publishedUri", "publishedCid", "publishedPosts"} {
		delete(r, key)
	}
	r["scheduledAt"] = r.ScheduledAt()
	delete(r, "scheduledFor")
	r["recordType"] = r.Collection()
	r["createdAt"], r["updatedAt"] = now, now
	r["createdByDid"], r["approvedByDid"], r["approvedAt"] = did, did, now
	if existing != nil {
		r["createdAt"], r["approvedAt"] = existing.Text("createdAt"), existing.Text("approvedAt")
	}
	if r.Text("timezonePolicy") == "" {
		r["timezonePolicy"] = "user_local"
	}
	r["retry"] = newRetryState()
	r["socialWireSubmissionKey"] = r.Text("publishRkey")
	if r.Collection() != SocialCollection {
		return nil
	}
	assigned := map[string]bool{}
	previous := existing.Array("posts")
	for index, raw := range r.Array("posts") {
		post := Record(raw.(map[string]any))
		if err := CanonicalizePost(post); err != nil {
			return err
		}
		key := post.Text("publishRkey")
		if index < len(previous) {
			if old, ok := previous[index].(map[string]any); ok {
				key = Record(old).Text("publishRkey")
			}
		}
		if !ValidTID(key) || assigned[key] {
			key = NewTID(time.Now())
		}
		post["publishRkey"] = key
		assigned[key] = true
		if index == 0 {
			r["publishRkey"] = key
		}
	}
	return nil
}

func CanonicalizePost(post Record) error {
	rawSource := post.Object("source")
	var authored []any
	explicit := post.Array("facets")
	detect := post["facets"] == nil
	if rawSource != nil {
		source := Record(rawSource)
		var mentions []richtext.Mention
		if raw := source["mentions"]; raw != nil {
			data, err := json.Marshal(raw)
			if err != nil || json.Unmarshal(data, &mentions) != nil {
				return errors.New("invalid post mentions")
			}
		}
		projection := richtext.Project(source.Text("text"), mentions)
		post["text"] = projection.Text
		authored, explicit, detect = projection.Facets, nil, false
	}
	text := post.Text("text")
	if text == "" || len(text) > 3000 || richtext.GraphemeCount(text) > 300 {
		return errors.New("invalid projected post text")
	}
	if facets := richtext.Facets(text, explicit, authored, detect); facets != nil {
		post["facets"] = facets
	} else {
		delete(post, "facets")
	}
	if embed := richtext.NormalizeEmbed(post["embed"]); embed != nil {
		post["embed"] = embed
	}
	return nil
}

const tidAlphabet = "234567abcdefghijklmnopqrstuvwxyz"

var tidLock sync.Mutex
var lastTID uint64
var tidClock = func() uint64 {
	var value [2]byte
	_, _ = rand.Read(value[:])
	return uint64(binary.BigEndian.Uint16(value[:]) & 1023)
}()

func NewTID(at time.Time) string {
	tidLock.Lock()
	value := max(uint64(max(at.UnixMicro(), 0))<<10|tidClock, lastTID+1)
	lastTID = value
	tidLock.Unlock()
	var encoded [13]byte
	for i := len(encoded) - 1; i >= 0; i-- {
		encoded[i] = tidAlphabet[value&31]
		value >>= 5
	}
	return string(encoded[:])
}

func ValidTID(value string) bool {
	return len(value) == 13 && strings.ContainsRune("234567abcdefghij", rune(value[0])) &&
		!strings.ContainsFunc(value, func(r rune) bool { return !strings.ContainsRune(tidAlphabet, r) })
}
