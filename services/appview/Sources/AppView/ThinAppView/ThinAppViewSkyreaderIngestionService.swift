import Foundation
import GatewayCore
import Logging
import ThinAppViewCore

actor ThinAppViewSkyreaderIngestionService {
  private let viewerFeedUrls: @Sendable (AuthContext) async throws -> [String]
  private let ingestFeed: @Sendable (String) async throws -> Int
  private let refreshFeed: @Sendable (String) async throws -> Int
  private let logger: Logger

  init(
    repo: ATProtoAuthenticatedRepoClient,
    rssIngestion: ThinAppViewRssIngestion,
    store: any ThinAppViewStore,
    logger: Logger
  ) {
    self.viewerFeedUrls = { auth in
      let records = try await repo.listAllRecords(
        auth: auth,
        repo: auth.did,
        collection: PublicationLexicons.skyreaderFeedSubscription,
        maxPages: 20
      )
      return records.compactMap {
        ThinAppViewRssIngestion.feedUrl(fromSubscriptionRecord: $0.value.values)
      }
    }
    self.ingestFeed = { try await rssIngestion.ingestFeed(normalizedFeedUrl: $0) }
    self.refreshFeed = { url in
      try Task.checkCancellation()
      let previous = try await store.fetchRssFeedMetadata(normalizedFeedUrl: url)
      let started = Date()
      try AppViewRSSRefreshEvidence.checkBackoff(previous, feedUrl: url, at: started)
      let indexed = try await rssIngestion.ingestFeed(normalizedFeedUrl: url)
      try Task.checkCancellation()
      let current = try await store.fetchRssFeedMetadata(normalizedFeedUrl: url)
      // The shared background ingester intentionally suppresses fetch/metadata
      // errors. Explicit refresh requires persisted evidence of a completed poll.
      try AppViewRSSRefreshEvidence.checkCompletion(current, feedUrl: url, started: started)
      return indexed
    }
    self.logger = logger
  }

  init(
    viewerFeedUrls: @escaping @Sendable (AuthContext) async throws -> [String],
    ingestFeed: @escaping @Sendable (String) async throws -> Int,
    refreshFeed: (@Sendable (String) async throws -> Int)? = nil,
    logger: Logger
  ) {
    self.viewerFeedUrls = viewerFeedUrls
    self.ingestFeed = ingestFeed
    self.refreshFeed = refreshFeed ?? ingestFeed
    self.logger = logger
  }

  /// Explicit refresh is scoped to requested subscriptions and retains healthy
  /// results when other publishers fail. Unknown/storage failures remain fatal.
  func refreshViewerSubscriptions(auth: AuthContext, feedUrls: [String]) async throws -> Int {
    try Task.checkCancellation()
    guard !feedUrls.isEmpty else { return 0 }
    let subscribed = Self.uniqueFeedUrls(try await viewerFeedUrls(auth))
    let allowed = Set(subscribed)
    // Decoded subscriptions already have canonical IDs. Normalize only inputs
    // which are not exact matches; repeated normalization can trim another slash.
    let requested = Set(feedUrls.compactMap { raw -> String? in
      let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
      if allowed.contains(trimmed) { return trimmed }
      guard let normalized = RssFeedIdentity.normalizeFeedUrl(trimmed), allowed.contains(normalized) else { return nil }
      return normalized
    })
    var indexed = 0
    var failures: [AppViewRSSRefreshFailure] = []
    for url in subscribed where requested.contains(url) {
      try Task.checkCancellation()
      do {
        indexed += try await refreshFeed(url)
      } catch let failure as AppViewRSSRefreshFailure {
        failures.append(AppViewRSSRefreshFailure(feedUrl: url, category: failure.category))
      }
      try Task.checkCancellation()
    }
    if !failures.isEmpty {
      throw AppViewEnrollmentPartialFailure(indexed: indexed, failures: failures)
    }
    return indexed
  }

  /// Warm only the selected publications. Full subscription discovery belongs to background warming.
  func ingestSelectedFeeds(feedUrls: [String]) async throws -> Int {
    try await ingestOrderedFeeds(Self.normalizedUniqueFeedUrls(feedUrls))
  }

  func ingestViewerSubscriptions(
    auth: AuthContext,
    priorityFeedUrls: [String] = []
  ) async throws -> Int {
    try Task.checkCancellation()
    let feedUrls = try await viewerFeedUrls(auth)
    // Subscription URLs were normalized during record decoding; preserve their existing identity.
    let ordered = Self.uniqueFeedUrls(
      priorityFeedUrls.compactMap { RssFeedIdentity.normalizeFeedUrl($0) } + feedUrls
    )
    let total = try await ingestOrderedFeeds(ordered)

    if total > 0 {
      logger.info(
        "Indexed viewer Skyreader subscriptions",
        metadata: [
          "viewer": .string(auth.did),
          "feeds": .stringConvertible(ordered.count),
          "items": .stringConvertible(total),
        ]
      )
    }
    return total
  }

  private func ingestOrderedFeeds(_ feedUrls: [String]) async throws -> Int {
    try Task.checkCancellation()
    var total = 0
    for url in feedUrls {
      try Task.checkCancellation()
      total += try await ingestFeed(url)
      try Task.checkCancellation()
    }
    return total
  }

  private static func normalizedUniqueFeedUrls(_ feedUrls: [String]) -> [String] {
    uniqueFeedUrls(feedUrls.compactMap { RssFeedIdentity.normalizeFeedUrl($0) })
  }

  private static func uniqueFeedUrls(_ feedUrls: [String]) -> [String] {
    var seen = Set<String>()
    return feedUrls.filter { seen.insert($0).inserted }
  }
}
