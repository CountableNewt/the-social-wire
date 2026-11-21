package sqlitedb

import (
	"database/sql/driver"
	"math"
	"strings"

	"modernc.org/sqlite"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// registerExtensions installs the tsw_timestamp collation and tsw_timestamp_us
// function the Swift store registered on every connection. Existing indexes
// and queries name them, so they must exist before any statement runs.
func registerExtensions() error {
	if err := sqlite.RegisterCollationUtf8("tsw_timestamp", CompareTimestamps); err != nil {
		return err
	}
	return sqlite.RegisterDeterministicScalarFunction("tsw_timestamp_us", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			raw, ok := textValue(args[0])
			if !ok {
				return nil, nil
			}
			parsed, err := isotime.Parse(raw)
			if err != nil {
				return nil, err
			}
			micros := float64(parsed.UnixNano()) / 1_000
			return int64(math.Round(micros)), nil
		})
}

func textValue(value driver.Value) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	default:
		return "", false
	}
}

// CompareTimestamps orders canonical UTC values without parsing, and falls
// back to parsed instants, then plain text, for older or offset values.
func CompareTimestamps(left, right string) int {
	if strings.HasSuffix(left, "Z") && strings.HasSuffix(right, "Z") &&
		(len(left) == 20 || len(left) == 27) && (len(right) == 20 || len(right) == 27) {
		return strings.Compare(padMicros(left), padMicros(right))
	}
	a, errA := isotime.Parse(left)
	b, errB := isotime.Parse(right)
	if errA != nil || errB != nil {
		return strings.Compare(left, right)
	}
	return a.Compare(b)
}

func padMicros(value string) string {
	if len(value) == 20 {
		return value[:19] + ".000000Z"
	}
	return value
}
