import Foundation
import GatewayCore
import Hummingbird
import Logging
import NIOCore
import ThinAppViewCore

/// Explicit viewer refresh of one feed. Every publication in scope is checked with
/// bounded concurrency, and each completion is streamed so clients can show
/// progress and repaint as articles land instead of guessing when work finished.
struct FeedRefreshStreamService: Sendable {
  let readService: ThinAppViewReadService
  let enrollService: ThinAppViewEnrollService
  let projectionService: PublicationProjectionService
  let skyreaderIngestionService: ThinAppViewSkyreaderIngestionService
  let listsService: StandardReaderListsService?
  let logger: Logger

  /// Matches the provider job cap in `ScopedContentRefresh`.
  static let concurrency = 4
  static let maximumPublications = 500

  struct Target: Sendable, Equatable {
    let publicationId: String
    let authorDid: String
    let publicationAtUri: String?
    let publicationScopeAtUris: [String]
    let publicationSiteUrls: [String]
  }

  func targets(auth: AuthContext, selector: AppViewFeedSelector) async throws -> [Target] {
    let scopes: [Target]
    switch selector.kind {
    case .publication:
      if let row = await projectionService.sidebarRow(for: auth.did, publicationId: selector.id) {
        scopes = [Target(publicationId: row.publicationId, authorDid: row.appViewScope.authorDid,
          publicationAtUri: row.appViewScope.publicationAtUri,
          publicationScopeAtUris: row.appViewScope.publicationScopeAtUris,
          publicationSiteUrls: row.appViewScope.publicationSiteUrls)]
      } else {
        // Cold sidebar cache: the stored section projection carries the same scope.
        let keys = Set(PublicationProjectionLogic.publicationIdLookupKeys(for: selector.id) + [selector.id])
        var matched: [Target] = []
        for kind in [AppViewFeedKind.subscribed, .following] {
          let found = try await readService.sectionScopes(auth: auth, selector: AppViewFeedSelector(kind: kind))
            .filter { keys.contains($0.publicationId) }.map(Self.target)
          matched += found
          if !found.isEmpty { break }
        }
        scopes = matched
      }
    case .list:
      guard let listsService else { return [] }
      scopes = try await listsService.preparedFeed(input: selector.id, viewerDid: auth.did).scopes.map(Self.target)
    case .subscribed, .following, .folder:
      scopes = try await readService.sectionScopes(auth: auth, selector: selector).map(Self.target)
    }
    var seen = Set<String>()
    return Array(scopes.filter { seen.insert($0.publicationId).inserted }.prefix(Self.maximumPublications))
  }

  func writeStream(auth: AuthContext, selector: AppViewFeedSelector, writer: inout any ResponseBodyWriter) async throws {
    let targets = try await targets(auth: auth, selector: selector)
    let total = targets.count
    try await write(.started(.init(total: total)), writer: &writer)
    var completed = 0
    var indexed = 0
    var unavailable = 0
    try await withThrowingTaskGroup(of: (String, Result<Int, any Error>).self) { group in
      defer { group.cancelAll() }
      var pending = targets.makeIterator()
      for _ in 0..<Self.concurrency {
        guard let target = pending.next() else { break }
        group.addTask { (target.publicationId, await refresh(target)) }
      }
      while let (publicationId, result) = try await group.next() {
        if let target = pending.next() {
          group.addTask { (target.publicationId, await refresh(target)) }
        }
        completed += 1
        let count: Int
        let status: FeedRefreshStreamEvent.Publication.Status
        switch result {
        case .success(let value):
          count = value
          status = value > 0 ? .updated : .checked
        case .failure(let error):
          try Task.checkCancellation()
          count = 0
          status = .unavailable
          unavailable += 1
          logger.debug("Feed refresh source unavailable", metadata: [
            "publicationId": .string(publicationId), "error": .string(String(describing: error)),
          ])
        }
        indexed += count
        if count > 0 { try? await projectionService.invalidateFirstPages(viewerDid: auth.did) }
        try await write(.publication(.init(publicationId: publicationId, status: status, indexed: count,
          completed: completed, total: total)), writer: &writer)
      }
    }
    // Recount before `done` so the client's follow-up unread fetch sees every article that landed.
    try await projectionService.reconcileEnrollmentCounters(auth: auth)
    try await write(.done(.init(total: total, indexed: indexed, unavailable: unavailable)), writer: &writer)
    logger.info("Feed refresh streamed", metadata: [
      "viewer": .string(auth.did), "kind": .string(selector.kind.rawValue),
      "publications": .stringConvertible(total), "indexed": .stringConvertible(indexed),
      "unavailable": .stringConvertible(unavailable),
    ])
  }

  private func refresh(_ target: Target) async -> Result<Int, any Error> {
    do {
      if target.authorDid == PublicationLexicons.rssAuthorDid {
        var count = 0
        for feedUrl in target.publicationSiteUrls {
          count += try await skyreaderIngestionService.refreshProjectedFeed(feedUrl)
        }
        return .success(count)
      }
      guard ThinAppViewEnrollBackfill.isBackfillEligibleAuthorDid(target.authorDid) else { return .success(0) }
      return .success(try await enrollService.refreshPublicContent(authorDid: target.authorDid,
        publicationAtUri: target.publicationAtUri, publicationScopeAtUris: target.publicationScopeAtUris,
        publicationSiteUrls: target.publicationSiteUrls, waitForCapacity: true))
    } catch {
      return .failure(error)
    }
  }

  private func write(_ event: FeedRefreshStreamEvent, writer: inout any ResponseBodyWriter) async throws {
    var buffer = ByteBuffer()
    buffer.writeBytes(try event.encodedLine())
    try await writer.write(buffer)
  }

  private static func target(_ scope: AppViewPublicationScope) -> Target {
    Target(publicationId: scope.publicationId, authorDid: scope.authorDid, publicationAtUri: scope.publicationAtUri,
      publicationScopeAtUris: scope.publicationScopeAtUris, publicationSiteUrls: scope.publicationSiteUrls)
  }
}
