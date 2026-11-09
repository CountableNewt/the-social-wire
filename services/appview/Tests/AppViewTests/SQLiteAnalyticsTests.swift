import Foundation
@preconcurrency import GRDB
import Testing
@testable import AppView

struct SQLiteAnalyticsTests {
  @Test func observationsAreDeduplicatedAndRefreshIsMonotonicAcrossReopen() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".sqlite").path
    defer { for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) } }
    let secret = String(repeating: "fixture-secret", count: 4)
    let store = try SQLiteAnalyticsStore(path: path, secret: secret)
    let author = "did:plc:author", viewer = "did:plc:viewer"
    let post = "at://\(author)/app.bsky.feed.post/one"
    let article = "at://\(author)/site.standard.document/one"
    let now = Date(timeIntervalSince1970: 1_790_000_000)
    let items = [
      AnalyticsItem(uri: post, kind: "post", title: "Post", url: "", publishedAt: "", engagement: .init(likes: 7), refreshedAt: now),
      AnalyticsItem(uri: article, kind: "article", title: "Article", url: "", publishedAt: "", refreshedAt: now),
    ]
    try await store.save(author: author, items: items, complete: true, at: now)
    try await store.record(viewer: viewer, author: author, event: .init(subjectUri: post, event: "outbound_click", surface: "social-feed"), at: now)
    for _ in 0..<2 {
      try await store.record(viewer: viewer, author: author, event: .init(subjectUri: post, event: "impression", surface: "social-feed"), at: now)
      try await store.record(viewer: viewer, author: author, event: .init(subjectUri: post, event: "outbound_click", surface: "social-feed"), at: now)
    }
    try await store.record(viewer: author, author: author, event: .init(subjectUri: post, event: "view", surface: "social-feed"), at: now)
    let reopened = try SQLiteAnalyticsStore(path: path, secret: secret)
    try await reopened.save(author: author, items: [], complete: true, at: now.addingTimeInterval(-1))
    let summary = try await reopened.summary(author: author, days: 30, limit: 1, cursor: "", at: now)
    #expect(summary.items.count == 1 && summary.cursor == post)
    #expect(summary.coverage.totalItems == 2 && summary.coverage.engagementKnownItems == 1)
    #expect(summary.totals.engagement?.likes == 7)
    #expect(summary.totals.tracking.impressions == 1 && summary.totals.tracking.outboundClicks == 1)
    #expect(summary.totals.tracking.views == 0 && summary.totals.tracking.ctr == 100)
    #expect(summary.series.first?.likes == 7 && summary.series.first?.impressions == 1)
    let second = try await reopened.summary(author: author, days: 30, limit: 10, cursor: post, at: now)
    #expect(second.items.first?.uri == article && second.items.first?.engagement == nil)
    try await reopened.save(author: author, items: [], complete: false, at: now.addingTimeInterval(1))
    #expect(try await reopened.summary(author: author, days: 30, limit: 10, cursor: "", at: now).coverage.totalItems == 2)
    try await reopened.save(author: author, items: [], complete: true, at: now.addingTimeInterval(2))
    #expect(try await reopened.summary(author: author, days: 30, limit: 10, cursor: "", at: now).items.isEmpty)
  }

  @Test func observationBudgetIsDurableAndPseudonymsRotateDaily() async throws {
    try await fixture { store in
      let author = "did:plc:author", viewer = "did:plc:viewer"
      let now = Date(timeIntervalSince1970: 1_790_000_000)
      let day = AnalyticsCoding.day(now)
      let event = AnalyticsEvent(subjectUri: "at://\(author)/app.bsky.feed.post/one", event: "impression", surface: "social-feed")
      try await store.record(viewer: viewer, author: author, event: event, at: now)
      let loaded = try await store.db.read { database in try String.fetchOne(database, sql: "SELECT viewer_key FROM creator_analytics_events") }
      let key = try #require(loaded)
      #expect(key.count == 64 && !key.contains(viewer))
      try await store.db.write { database in
        try database.execute(sql: """
          WITH RECURSIVE numbers(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM numbers WHERE i<999)
          INSERT INTO creator_analytics_events(author_did,subject_uri,day,viewer_key,event,surface)
          SELECT ?,'budget-'||i,?,?,'view','social-feed' FROM numbers
          """, arguments: [author, day, key])
      }
      await #expect(throws: AnalyticsError.rateLimit) { try await store.record(viewer: viewer, author: author, event: event, at: now) }
      try await store.record(viewer: viewer, author: author, event: event, at: now.addingTimeInterval(86400))
      let keys = try await store.db.read { database in try Int.fetchOne(database, sql: "SELECT count(DISTINCT viewer_key) FROM creator_analytics_events") }
      #expect(keys == 2)
    }
  }

  @Test func concurrentObservationsIncrementOnceAndRespectOwnerIsolation() async throws {
    try await fixture { store in
      let now = Date(timeIntervalSince1970: 1_790_000_000)
      let event = AnalyticsEvent(subjectUri: "at://did:plc:author/app.bsky.feed.post/one", event: "view", surface: "social-feed")
      try await withThrowingTaskGroup(of: Void.self) { group in
        for _ in 0..<20 {
          group.addTask { try await store.record(viewer: "did:plc:viewer", author: "did:plc:author", event: event, at: now) }
        }
        try await group.waitForAll()
      }
      #expect(try await store.summary(author: "did:plc:author", days: 30, limit: 10, cursor: "", at: now).totals.tracking.views == 1)
      #expect(try await store.summary(author: "did:plc:other", days: 30, limit: 10, cursor: "", at: now).totals.tracking.views == 0)
    }
  }

  @Test func retentionIsBoundedAndInvalidRangesFail() async throws {
    try await fixture { store in
      let now = Date(timeIntervalSince1970: 1_790_000_000)
      try await store.db.write { database in
        try database.execute(sql: """
          WITH RECURSIVE numbers(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM numbers WHERE i<1100)
          INSERT INTO creator_analytics_events(author_did,subject_uri,day,viewer_key,event,surface)
          SELECT 'author','expired-'||i,'2000-01-01','viewer','view','social-feed' FROM numbers
          """)
      }
      try await store.cleanup(at: now)
      #expect(try await store.db.read { try Int.fetchOne($0, sql: "SELECT count(*) FROM creator_analytics_events") } == 100)
      await #expect(throws: AnalyticsError.invalid) { try await store.summary(author: "author", days: 0, limit: 10, cursor: "", at: now) }
      await #expect(throws: AnalyticsError.invalid) { try await store.summary(author: "author", days: 30, limit: 101, cursor: "", at: now) }
    }
  }

  private func fixture(_ body: @Sendable (SQLiteAnalyticsStore) async throws -> Void) async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".sqlite").path
    defer { for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) } }
    try await body(SQLiteAnalyticsStore(path: path, secret: String(repeating: "fixture-secret", count: 4)))
  }
}
