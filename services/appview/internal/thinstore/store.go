package thinstore

import (
	"context"
	"errors"
	"time"
)

// ErrReadStateAlreadyMigrated mirrors PDSReadStateStorageError.alreadyMigrated: a
// legacy AppView read-state mutation was attempted for a viewer whose read state
// is now PDS-authoritative (appview_pds_read_state_authority.manifest_cid set).
var ErrReadStateAlreadyMigrated = errors.New("thinstore: read state already migrated to the PDS")

// ErrAmbiguousReadState mirrors ThinAppViewStoreError.ambiguousReadState: the
// ID-only ReadStates API cannot represent conflicting publication floors.
var ErrAmbiguousReadState = errors.New("thinstore: ambiguous read state")

// ListEntriesParams selects one author's entries, optionally narrowed to a publication.
type ListEntriesParams struct {
	ViewerDID              string
	AuthorDID              string
	PublicationAtURI       *string
	PublicationScopeAtURIs []string
	PublicationSiteURLs    []string
	Filter                 EntryListFilter
	Cursor                 *string
	Limit                  int
	ReadBoundary           *ReadWatermarkBoundary
}

// Store is persistence for thin AppView content_items, read marks, unread
// counters and viewer feed projections (ThinAppViewStore). It covers every
// method the standalone AppView services, indexer, RSS ingestion and the
// maintenance cleanup job call.
type Store interface {
	Ping(ctx context.Context) error

	UpsertContentItem(ctx context.Context, item IndexedContentItem) error
	// InsertContentItemIfAbsent never replaces an existing row (PublicContentInsertStoring).
	InsertContentItemIfAbsent(ctx context.Context, item IndexedContentItem) (bool, error)
	DeleteContentItem(ctx context.Context, uri string) error
	DeleteContentItemsByAuthor(ctx context.Context, authorDID string) (int, error)
	DeleteContentItemsExcluding(ctx context.Context, authorDID string, excludingURIs []string, indexedAtOrBefore time.Time) (int, error)
	FetchContentIdentity(ctx context.Context, uri string) (*IndexedContentIdentity, error)

	UpsertReadMark(ctx context.Context, viewerDID, subjectURI string, createdAt time.Time) error
	// UpsertReadMarks marks subjects read without advancing any publication floor and
	// marks affected counters dirty; callers refresh them and invalidate scoped caches.
	UpsertReadMarks(ctx context.Context, viewerDID string, subjectURIs []string, createdAt time.Time) error
	DeleteReadMark(ctx context.Context, viewerDID, subjectURI string) error
	MarkEntryUnread(ctx context.Context, viewerDID, subjectURI string, createdAt time.Time) error
	PurgeReadMarks(ctx context.Context, viewerDID string) error

	FetchContentItem(ctx context.Context, uri string) (*EntryListItem, error)
	FetchContentRender(ctx context.Context, uri string) (*ContentRenderFields, error)
	HasReadMark(ctx context.Context, viewerDID, subjectURI string) (bool, error)
	ReadStates(ctx context.Context, viewerDID string, entries []EntryListItem) (map[string]bool, error)

	ListEntries(ctx context.Context, params ListEntriesParams) (EntryListResponse, error)
	ListFeedEntries(ctx context.Context, viewerDID string, scopes []PublicationUnreadScope, filter EntryListFilter, cursor *string, limit int) (EntryListResponse, error)
	// ListSelectorFeedEntries returns nil when the selector's feed is not materialized.
	ListSelectorFeedEntries(ctx context.Context, viewerDID string, selector FeedSelector, filter EntryListFilter, cursor *string, limit int) (*FeedPage, error)
	HasViewerFeedProjection(ctx context.Context, viewerDID string) (bool, error)
	PublicationScopes(ctx context.Context, viewerDID, sectionKey string) ([]PublicationScope, error)
	ListAggregateEntries(ctx context.Context, viewerDID string, scopes []PublicationScope, filter EntryListFilter, cursor *string, limit int) (AggregatePageResult, error)
	// ListUnreadEntriesForReadMutation returns every unread record in scope, including
	// records hidden by feed URL deduplication.
	ListUnreadEntriesForReadMutation(ctx context.Context, viewerDID string, scopes []PublicationScope, cursor *string, limit int) (UnreadReadMutationPage, error)
	ReadBoundary(ctx context.Context, viewerDID, publicationID string) (*ReadWatermarkBoundary, error)

	CountUnreadEntries(ctx context.Context, viewerDID, authorDID string, publicationAtURI *string, publicationScopeAtURIs, publicationSiteURLs []string) (int, error)
	CountUnreadEntriesBatch(ctx context.Context, viewerDID string, scopes []PublicationUnreadScope) (map[string]int, error)

	UpsertPublicationScopes(ctx context.Context, scopes []PublicationScope) error
	ReplacePublicationScopes(ctx context.Context, viewerDID string, scopes []PublicationScope) error
	UpsertViewerFeedProjection(ctx context.Context, viewerDID string, scopes []PublicationScope, feeds []ViewerFeed, memberships []FeedPublication) error
	ReplaceViewerFeedProjection(ctx context.Context, viewerDID string, scopes []PublicationScope, feeds []ViewerFeed, memberships []FeedPublication) error

	// FetchUnreadCounters returns all counters when publicationIDs is empty.
	FetchUnreadCounters(ctx context.Context, viewerDID string, publicationIDs []string) ([]UnreadCounter, error)
	RefreshUnreadCounters(ctx context.Context, viewerDID string, scopes []PublicationUnreadScope) ([]UnreadCounter, error)
	IncrementUnreadCountersForContentItem(ctx context.Context, item IndexedContentItem) error
	MarkUnreadCountersDirtyForContent(ctx context.Context, authorDID string, publicationSite *string) error
	MarkUnreadCountersDirtyForAuthor(ctx context.Context, authorDID string) error
	AdjustUnreadCountersForReadState(ctx context.Context, viewerDID, subjectURI string, delta int) error
	MarkAllReadCounters(ctx context.Context, viewerDID string, scopes []PublicationUnreadScope, readAt time.Time) ([]UnreadCounter, []ReadWatermarkBoundary, error)

	DeleteExpiredCircleCaches(ctx context.Context, before time.Time, batchSize int) (int, error)
	DeleteExpiredContent(ctx context.Context, before time.Time, batchSize int) (int, error)
	DeleteExpiredReadMarks(ctx context.Context, before time.Time, batchSize int) (int, error)
	DeleteExpiredIngestionInbox(ctx context.Context, environment string, before time.Time, batchSize int) (int, error)
	DeleteExpiredTapEventReceipts(ctx context.Context, environment string, before time.Time, batchSize int) (int, error)
	DeleteExpiredProjectionRepairs(ctx context.Context, environment string, before time.Time, batchSize int) (int, error)

	// ListAuthorDIDsForProactiveBackfill returns authors with the stalest index.
	ListAuthorDIDsForProactiveBackfill(ctx context.Context, limit int) ([]string, error)
	// ListRssPublicationSites returns distinct RSS feed URLs for Skyreader poll refresh.
	ListRssPublicationSites(ctx context.Context, limit int) ([]string, error)
	FetchRssFeedMetadata(ctx context.Context, normalizedFeedURL string) (*RssFeedFetchMetadata, error)
	StoreRssFeedMetadata(ctx context.Context, metadata RssFeedFetchMetadata) error
	// ListContentItemsForPublicationSite lists one RSS feed's rows (duplicate cleanup).
	ListContentItemsForPublicationSite(ctx context.Context, authorDID, publicationSite string, limit int) ([]ContentURIRender, error)

	// ContentVerificationMetadataStoring.
	ContentVerificationMetadata(ctx context.Context, uri string) (*ContentVerificationMetadata, error)
	UpsertContentItemExpectingCID(ctx context.Context, item IndexedContentItem, expectedCID *string) (bool, error)
	DeleteContentItemExpectingCID(ctx context.Context, uri, expectedCID string) (bool, error)

	// FinanceSelectionStoring / SportsSelectionStoring.
	ApplyFinanceSelection(ctx context.Context, mutation FinanceSelectionMutation) error
	ApplySportsSelection(ctx context.Context, mutation SportsSelectionMutation) error
}

// IngestionScopePolicy mirrors AppViewIngestionScopePolicy.
var IngestionScopePolicy = struct {
	Version                      string
	PublicationAuthorCollections []string
	ViewerCollections            []string
}{
	Version: "publication-author-viewer-v1",
	PublicationAuthorCollections: []string{
		"site.standard.document", "site.standard.entry", "com.standard.document", "com.standard.entry",
	},
	ViewerCollections: []string{
		"app.thesocialwire.finance.selection", "app.thesocialwire.sports.selection",
		"app.thesocialwire.readState", "app.skyreader.feed.subscription", "site.standard.graph.subscription",
	},
}
