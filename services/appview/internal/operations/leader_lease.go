package operations

import "time"

// LeaderLease is IngestionLeaderLease: a fenced, named lease row in
// appview_ingestion_leases.
type LeaderLease struct {
	Environment      string
	Name             string
	SourceGeneration string
	OwnerID          string
	FencingToken     int64
	AcquiredAt       time.Time
	ExpiresAt        time.Time
	UpdatedAt        time.Time
}
