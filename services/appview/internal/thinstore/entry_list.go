package thinstore

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// EntryListFilter selects all, unread, or read entries.
type EntryListFilter string

const (
	FilterAll    EntryListFilter = "all"
	FilterUnread EntryListFilter = "unread"
	FilterRead   EntryListFilter = "read"
)

// EntryListItem is one feed row (AppViewEntryListItem).
type EntryListItem struct {
	EntryID              string
	Title                string
	Summary              *string
	PublishedAt          time.Time
	ThumbnailURL         *string
	ThumbnailFallbackURL *string
	ContentLabels        []string
	// OriginalURL is the canonical article HTTPS URL when indexed.
	OriginalURL *string
	// PublicationID is the viewer-scoped publication identity for aggregate presentation.
	PublicationID *string
	// FeedPositionAt is the database ordering position used by cursors and read watermarks.
	FeedPositionAt time.Time
	// IsRead is the server-authoritative read state.
	IsRead bool
}

// WithPublicationID returns a copy carrying publicationID.
func (e EntryListItem) WithPublicationID(publicationID string) EntryListItem {
	e.PublicationID = &publicationID
	return e
}

// WithReadState returns a copy with IsRead set.
func (e EntryListItem) WithReadState(isRead bool) EntryListItem {
	e.IsRead = isRead
	return e
}

type entryListItemWire struct {
	EntryID              string       `json:"entryId"`
	Title                string       `json:"title"`
	Summary              *string      `json:"summary,omitempty"`
	PublishedAt          isotime.Time `json:"publishedAt"`
	ThumbnailURL         *string      `json:"thumbnailUrl,omitempty"`
	ThumbnailFallbackURL *string      `json:"thumbnailFallbackUrl,omitempty"`
	ContentLabels        []string     `json:"contentLabels,omitempty"`
	OriginalURL          *string      `json:"originalUrl,omitempty"`
	PublicationID        *string      `json:"publicationId,omitempty"`
	FeedPositionAt       isotime.Time `json:"feedPositionAt"`
	IsRead               bool         `json:"isRead"`
}

// MarshalJSON matches Swift's synthesized Codable with whole-second ISO dates.
func (e EntryListItem) MarshalJSON() ([]byte, error) {
	return json.Marshal(entryListItemWire{
		EntryID: e.EntryID, Title: e.Title, Summary: e.Summary,
		PublishedAt: isotime.Time{Time: e.PublishedAt}, ThumbnailURL: e.ThumbnailURL,
		ThumbnailFallbackURL: e.ThumbnailFallbackURL, ContentLabels: e.ContentLabels,
		OriginalURL: e.OriginalURL, PublicationID: e.PublicationID,
		FeedPositionAt: isotime.Time{Time: e.FeedPositionAt}, IsRead: e.IsRead,
	})
}

// UnmarshalJSON accepts the MarshalJSON shape; feedPositionAt defaults to publishedAt.
func (e *EntryListItem) UnmarshalJSON(data []byte) error {
	var wire struct {
		entryListItemWire
		FeedPositionAt *isotime.Time `json:"feedPositionAt"`
		IsRead         *bool         `json:"isRead"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*e = EntryListItem{
		EntryID: wire.EntryID, Title: wire.Title, Summary: wire.Summary,
		PublishedAt: wire.PublishedAt.Time, ThumbnailURL: wire.ThumbnailURL,
		ThumbnailFallbackURL: wire.ThumbnailFallbackURL, ContentLabels: wire.ContentLabels,
		OriginalURL: wire.OriginalURL, PublicationID: wire.PublicationID,
		FeedPositionAt: wire.PublishedAt.Time,
	}
	if wire.FeedPositionAt != nil {
		e.FeedPositionAt = wire.FeedPositionAt.Time
	}
	if wire.IsRead != nil {
		e.IsRead = *wire.IsRead
	}
	return nil
}

// EntryListResponse is one page of entries (AppViewEntryListResponse).
type EntryListResponse struct {
	Entries []EntryListItem `json:"entries"`
	Cursor  *string         `json:"cursor,omitempty"`
}

func emptyEntryListResponse() EntryListResponse {
	return EntryListResponse{Entries: []EntryListItem{}}
}

// AggregatePageDiagnostics reports one aggregate scan.
type AggregatePageDiagnostics struct {
	RowsScanned          int
	RowsReturned         int
	DuplicatesSuppressed int
	QueryDuration        time.Duration
}

// AggregatePageResult pairs an aggregate page with its diagnostics.
type AggregatePageResult struct {
	Response    EntryListResponse
	Diagnostics AggregatePageDiagnostics
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPointer(value string) *string { return &value }

func itoa64(value int64) string { return strconv.FormatInt(value, 10) }
