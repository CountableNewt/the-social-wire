import Foundation
import GatewayCore
import Hummingbird
import HummingbirdTesting
import NIOFoundationCompat
import Testing
@testable import AppView

@Suite struct BlueskyAnalyticsRoutesTests {
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

  @Test func rejectsUnauthenticatedRequestsBeforeProviderReads() async throws {
    try await fixture { store in
      let router = Router(context: GatewayRequestContext.self)
      AnalyticsRoutes(store: store, provider: AnalyticsTestContent(), bluesky: .init(reader: Reader())).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        let response = try await client.execute(uri: "/v1/analytics/bluesky?days=30", method: .get)
        #expect(response.status == .unauthorized)
        #expect(response.headers[.cacheControl] == "no-store")
      }
    }
  }

  @Test func rejectsUnsupportedPeriodsAndKeepsActorBoundToAuthenticatedViewer() async throws {
    try await fixture { store in
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: "did:plc:creator"))
      AnalyticsRoutes(store: store, provider: AnalyticsTestContent(), bluesky: .init(reader: Reader())).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        for period in ["0", "1", "8", "91", "garbage"] {
          let response = try await client.execute(uri: "/v1/analytics/bluesky?days=\(period)", method: .get)
          #expect(response.status == .badRequest)
          #expect(response.headers[.cacheControl] == "no-store")
        }
        let response = try await client.execute(uri: "/v1/analytics/bluesky?days=7&author=did:plc:foreign", method: .get)
        #expect(response.status == .ok)
        #expect(response.headers[.cacheControl] == "no-store")
        let summary = try AnalyticsCoding.decoder().decode(BlueskyAnalyticsSummary.self, from: Data(buffer: response.body))
        #expect(summary.days == 7)
        #expect(summary.profile.followers == 12)
        #expect(summary.coverage.feedAvailable)
      }
    }
  }

  @Test func discoversOwnedArticleSeedsBeyondTheFirstInventoryPage() async throws {
    try await fixture { store in
      let now = Date()
      let author = "did:plc:creator"
      var items = (0..<100).map { index in
        AnalyticsItem(uri: "at://\(author)/app.bsky.feed.post/post\(index)", kind: "post", title: "Post", url: "", publishedAt: "2026-11-08T12:00:00Z", refreshedAt: now)
      }
      let article = AnalyticsItem(uri: "at://\(author)/site.standard.document/article", kind: "article", title: "Owned Article", url: "https://publisher.example/article", publishedAt: "2026-11-08T12:00:00Z", refreshedAt: now)
      items.append(article)
      try await store.save(author: author, items: items, complete: true, at: now)
      let foreign = AnalyticsItem(uri: "at://did:plc:foreign/site.standard.document/private", kind: "article", title: "Foreign Article", url: "https://foreign.example/article", publishedAt: "2026-11-08T12:00:00Z", refreshedAt: now)
      try await store.save(author: "did:plc:foreign", items: [foreign], complete: true, at: now)
      let router = Router(context: GatewayRequestContext.self)
      router.add(middleware: SocialDraftTestAuth(owner: author))
      AnalyticsRoutes(store: store, provider: AnalyticsTestContent(), bluesky: .init(reader: Reader())).register(on: router.group())
      try await Application(router: router).test(.router) { client in
        let response = try await client.execute(uri: "/v1/analytics/bluesky", method: .get)
        #expect(response.status == .ok)
        let summary = try AnalyticsCoding.decoder().decode(BlueskyAnalyticsSummary.self, from: Data(buffer: response.body))
        #expect(summary.days == 30)
        #expect(summary.coverage.articlesSearched == 1)
        #expect(summary.coverage.sharesAvailable)
        #expect(summary.shares.map(\.articleUri) == [article.uri])
        #expect(summary.shares.first?.articleUrl == article.url)
      }
    }
  }

  private func fixture(_ body: (SQLiteAnalyticsStore) async throws -> Void) async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".sqlite").path
    defer { for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) } }
    try await body(SQLiteAnalyticsStore(path: path, secret: String(repeating: "fixture-secret", count: 4)))
  }

  private struct Reader: BlueskyAnalyticsReading {
    func profile(author: String) async throws -> BlueskyAnalyticsProfile {
      #expect(author == "did:plc:creator")
      return .init(followers: 12, following: 5, posts: 100)
    }
    func feed(author: String, cursor: String?) async throws -> BlueskyAnalyticsFeed {
      #expect(author == "did:plc:creator")
      return .init(feed: [], cursor: nil)
    }
    func shares(url: String, since: Date, until: Date) async throws -> [BlueskyAnalyticsFeed.Post] {
      #expect(url == "https://publisher.example/article")
      return []
    }
  }
}
