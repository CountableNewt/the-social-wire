package scheduling

import (
	"testing"
	"time"
)

func TestQuarterHourPublishTimes(t *testing.T) {
	for _, value := range []string{"2099-01-01T12:00:00Z", "2099-01-01T12:15:00Z", "2099-01-01T12:30:00Z", "2099-01-01T12:45:00Z", "2099-01-01T12:15:00+05:45"} {
		date, _ := time.Parse(time.RFC3339Nano, value)
		if !IsQuarterHour(date) {
			t.Fatalf("valid quarter hour rejected: %s", value)
		}
	}
	for _, value := range []string{"2099-01-01T12:01:00Z", "2099-01-01T12:15:01Z", "2099-01-01T12:15:00.001Z"} {
		date, _ := time.Parse(time.RFC3339Nano, value)
		if IsQuarterHour(date) {
			t.Fatalf("off-grid time accepted: %s", value)
		}
	}
}
