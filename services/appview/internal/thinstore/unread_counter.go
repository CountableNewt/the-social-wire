package thinstore

import (
	"encoding/json"
	"math"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// UnreadCounterAccuracy says whether a materialized counter was recounted.
type UnreadCounterAccuracy string

const (
	AccuracyEstimated UnreadCounterAccuracy = "estimated"
	AccuracyExact     UnreadCounterAccuracy = "exact"
)

// UnreadCounter is a materialized appview_unread_counters row.
type UnreadCounter struct {
	PublicationID string
	UnreadCount   int
	Generation    int64
	Accuracy      UnreadCounterAccuracy
	Dirty         bool
	CountedAt     time.Time
}

// NewUnreadCounter clamps negative counts to zero like the Swift initializer.
func NewUnreadCounter(publicationID string, unreadCount int, generation int64,
	accuracy UnreadCounterAccuracy, dirty bool, countedAt time.Time) UnreadCounter {
	return UnreadCounter{
		PublicationID: publicationID, UnreadCount: max(0, unreadCount), Generation: generation,
		Accuracy: accuracy, Dirty: dirty, CountedAt: countedAt,
	}
}

type unreadCounterWire struct {
	PublicationID string                `json:"publicationId"`
	UnreadCount   int                   `json:"unreadCount"`
	Generation    int64                 `json:"generation"`
	Accuracy      UnreadCounterAccuracy `json:"accuracy"`
	Dirty         bool                  `json:"dirty"`
	CountedAt     isotime.Time          `json:"countedAt"`
}

// MarshalJSON matches the synthesized Codable shape.
func (c UnreadCounter) MarshalJSON() ([]byte, error) {
	return json.Marshal(unreadCounterWire{c.PublicationID, c.UnreadCount, c.Generation, c.Accuracy, c.Dirty,
		isotime.Time{Time: c.CountedAt}})
}

// UnmarshalJSON accepts the MarshalJSON shape.
func (c *UnreadCounter) UnmarshalJSON(data []byte) error {
	var wire unreadCounterWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*c = NewUnreadCounter(wire.PublicationID, wire.UnreadCount, wire.Generation, wire.Accuracy, wire.Dirty,
		wire.CountedAt.Time)
	return nil
}

// CounterGeneration is milliseconds since the epoch, rounded (AppViewUnreadCounterSupport.generation).
func CounterGeneration(at time.Time) int64 {
	return int64(math.Round(float64(at.UnixNano()) / 1e6))
}
