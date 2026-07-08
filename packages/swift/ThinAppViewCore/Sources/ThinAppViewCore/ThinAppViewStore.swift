import Foundation

public struct RssFeedFetchMetadata: Sendable {
  public let normalizedFeedUrl: String
  public let etag: String?
  public let lastModified: String?
  public let lastPollAt: Date?
  public let backoffUntil: Date?
  public let consecutiveErrorCount: Int

  public init(
    normalizedFeedUrl: String,
    etag: String?,
    lastModified: String?,
    lastPollAt: Date?,
    backoffUntil: Date?,
    consecutiveErrorCount: Int
  ) {
    self.normalizedFeedUrl = normalizedFeedUrl
    self.etag = etag
    self.lastModified = lastModified
    self.lastPollAt = lastPollAt
    self.backoffUntil = backoffUntil
    self.consecutiveErrorCount = consecutiveErrorCount
  }
}

/// Persistence for thin AppView `content_items` and `read_marks`.
public protocol ThinAppViewStore: Actor {
  func upsertContentItem(_ item: IndexedContentItem) async throws
  func deleteContentItem(uri: String) async throws

  func upsertReadMark(viewerDid: String, subjectUri: String, createdAt: Date) async throws
  func deleteReadMark(viewerDid: String, subjectUri: String) async throws
  func purgeReadMarks(viewerDid: String) async throws

  func fetchContentItem(uri: String) async throws -> AppViewEntryListItem?
  func hasReadMark(viewerDid: String, subjectUri: String) async throws -> Bool

  func listEntries(
    viewerDid: String,
    authorDid: String,
    publicationAtUri: String?,
    publicationScopeAtUris: [String],
    publicationSiteUrls: [String],
    filter: EntryListFilter,
    cursor: String?,
    limit: Int
  ) async throws -> AppViewEntryListResponse

  func countUnreadEntries(
    viewerDid: String,
    authorDid: String,
    publicationAtUri: String?,
    publicationScopeAtUris: [String],
    publicationSiteUrls: [String]
  ) async throws -> Int

  func countUnreadEntriesBatch(
    viewerDid: String,
    scopes: [PublicationUnreadScope]
  ) async throws -> [String: Int]

  func upsertPublicationScopes(_ scopes: [AppViewPublicationScope]) async throws

  func replacePublicationScopes(
    viewerDid: String,
    scopes: [AppViewPublicationScope]
  ) async throws

  func fetchUnreadCounters(
    viewerDid: String,
    publicationIds: [String]?
  ) async throws -> [AppViewUnreadCounter]

  func refreshUnreadCounters(
    viewerDid: String,
    scopes: [PublicationUnreadScope]
  ) async throws -> [AppViewUnreadCounter]

  func incrementUnreadCountersForContentItem(_ item: IndexedContentItem) async throws

  func markUnreadCountersDirtyForContent(authorDid: String, publicationSite: String?) async throws

  func adjustUnreadCountersForReadState(
    viewerDid: String,
    subjectUri: String,
    delta: Int
  ) async throws

  func markAllReadCounters(
    viewerDid: String,
    publicationIds: [String],
    readAt: Date
  ) async throws -> [AppViewUnreadCounter]

  func deleteExpiredContent(before: Date) async throws -> Int
  func deleteExpiredReadMarks(before: Date) async throws -> Int

  func recordIngestionCheckpoint(
    source: String,
    repoDid: String,
    collection: String,
    cursor: String?,
    eventTime: Date?,
    observedAt: Date
  ) async throws

  /// Authors with the stalest index; used by the worker proactive backfill loop.
  func listAuthorDidsForProactiveBackfill(limit: Int) async throws -> [String]

  /// Distinct RSS feed URLs (`publication_site`) for Skyreader poll refresh.
  func listRssPublicationSites(limit: Int) async throws -> [String]

  func fetchRssFeedMetadata(normalizedFeedUrl: String) async throws -> RssFeedFetchMetadata?
  func storeRssFeedMetadata(_ metadata: RssFeedFetchMetadata) async throws

  func fetchContentRender(uri: String) async throws -> ContentRenderFields?

  /// Lists indexed rows for one RSS feed URL (Skyreader duplicate cleanup).
  func listContentItemsForPublicationSite(
    authorDid: String,
    publicationSite: String,
    limit: Int
  ) async throws -> [(uri: String, renderJSON: String)]
}
