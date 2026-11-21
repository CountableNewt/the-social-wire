package thinstore

import "time"

// IndexedContentItem is one content_items row to write.
type IndexedContentItem struct {
	URI             string
	CID             string
	AuthorDID       string
	Collection      string
	CreatedAt       time.Time
	IndexedAt       time.Time
	PublicationSite *string
	Render          ContentRenderFields
	ExpiresAt       time.Time
}

// IndexedContentIdentity is the identity of a live content row.
type IndexedContentIdentity struct {
	URI        string
	CID        string
	AuthorDID  string
	Collection string
}

// ContentURIRender pairs a content URI with its stored render JSON.
type ContentURIRender struct {
	URI        string
	RenderJSON string
}

// ContentVerificationMetadata is the stored version an authoritative verifier observed.
type ContentVerificationMetadata struct {
	CID             string
	PublicationSite *string
}

// ContentVerificationExpectation lets a verifier replace only the local version
// observed before its network check; a nil CID means the row must still be absent.
type ContentVerificationExpectation struct {
	CID *string
}

// RssFeedFetchMetadata is the conditional-GET and backoff state for one feed URL.
type RssFeedFetchMetadata struct {
	NormalizedFeedURL     string
	ETag                  *string
	LastModified          *string
	LastPollAt            *time.Time
	BackoffUntil          *time.Time
	ConsecutiveErrorCount int
}
