package sqlitedb

import "testing"

func TestTimestampMicrosecondsPreservesInstantsAndRoundsExactly(t *testing.T) {
	for _, example := range []struct {
		raw  string
		want int64
	}{
		{"1970-01-01T00:00:00Z", 0},
		{"1970-01-01T01:00:00+01:00", 0},
		{"1970-01-01T00:00:00.1Z", 100000},
		{"1970-01-01T00:00:00.000000499Z", 0},
		{"1970-01-01T00:00:00.000000500Z", 1},
		{"1970-01-01T00:00:00.999999499Z", 999999},
		{"1970-01-01T00:00:00.999999500Z", 1000000},
		{"1969-12-31T23:59:59.999999501Z", 0},
		{"1969-12-31T23:59:59.999999500Z", -1},
		{"1969-12-31T23:59:59.999998500Z", -2},
		{"0001-01-01T00:00:00Z", -62135596800000000},
		{"9999-12-31T23:59:59.999999Z", 253402300799999999},
	} {
		t.Run(example.raw, func(t *testing.T) {
			got, err := TimestampMicroseconds(example.raw)
			if err != nil || got != example.want {
				t.Fatalf("got %d (%v), want %d", got, err, example.want)
			}
		})
	}
	for _, raw := range []string{"", "not-a-date", "2026-02-30T00:00:00Z", "2026-01-01T00:00:00.0000000001Z"} {
		if _, err := TimestampMicroseconds(raw); err == nil {
			t.Fatalf("accepted invalid timestamp %q", raw)
		}
	}
}
