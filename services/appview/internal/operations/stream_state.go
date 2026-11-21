package operations

import "time"

// StreamState is IngestionStreamState without the derived queue evidence,
// read from appview_ingestion_stream_state.
type StreamState struct {
	Environment           string
	Source                string
	ConnectionState       string
	ConnectedAt           *time.Time
	LastDisconnectAt      *time.Time
	LastDisconnectReason  *string
	LastReceivedCursor    *int64
	LastReceivedEventAt   *time.Time
	LastReceivedAt        *time.Time
	LastCommittedCursor   *int64
	LastCommittedEventAt  *time.Time
	LastCommittedAt       *time.Time
	QueueDepth            int
	QueueCapacity         *int
	QueueOverflowTotal    *int64
	QueueObservedAt       *time.Time
	TransportHeartbeatAt  *time.Time
	LastIndexedMutationAt *time.Time
	ProjectionWatermark   *string
	ValidationWatermark   *string
	HeartbeatAt           time.Time
	Version               int
}
