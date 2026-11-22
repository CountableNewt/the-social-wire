package operations

import (
	"testing"
	"time"
)

func TestFreshnessDoesNotInferGlobalIngestionHealth(t *testing.T) {
	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	response := EvaluateFreshness("appview", now)
	if response.Service != "appview" || response.Freshness != Unknown || response.Completeness != Unknown || !response.CheckedAt.Time.Equal(now) {
		t.Fatalf("unexpected freshness: %+v", response)
	}
}
