package operations

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// storeTimeLayout is ISO8601DateFormatter with .withFractionalSeconds, the
// form SQLiteOperationsStore writes: millisecond UTC, e.g. 2026-11-20T12:00:00.123Z.
const storeTimeLayout = "2006-01-02T15:04:05.000Z"

// storeTime formats a stored timestamp.
func storeTime(t time.Time) string { return t.UTC().Format(storeTimeLayout) }

// parseStoreTime accepts fractional and whole-second ISO 8601 values.
func parseStoreTime(value string) (time.Time, bool) {
	t, err := isotime.Parse(value)
	return t, err == nil
}

func parseNullTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	t, ok := parseStoreTime(value.String)
	if !ok {
		return nil
	}
	return &t
}

// swiftJSON encodes a string map the way Foundation's JSONEncoder does for the
// stored JSON columns: no HTML escaping, "/" escaped as "\/".
func swiftJSON(value map[string]string) string {
	if value == nil {
		value = map[string]string{}
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "{}"
	}
	return strings.ReplaceAll(strings.TrimSuffix(buffer.String(), "\n"), "/", `\/`)
}

func decodeStringMap(value string) map[string]string {
	result := map[string]string{}
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return map[string]string{}
	}
	return result
}
