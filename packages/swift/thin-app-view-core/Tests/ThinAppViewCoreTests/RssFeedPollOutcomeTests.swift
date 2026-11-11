import AsyncHTTPClient
import Foundation
import GRDB
import Logging
import Testing
@testable import ThinAppViewCore

@Suite("RSS poll outcomes")
struct RssFeedPollOutcomeTests {
  private let feed = "https://publisher.example/rss?token=private"

  @Test("SQLite coalesces overlapping RSS leases and protects successor ownership")
  func sqliteRSSLeaseOwnership() async throws {
    let fixture = try RSSPollingFixture()
    defer { fixture.cleanup() }
    let cache: any AppViewProjectionCacheStore = fixture.cache
    async let one = cache.acquireRefreshLease(domain: "rss", resource: feed, ttl: 120)
    async let two = cache.acquireRefreshLease(domain: "rss", resource: feed, ttl: 120)
    let holders = await [one, two].compactMap { $0 }
    #expect(holders.count == 1)
    let owner = try #require(holders.first)
    #expect(!owner.key.contains("private"))
    #expect(await cache.renewRefreshLease(owner))
    #expect(await cache.acquireRefreshLease(domain: "rss", resource: "another", ttl: 120) != nil)
    #expect(await cache.acquireRefreshLease(domain: "sidebar", resource: feed, ttl: 120) != nil)
    #expect(await cache.acquireRefreshLease(domain: "sidebar", resource: feed, ttl: 120) != nil)
    await cache.releaseRefreshLease(owner)
    let successor = try #require(await cache.acquireRefreshLease(domain: "rss", resource: feed, ttl: 120))
    await cache.releaseRefreshLease(owner)
    #expect(await cache.acquireRefreshLease(domain: "rss", resource: feed, ttl: 120) == nil)
    #expect(!(await cache.renewRefreshLease(owner)))
    #expect(await cache.renewRefreshLease(successor))
    await cache.releaseRefreshLease(successor)
  }

  @Test("explicit poll distinguishes blocked, busy, backoff, and persisted no-change")
  func outcomes() async throws {
    let fixture = try RSSPollingFixture()
    defer { fixture.cleanup() }
    let ingester = fixture.ingester { _ in HTTPClientResponse(status: .notModified) }
    let started = Date()
    let blocked = try await ingester.refreshFeed(normalizedFeedUrl: "https://localhost/rss", started: started)
    guard case .unfetchable = blocked else { Issue.record("expected blocked feed"); return }
    let lease = try #require(await fixture.cache.acquireRefreshLease(domain: "rss", resource: feed, ttl: 120))
    let busy = try await ingester.refreshFeed(normalizedFeedUrl: feed, started: started)
    guard case .leaseBusy = busy else { Issue.record("expected held lease"); return }
    await fixture.cache.releaseRefreshLease(lease)
    let completed = try await ingester.refreshFeed(normalizedFeedUrl: feed, started: started)
    guard case .completed(let indexed, let metadata) = completed else {
      Issue.record("expected persisted completion"); return
    }
    #expect(indexed == 0)
    #expect(metadata.lastPollAt ?? .distantPast >= started)
    #expect(try await fixture.store.fetchRssFeedMetadata(normalizedFeedUrl: feed)?.lastPollAt != nil)
    let previousFailure = RssFeedFetchMetadata(normalizedFeedUrl: feed, etag: nil, lastModified: nil,
      lastPollAt: started.addingTimeInterval(-1), backoffUntil: started.addingTimeInterval(300), consecutiveErrorCount: 1)
    try await fixture.store.storeRssFeedMetadata(previousFailure)
    let backoff = try await ingester.refreshFeed(normalizedFeedUrl: feed, started: started)
    guard case .backedOff(let failure) = backoff else { Issue.record("expected active backoff"); return }
    #expect(failure.consecutiveErrorCount == 1)
  }

  @Test("explicit metadata failure throws while background polling keeps its zero result")
  func metadataPersistenceFailure() async throws {
    let fixture = try RSSPollingFixture()
    defer { fixture.cleanup() }
    try await fixture.store.db.write { db in
      try db.execute(sql: """
        CREATE TRIGGER reject_rss_metadata BEFORE INSERT ON rss_feed_fetch_metadata
        BEGIN SELECT RAISE(ABORT, 'fixture metadata write failure'); END;
        """)
    }
    let ingester = fixture.ingester { _ in HTTPClientResponse(status: .notModified) }
    await #expect(throws: DatabaseError.self) {
      try await ingester.refreshFeed(normalizedFeedUrl: feed, started: Date())
    }
    #expect(try await ingester.ingestFeed(normalizedFeedUrl: feed) == 0)
    #expect(try await fixture.store.fetchRssFeedMetadata(normalizedFeedUrl: feed) == nil)
    #expect(await fixture.cache.acquireRefreshLease(domain: "rss", resource: feed, ttl: 120) != nil)
  }

  @Test("canceled fetches release the lease and leave backoff untouched")
  func canceledPoll() async throws {
    let fixture = try RSSPollingFixture()
    defer { fixture.cleanup() }
    let ingester = fixture.ingester { _ in throw CancellationError() }
    await #expect(throws: CancellationError.self) {
      try await ingester.refreshFeed(normalizedFeedUrl: feed, started: Date())
    }
    #expect(try await fixture.store.fetchRssFeedMetadata(normalizedFeedUrl: feed) == nil)
    #expect(await fixture.cache.acquireRefreshLease(domain: "rss", resource: feed, ttl: 120) != nil)
  }
}

private struct RSSPollingFixture {
  let path: String
  let store: SQLiteThinAppViewStore
  let cache: SQLiteAppViewProjectionCacheStore
  private let logger = Logger(label: "rss.poll.outcome.test")

  init() throws {
    path = NSTemporaryDirectory() + "rss-poll-\(UUID()).sqlite"
    store = try SQLiteThinAppViewStore(path: path, logger: logger)
    cache = try SQLiteAppViewProjectionCacheStore(path: path, logger: logger)
  }

  func ingester(
    fetch: @escaping @Sendable (HTTPClientRequest) async throws -> HTTPClientResponse
  ) -> ThinAppViewRssIngestion {
    ThinAppViewRssIngestion(store: store, config: .fromEnvironment([:]), logger: logger,
      projectionCache: cache, fetch: fetch)
  }

  func cleanup() {
    for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) }
  }
}
