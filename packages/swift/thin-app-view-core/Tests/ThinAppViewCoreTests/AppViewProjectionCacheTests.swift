import Foundation
import GRDB
import Logging
import Testing
@testable import ThinAppViewCore

struct AppViewProjectionCacheTests {
  @Test("SQLite cache invalidation waits for another store's writer transaction")
  func sqliteInvalidationWaitsForWriter() async throws {
    let directory = URL(fileURLWithPath: NSTemporaryDirectory())
      .appendingPathComponent("sw-projection-contention-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    let path = directory.appendingPathComponent("appview.sqlite").path
    let store = try SQLiteAppViewProjectionCacheStore(
      path: path, logger: Logger(label: "projection-cache.contention.test")
    )
    let viewer = "did:plc:viewer"
    try await store.storeUnreadCounts(
      viewerDid: viewer, counts: ["pub": 3], expiresAt: Date().addingTimeInterval(3600)
    )
    var configuration = Configuration()
    configuration.allowsUnsafeTransactions = true
    let competingWriter = try DatabaseQueue(path: path, configuration: configuration)
    // Acquire the external lock before invalidation starts, matching the separate
    // connections used by ingestion, private drafts, and projection caches.
    try await competingWriter.writeWithoutTransaction { database in
      try database.execute(sql: "BEGIN IMMEDIATE TRANSACTION")
    }
    defer {
      try? competingWriter.writeWithoutTransaction { database in
        try database.execute(sql: "ROLLBACK")
      }
    }
    let release = Task {
      try await Task.sleep(for: .milliseconds(200))
      try await competingWriter.writeWithoutTransaction { database in
        try database.execute(sql: "COMMIT")
      }
    }
    do {
      try await store.invalidateUnreadCounts(viewerDid: viewer, publicationId: "pub")
      try await release.value
    } catch {
      // Always release the lock before cleaning up the temporary database.
      try await release.value
      throw error
    }
    #expect(try await store.cachedUnreadCounts(viewerDid: viewer) == nil)
  }

  @Test("SQLite projection cache round-trips sidebar unread and first page")
  func sqliteRoundTrip() async throws {
    let path = NSTemporaryDirectory() + "sw-projection-cache-\(UUID().uuidString).sqlite"
    defer { try? FileManager.default.removeItem(atPath: path) }

    let store = try SQLiteAppViewProjectionCacheStore(
      path: path,
      logger: Logger(label: "projection-cache.test")
    )
    let viewerDid = "did:plc:viewer"
    let publicationId = "pub-a"
    let expires = Date().addingTimeInterval(3600)

    try await store.storeSidebarProjectionJSON(
      viewerDid: viewerDid,
      jsonBody: #"{"priority":{"viewerDid":"did:plc:viewer"}}"#,
      expiresAt: expires
    )
    try await store.storeUnreadCounts(
      viewerDid: viewerDid,
      counts: [publicationId: 3, "pub-zero": 0],
      expiresAt: expires
    )
    try await store.storeFirstPageJSON(
      viewerDid: viewerDid,
      publicationId: publicationId,
      jsonBody: #"{"entries":[]}"#,
      expiresAt: expires
    )

    #expect(try await store.cachedSidebarProjectionJSON(viewerDid: viewerDid)?.contains("viewerDid") == true)
    #expect(try await store.cachedUnreadCounts(viewerDid: viewerDid)?[publicationId] == 3)
    #expect(try await store.cachedUnreadCounts(viewerDid: viewerDid)?["pub-zero"] == 0)
    #expect(try await store.cachedFirstPageJSON(viewerDid: viewerDid, publicationId: publicationId) == #"{"entries":[]}"#)
    let sidebarEvidence = try await store.sidebarProjectionCacheEntry(viewerDid: viewerDid)
    #expect(sidebarEvidence?.source == .projectionCache)
    #expect(sidebarEvidence?.cachedAt ?? .distantFuture < sidebarEvidence?.expiresAt ?? .distantPast)

    try await store.invalidateFirstPageForAllViewers(publicationId: publicationId)
    #expect(try await store.cachedFirstPageJSON(viewerDid: viewerDid, publicationId: publicationId) == nil)

    try await store.invalidateUnreadCounts(viewerDid: viewerDid, publicationId: publicationId)
    #expect(try await store.cachedUnreadCounts(viewerDid: viewerDid)?["pub-zero"] == 0)

    try await store.invalidateUnreadCounts(viewerDid: viewerDid, publicationId: nil)
    #expect(try await store.cachedUnreadCounts(viewerDid: viewerDid) == nil)

    let deleted = try await store.deleteExpiredProjectionCaches(
      before: Date().addingTimeInterval(7200),
      batchSize: 1_000
    )
    #expect(deleted >= 1)
  }

  @Test("publication site keys preserve RSS feed query")
  func publicationSiteKeysPreserveRssFeedQuery() {
    let feedUrl = "https://basicappleguy.com/basicappleblog?format=rss"
    let keys = AppViewProjectionCacheScopeKeys.publicationSiteKeys(
      publicationAtUri: nil,
      publicationScopeAtUris: [],
      publicationSiteUrls: [feedUrl]
    )

    #expect(keys.contains(feedUrl))
    #expect(keys.contains("https://basicappleguy.com/basicappleblog"))
  }
}
