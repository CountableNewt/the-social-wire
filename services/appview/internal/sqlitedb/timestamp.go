package sqlitedb

import (
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// TimestampMicroseconds converts a persisted RFC 3339 timestamp without a
// UnixNano intermediate or floating-point loss. Original strings remain the
// wire representation; older sub-microsecond values round to the nearest
// microsecond, with exact halves rounded away from the Unix epoch.
func TimestampMicroseconds(raw string) (int64, error) {
	value, err := isotime.Parse(raw)
	if err != nil {
		return 0, err
	}
	return TimeMicroseconds(value), nil
}

// TimeMicroseconds accepts times in the RFC 3339 year range (0000–9999), whose
// microsecond counts fit int64 even where nanosecond counts would overflow.
func TimeMicroseconds(value time.Time) int64 {
	seconds, nanos := value.Unix(), int64(value.Nanosecond())
	micros := seconds*1_000_000 + nanos/1_000
	remainder := nanos % 1_000
	if remainder > 500 || (remainder == 500 && seconds >= 0) {
		micros++
	}
	return micros
}
