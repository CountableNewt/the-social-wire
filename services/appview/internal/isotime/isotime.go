// Package isotime formats and parses the timestamp shapes the backend stores and serves.
package isotime

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Seconds is the whole-second UTC form Foundation's ISO8601DateFormatter and
// JSONEncoder's .iso8601 strategy emit, e.g. 2026-11-20T12:00:00Z.
const Seconds = "2006-01-02T15:04:05Z"

// Micros is the canonical fixed-width microsecond UTC form, e.g. 2026-11-20T12:00:00.000000Z.
const Micros = "2006-01-02T15:04:05.000000Z"

// ErrInvalid reports a value that is not an RFC 3339 timestamp.
var ErrInvalid = errors.New("isotime: invalid timestamp")

var pattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$`)

// Format renders t as a whole-second UTC timestamp.
func Format(t time.Time) string { return t.UTC().Format(Seconds) }

// FormatMicros renders t as a fixed-width microsecond UTC timestamp.
func FormatMicros(t time.Time) string { return t.UTC().Format(Micros) }

// Parse accepts the strict RFC 3339 grammar ReadStateValidation.date accepts,
// keeping up to nanosecond fractions.
func Parse(value string) (time.Time, error) {
	match := pattern.FindStringSubmatch(value)
	if match == nil {
		return time.Time{}, ErrInvalid
	}
	whole, err := time.Parse(time.RFC3339, match[1]+match[3])
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	if match[2] == "" {
		return whole, nil
	}
	fraction := match[2] + strings.Repeat("0", 9-len(match[2]))
	nanos, err := strconv.Atoi(fraction)
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	return whole.Add(time.Duration(nanos)), nil
}

// Time marshals to JSON the way Swift's JSONEncoder .iso8601 strategy does.
type Time struct{ time.Time }

// MarshalJSON emits a whole-second UTC timestamp.
func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(`"` + Format(t.Time) + `"`), nil
}

// UnmarshalJSON accepts any RFC 3339 timestamp.
func (t *Time) UnmarshalJSON(data []byte) error {
	raw, err := strconv.Unquote(string(data))
	if err != nil {
		return ErrInvalid
	}
	parsed, err := Parse(raw)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}
