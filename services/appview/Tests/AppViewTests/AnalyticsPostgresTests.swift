import Crypto
import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import Logging
import NIOCore
import PostgresNIO
import Testing
import ThinAppViewCore
@testable import AppView

@Suite(.serialized, .enabled(if: ProcessInfo.processInfo.environment["ANALYTICS_TEST_DATABASE_URL"] != nil))
struct AnalyticsPostgresTests {
  @Test func articleInventoryIsOwnedFilteredAndNewestFirst() async throws {
    try await fixture { store in
      let now = Date()
      let author = "did:plc:article-inventory-" + UUID().uuidString.lowercased()
      let old = AnalyticsItem(uri: "at://\(author)/site.standard.document/old", kind: "article", title: "Old", url: "https://publisher.example/old", publishedAt: "2026-10-01T12:00:00Z", refreshedAt: now)
      let recent = AnalyticsItem(uri: "at://\(author)/site.standard.document/recent", kind: "article", title: "Recent", url: "https://publisher.example/recent", publishedAt: "2026-11-01T12:00:00Z", refreshedAt: now)
      let post = AnalyticsItem(uri: "at://\(author)/app.bsky.feed.post/post", kind: "post", title: "Post", url: "", publishedAt: "2026-11-09T12:00:00Z", refreshedAt: now)
      try await store.save(author: author, items: [old, post, recent], complete: true, at: now)
      let rows = try await store.articles(author: author)
      #expect(rows.map(\.uri) == [recent.uri, old.uri])
      #expect(try await store.articles(author: "did:plc:foreign").isEmpty)
    }
  }

  @Test func durableObservationBudgetAndDailyPseudonymRotation() async throws {
    try await fixture { store in
      let author = "did:plc:budget-" + UUID().uuidString.lowercased()
      let viewer = "did:plc:observer-" + UUID().uuidString.lowercased()
      let uri = "at://\(author)/app.bsky.feed.post/one"
      let now = Date()
      let event = AnalyticsEvent(subjectUri: uri, event: "impression", surface: "social-feed")
      try await store.record(viewer: viewer, author: author, event: event, at: now)
      let rows = try await store.pool.query("SELECT viewer_key FROM creator_analytics_events WHERE author_did=\(author)", logger: store.logger)
      var key: String?
      for try await row in rows { key = try row.decode(String.self) }
      let viewerKey = try #require(key)
      try await store.pool.query("""
        INSERT INTO creator_analytics_events(author_did,subject_uri,day,viewer_key,event,surface)
        SELECT \(author),'budget-'||i::text,\(AnalyticsCoding.day(now))::date,\(viewerKey),'view','social-feed'
        FROM generate_series(1,999) i
        """, logger: store.logger)
      await #expect(throws: AnalyticsError.rateLimit) { try await store.record(viewer: viewer, author: author, event: event, at: now) }
      try await store.record(viewer: viewer, author: author, event: event, at: now.addingTimeInterval(86400))
      let rotated = try await store.pool.query("SELECT count(DISTINCT viewer_key) FROM creator_analytics_events WHERE author_did=\(author)", logger: store.logger)
      for try await row in rotated { #expect(try row.decode(Int.self) == 2) }
    }
  }

  @Test func retentionDeletesAtMostOneThousandExpiredRowsPerTable() async throws {
    try await fixture { store in
      let author = "did:plc:retention-" + UUID().uuidString.lowercased()
      let now = Date()
      let old = AnalyticsCoding.day(now.addingTimeInterval(-120 * 86400))
      try await store.pool.query("""
        INSERT INTO creator_analytics_events(author_did,subject_uri,day,viewer_key,event,surface)
        SELECT \(author),\(author)||i::text,\(old)::date,'expired-fixture','view','social-feed' FROM generate_series(1,1100) i
        """, logger: store.logger)
      let cutoff = AnalyticsCoding.day(now.addingTimeInterval(-30 * 86400))
      var before = 0
      for try await row in try await store.pool.query("SELECT count(*) FROM creator_analytics_events WHERE day<\(cutoff)::date", logger: store.logger) { before = try row.decode(Int.self) }
      try await store.cleanup(at: now)
      var after = 0
      for try await row in try await store.pool.query("SELECT count(*) FROM creator_analytics_events WHERE day<\(cutoff)::date", logger: store.logger) { after = try row.decode(Int.self) }
      #expect(before - after == 1000)
    }
  }

  @Test func deduplicatesObservationsPreservesNullsAndKeepsRefreshMonotonic() async throws {
    try await fixture { store in
      let author = "did:plc:analytics-" + UUID().uuidString.lowercased()
      let viewer = "did:plc:viewer-" + UUID().uuidString.lowercased()
      let post = "at://\(author)/app.bsky.feed.post/one"
      let article = "at://\(author)/site.standard.document/one"
      let now = Date()
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
      try await store.save(author: author, items: [], complete: true, at: now.addingTimeInterval(-1))
      let summary = try await store.summary(author: author, days: 30, limit: 1, cursor: "", at: now)
      #expect(summary.items.count == 1 && summary.cursor == post)
      #expect(summary.coverage.totalItems == 2 && summary.coverage.engagementKnownItems == 1)
      #expect(summary.totals.engagement?.likes == 7)
      #expect(summary.totals.tracking.impressions == 1 && summary.totals.tracking.outboundClicks == 1 && summary.totals.tracking.views == 0)
      #expect(summary.totals.tracking.ctr == 100)
      let second = try await store.summary(author: author, days: 30, limit: 10, cursor: summary.cursor ?? "", at: now)
      #expect(second.items.first?.uri == article && second.items.first?.engagement == nil)
      let observations = try await store.pool.query("SELECT viewer_key FROM creator_analytics_events WHERE author_did=\(author)", logger: store.logger)
      for try await row in observations { let key = try row.decode(String.self); #expect(key.count == 64 && !key.contains(viewer)) }
      try await store.save(author: author, items: [], complete: false, at: now.addingTimeInterval(1))
      #expect(try await store.summary(author: author, days: 30, limit: 10, cursor: "", at: now).coverage.totalItems == 2)
      try await store.save(author: author, items: [], complete: true, at: now.addingTimeInterval(2))
      #expect(try await store.summary(author: author, days: 30, limit: 10, cursor: "", at: now).items.isEmpty)
    }
  }

  @Test func routesEnforceOwnershipCanonicalContentRangesAndRefreshLimits() async throws {
    try await fixture { store in
      let author = "did:plc:analytics-" + UUID().uuidString.lowercased()
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: author))
      AnalyticsRoutes(store: store, provider: AnalyticsTestContent(valid: false)).register(on: router.group())
      let app = Application(router: router, configuration: .init(address: .hostname("127.0.0.1", port: 0)))
      try await app.test(.live) { client in
        #expect(try await client.execute(uri: "/v1/analytics/summary?days=0", method: .get).status.code == 400)
        #expect(try await client.execute(uri: "/v1/analytics/summary?cursor=at://did:plc:foreign/app.bsky.feed.post/one", method: .get).status.code == 400)
        let refresh = try await client.execute(uri: "/v1/analytics/refresh", method: .post)
        #expect(refresh.status.code == 200 && refresh.headers[.cacheControl] == "no-store")
        #expect(try await client.execute(uri: "/v1/analytics/refresh", method: .post).status.code == 429)
        let event = AnalyticsEvent(subjectUri: "at://did:plc:foreign/app.bsky.feed.post/one", event: "view", surface: "social-feed")
        #expect(try await client.execute(uri: "/v1/analytics/events", method: .post, body: ByteBuffer(bytes: JSONEncoder().encode(event))).status.code == 422)
        let selfEvent = AnalyticsEvent(subjectUri: "at://\(author)/app.bsky.feed.post/one", event: "view", surface: "social-feed")
        #expect(try await client.execute(uri: "/v1/analytics/events", method: .post, body: ByteBuffer(bytes: JSONEncoder().encode(selfEvent))).status.code == 202)
      }
    }
  }

  private func fixture(_ body: @Sendable (AnalyticsStore) async throws -> Void) async throws {
    let url = try #require(ProcessInfo.processInfo.environment["ANALYTICS_TEST_DATABASE_URL"])
    let logger = Logger(label: "analytics.test")
    let pool = PostgresClient(configuration: try makePostgresConfig(from: url, logger: logger), backgroundLogger: logger)
    let run = Task { await pool.run() }
    let store = AnalyticsStore(pool: pool, secret: String(repeating: "fixture-secret", count: 4), logger: logger)
    do { try await body(store) }
    catch { run.cancel(); await run.value; throw error }
    run.cancel(); await run.value
  }
}
