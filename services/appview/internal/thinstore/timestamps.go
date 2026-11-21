package thinstore

import (
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// distantFuture mirrors Foundation's Date.distantFuture (4001-01-01T00:00:00Z).
var distantFuture = time.Date(4001, 1, 1, 0, 0, 0, 0, time.UTC)

// isoString is ISO8601DateFormatter's whole-second UTC form used for most columns.
func isoString(t time.Time) string { return isotime.Format(t) }

// microsTimestamp keeps the database's microsecond boundary (ThinAppViewCursor.timestamp),
// rounding rather than truncating like Swift's Double-based conversion.
func microsTimestamp(t time.Time) string {
	return t.UTC().Round(time.Microsecond).Format(isotime.Micros)
}

// pdsTimestamp is the microsecond form stored in content_items.created_at and read floors.
func pdsTimestamp(t time.Time) string { return microsTimestamp(t) }

// feedTimestampKey matches feedTimestampSQL's nine-digit fractional key.
func feedTimestampKey(t time.Time) string {
	value := microsTimestamp(t)
	return value[:len(value)-1] + "000Z"
}

// dateFromISO parses a stored timestamp with ReadStateValidation.date's strict grammar.
func dateFromISO(raw string) (time.Time, bool) {
	parsed, err := isotime.Parse(raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// dateOrZero mirrors `date(fromIso:) ?? Date.distantPast`.
func dateOrZero(raw string) time.Time {
	parsed, _ := dateFromISO(raw)
	return parsed
}

// parseISO8601Date accepts internet date-times with or without fractional seconds
// (ThinAppViewQuerySupport.parseISO8601Date).
func parseISO8601Date(raw string) (time.Time, bool) {
	return dateFromISO(raw)
}

// unixSeconds is Date.timeIntervalSince1970 for DOUBLE columns.
func unixSeconds(t time.Time) float64 {
	return float64(t.UnixNano()) / 1e9
}
