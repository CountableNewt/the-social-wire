import AsyncHTTPClient
import GatewayCore
import Foundation
import Hummingbird
import Logging
import ThinAppViewCore

actor ThinAppViewEnrollService {
  private let backfill: ThinAppViewEnrollBackfill
  private let publicRefresh: ScopedContentRefresh
  private let publicHistory: ScopedContentHistory
  private let skyreaderIngestion: ThinAppViewSkyreaderIngestionService?
  private let config: ThinAppViewConfig
  private let logger: Logger

  init(
    store: any ThinAppViewStore,
    indexer: ThinAppViewIndexer,
    httpClient: HTTPClient,
    plcURL: String,
    config: ThinAppViewConfig,
    logger: Logger,
    skyreaderIngestion: ThinAppViewSkyreaderIngestionService? = nil,
    publicContentProvider: (any PublicContentDiscovering)? = nil,
    publicContentPDS: (@Sendable (String, Bool) async throws -> Int)? = nil
  ) {
    let backfill = ThinAppViewEnrollBackfill(
      store: store,
      indexer: indexer,
      httpClient: httpClient,
      plcURL: plcURL,
      config: config,
      logger: logger
    )
    self.backfill = backfill
    let enumerate: @Sendable (String, Bool) async throws -> Int = publicContentPDS ?? { authorDid, recent in
      try await backfill.enroll(authorDids: [authorDid], recentOnly: recent)
    }
    self.publicRefresh = ScopedContentRefresh(provider: publicContentProvider, logger: logger) { authorDid in
      try await enumerate(authorDid, true)
    }
    self.publicHistory = ScopedContentHistory(logger: logger) { authorDid in
      try await enumerate(authorDid, false)
    }
    self.skyreaderIngestion = skyreaderIngestion
    self.config = config
    self.logger = logger
  }

  static func discoveryScope(authorDid: String, publicationAtUri: String?, publicationScopeAtUris: [String] = [],
    publicationSiteUrls: [String] = []) throws -> PublicContentDiscoveryScope {
    let keys = Array(Set(([publicationAtUri].compactMap { $0 } + publicationScopeAtUris + publicationSiteUrls))).sorted()
    let valid = keys.prefix(8).compactMap { key in
      try? PublicContentDiscoveryScope(authorDid: authorDid, publicationKeys: [key])
    }.flatMap(\.publicationKeys)
    return try PublicContentDiscoveryScope(authorDid: authorDid, publicationKeys: valid)
  }

  func refreshPublicContent(authorDid: String, publicationAtUri: String?, publicationScopeAtUris: [String] = [],
    publicationSiteUrls: [String] = []) async throws -> Int {
    guard authorDid != PublicationLexicons.rssAuthorDid else { return 0 }
    let scope = try Self.discoveryScope(authorDid: authorDid, publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris, publicationSiteUrls: publicationSiteUrls)
    await publicHistory.schedule(authorDid: authorDid)
    return try await publicRefresh.refresh(scope: scope)
  }

  func refreshPublicContentFast(authorDid: String, publicationAtUri: String?, publicationScopeAtUris: [String] = [],
    publicationSiteUrls: [String] = []) async throws -> Int {
    guard authorDid != PublicationLexicons.rssAuthorDid else { return 0 }
    let scope = try Self.discoveryScope(authorDid: authorDid, publicationAtUri: publicationAtUri,
      publicationScopeAtUris: publicationScopeAtUris, publicationSiteUrls: publicationSiteUrls)
    await publicHistory.schedule(authorDid: authorDid)
    if !scope.publicationKeys.isEmpty {
      Task { _ = try? await publicRefresh.refresh(scope: scope) }
    }
    return try await publicRefresh.refresh(scope: PublicContentDiscoveryScope(authorDid: authorDid, publicationKeys: []))
  }

  func shutdown() async {
    async let history: Void = publicHistory.shutdown()
    async let refresh: Void = publicRefresh.shutdown()
    _ = await (history, refresh)
  }

  func enroll(
    auth: AuthContext,
    authorDids: [String],
    feedUrls: [String] = [],
    recentOnly: Bool = true
  ) async throws -> Int {
    let uniqueAuthors = Array(
      Set(authorDids.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty })
    )
    .prefix(config.maxEnrollAuthors)

    var indexed = 0
    for author in uniqueAuthors { await publicHistory.schedule(authorDid: author) }
    if recentOnly {
      for author in uniqueAuthors where ThinAppViewEnrollBackfill.isBackfillEligibleAuthorDid(author)
        && author != PublicationLexicons.rssAuthorDid {
        indexed += try await publicRefresh.refresh(scope: PublicContentDiscoveryScope(authorDid: author, publicationKeys: []))
      }
    }
    // Full history is scheduled above with independent concurrency/deadline
    // bounds; it must not hold a bootstrap response or duplicate another sweep.

    let priorityFeedUrls = feedUrls
      .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
      .filter { !$0.isEmpty }
    if let skyreaderIngestion, !priorityFeedUrls.isEmpty {
      indexed += try await skyreaderIngestion.ingestViewerSubscriptions(
        auth: auth,
        priorityFeedUrls: priorityFeedUrls
      )
    }

    logger.info(
      "Enrollment backfill complete",
      metadata: [
        "viewer": .string(auth.did),
        "authors": .stringConvertible(uniqueAuthors.count),
        "feeds": .stringConvertible(priorityFeedUrls.count),
        "records": .stringConvertible(indexed),
      ]
    )
    return indexed
  }
}
