package thinstore

import (
	"strings"
	"time"
)

// FeedKind names a materialized viewer feed (AppViewFeedKind).
type FeedKind string

const (
	FeedSubscribed      FeedKind = "subscribed"
	FeedFollowing       FeedKind = "following"
	FeedFolder          FeedKind = "folder"
	FeedKindPublication FeedKind = "publication"
	FeedList            FeedKind = "list"
)

// RequiresID reports whether the kind addresses one folder, publication or list.
func (k FeedKind) RequiresID() bool {
	return k == FeedFolder || k == FeedKindPublication || k == FeedList
}

// ParseFeedKind returns the kind for a raw value.
func ParseFeedKind(raw string) (FeedKind, bool) {
	switch FeedKind(raw) {
	case FeedSubscribed, FeedFollowing, FeedFolder, FeedKindPublication, FeedList:
		return FeedKind(raw), true
	}
	return "", false
}

// FeedSelector addresses one materialized feed (AppViewFeedSelector).
type FeedSelector struct {
	Kind FeedKind `json:"kind"`
	ID   string   `json:"id"`
}

// NewFeedSelector trims the id and reduces folder AT-URIs to their record key.
func NewFeedSelector(kind FeedKind, id string) FeedSelector {
	trimmed := strings.TrimSpace(id)
	if kind == FeedFolder && strings.HasPrefix(trimmed, "at://") {
		parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '/' })
		if len(parts) > 0 {
			trimmed = parts[len(parts)-1]
		}
	}
	return FeedSelector{Kind: kind, ID: trimmed}
}

// ViewerFeed is one appview_viewer_feeds row.
type ViewerFeed struct {
	ViewerDID string
	Kind      FeedKind
	FeedID    string
	UpdatedAt time.Time
}

// FeedPublication is one appview_feed_publications membership row.
type FeedPublication struct {
	ViewerDID     string
	Kind          FeedKind
	FeedID        string
	PublicationID string
}

// FeedPage is a selector feed page with its membership freshness.
type FeedPage struct {
	Response                     EntryListResponse
	MembershipUpdatedAt          time.Time
	DatabaseDurationMilliseconds float64
}
