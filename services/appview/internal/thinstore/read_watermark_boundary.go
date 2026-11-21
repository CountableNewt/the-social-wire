package thinstore

import (
	"encoding/json"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// ReadWatermarkBoundary is a publication read floor: everything at or before
// (CreatedAt, EntryID) is read unless explicitly overridden.
type ReadWatermarkBoundary struct {
	PublicationID string
	CreatedAt     time.Time
	EntryID       *string
}

// Contains reports whether a candidate position is covered by the floor.
func (b ReadWatermarkBoundary) Contains(candidateDate time.Time, candidateID string) bool {
	if !candidateDate.Equal(b.CreatedAt) {
		return candidateDate.Before(b.CreatedAt)
	}
	if b.EntryID == nil {
		return true
	}
	return candidateID <= *b.EntryID
}

// IsAfter reports whether b is a strictly newer floor than other.
func (b ReadWatermarkBoundary) IsAfter(other ReadWatermarkBoundary) bool {
	if !b.CreatedAt.Equal(other.CreatedAt) {
		return b.CreatedAt.After(other.CreatedAt)
	}
	switch {
	case b.EntryID == nil && other.EntryID == nil:
		return false
	case b.EntryID == nil:
		return true
	case other.EntryID == nil:
		return false
	default:
		return *b.EntryID > *other.EntryID
	}
}

type readWatermarkBoundaryWire struct {
	PublicationID string       `json:"publicationId"`
	CreatedAt     isotime.Time `json:"createdAt"`
	EntryID       *string      `json:"entryId,omitempty"`
}

// MarshalJSON matches the synthesized Codable shape.
func (b ReadWatermarkBoundary) MarshalJSON() ([]byte, error) {
	return json.Marshal(readWatermarkBoundaryWire{b.PublicationID, isotime.Time{Time: b.CreatedAt}, b.EntryID})
}

// UnmarshalJSON accepts the MarshalJSON shape.
func (b *ReadWatermarkBoundary) UnmarshalJSON(data []byte) error {
	var wire readWatermarkBoundaryWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*b = ReadWatermarkBoundary{wire.PublicationID, wire.CreatedAt.Time, wire.EntryID}
	return nil
}
