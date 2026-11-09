import Foundation
import Testing
@testable import AppView

@Suite struct BlueskyAnalyticsProviderTests {
  private let author = "did:plc:creator"
  private var now: Date { ISO8601DateFormatter().date(from: "2026-11-09T12:00:00Z")! }

  @Test func filtersAuthoredPeriodDeduplicatesAndRanksCurrentEngagement() async throws {
    let low = post("low", likes: 1)
    let high = post("high", likes: 8)
    let reply = post("reply", likes: 3, reply: true)
    let foreign = post("foreign", did: "did:plc:other", likes: 100)
    let old = post("old", date: "2026-10-01T12:00:00Z")
    let future = post("future", date: "2026-11-10T12:00:00Z")
    let pinned = BlueskyAnalyticsFeed.Row(post: old, reason: .init(type: "app.bsky.feed.defs#reasonPin"))
    let repost = BlueskyAnalyticsFeed.Row(post: high, reason: .init(type: "app.bsky.feed.defs#reasonRepost"))
    let reader = Reader(pages: ["": .init(feed: [pinned, repost] + [low, high, reply, high, foreign, old, future].map(row), cursor: nil)])
    let summary = await BlueskyAnalyticsProvider(reader: reader).summary(author: author, days: 7, articles: [], at: now)
    let activity = try #require(summary.activity)
    #expect(activity.posts == 2)
    #expect(activity.replies == 1)
    #expect(activity.postsPerDay == 2.0 / 7.0)
    #expect(summary.topPosts.map(\.uri) == [high.uri, reply.uri, low.uri])
    #expect(summary.topPosts.first?.engagement?.likes == 8)
    #expect(summary.coverage.feedComplete)
    #expect(summary.coverage.feedAvailable)
    #expect(summary.coverage.feedPostsExamined == 9)
  }

  @Test func distinguishesZeroUnknownAndNegativePostCounters() async throws {
    let zero = post("zero", likes: 0)
    var missing = post("missing"); missing.replyCount = nil
    let negative = post("negative", likes: -1)
    let reader = Reader(pages: ["": .init(feed: [missing, negative, zero].map(row), cursor: nil)])
    let summary = await BlueskyAnalyticsProvider(reader: reader).summary(author: author, days: 30, articles: [], at: now)
    #expect(summary.topPosts.first?.uri == zero.uri)
    #expect(summary.topPosts.first?.engagement?.likes == 0)
    #expect(summary.topPosts.filter { $0.engagement == nil }.count == 2)
    let data = try AnalyticsCoding.encoder().encode(summary)
    let object = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
    let encodedPosts = try #require(object["topPosts"] as? [[String: Any]])
    #expect(encodedPosts.first?["engagement"] is [String: Any])
    #expect(encodedPosts.last?["engagement"] is NSNull)
  }

  @Test func reportsUnavailableProvidersWithoutFabricatingZeros() async throws {
    let reader = Reader(failProfile: true, failedCursors: [""])
    let summary = await BlueskyAnalyticsProvider(reader: reader).summary(author: author, days: 30, articles: [], at: now)
    #expect(summary.profile.followers == nil)
    #expect(summary.profile.following == nil)
    #expect(summary.profile.posts == nil)
    #expect(summary.activity == nil)
    #expect(!summary.coverage.feedAvailable)
    #expect(!summary.coverage.feedComplete)
    #expect(summary.topPosts.isEmpty)
    let object = try #require(JSONSerialization.jsonObject(with: AnalyticsCoding.encoder().encode(summary)) as? [String: Any])
    #expect(object["activity"] is NSNull)
    let profile = try #require(object["profile"] as? [String: Any])
    #expect(profile["followers"] is NSNull)
  }

  @Test func keepsPartialFeedAndStopsRepeatedCursors() async throws {
    let one = post("one")
    let two = post("two")
    let reader = Reader(pages: ["": .init(feed: [row(one)], cursor: "repeat"), "repeat": .init(feed: [row(two)], cursor: "repeat")])
    let summary = await BlueskyAnalyticsProvider(reader: reader).summary(author: author, days: 30, articles: [], at: now)
    #expect(summary.activity?.posts == 2)
    #expect(summary.coverage.feedAvailable)
    #expect(!summary.coverage.feedComplete)
    #expect(summary.coverage.feedPostsExamined == 2)
    let partialReader = Reader(pages: ["": .init(feed: [row(one)], cursor: "failed")], failedCursors: ["failed"])
    let partial = await BlueskyAnalyticsProvider(reader: partialReader).summary(author: author, days: 30, articles: [], at: now)
    #expect(partial.activity?.posts == 1)
    #expect(partial.coverage.feedAvailable)
    #expect(!partial.coverage.feedComplete)
  }

  @Test func boundsFeedAndTopPosts() async throws {
    let posts = (0..<12).map { post("post\($0)", likes: $0) }
    let reader = Reader(pages: ["": .init(feed: posts.map(row), cursor: "second"), "second": .init(feed: [], cursor: "third"), "third": .init(feed: [], cursor: "fourth")])
    let summary = await BlueskyAnalyticsProvider(reader: reader).summary(author: author, days: 90, articles: [], at: now)
    #expect(summary.topPosts.count == 10)
    #expect(summary.topPosts.first?.engagement?.likes == 11)
    #expect(!summary.coverage.feedComplete)
  }

  @Test func scopesSearchToEligibleArticlesAndDeduplicatesDiscoveredPosts() async throws {
    let shared = post("shared", did: "did:plc:reader", likes: 7)
    var mismatched = shared; mismatched.author.did = "did:plc:other"
    let old = post("oldshare", date: "2026-10-01T12:00:00Z")
    let future = post("futureshare", date: "2026-11-10T12:00:00Z")
    let article = item("article", url: "https://publisher.example/article")
    var unsafe = item("unsafe", url: "https://user:pass@publisher.example/private")
    unsafe.publishedAt = "2026-11-09T11:00:00Z"
    var notArticle = article; notArticle.kind = "post"
    let reader = Reader(searchPosts: [article.url: [shared, shared, mismatched, old, future]])
    let summary = await BlueskyAnalyticsProvider(reader: reader).summary(author: author, days: 7, articles: [article, article, unsafe, notArticle], at: now)
    #expect(summary.coverage.articlesSearched == 1)
    #expect(summary.coverage.sharesAvailable)
    #expect(!summary.coverage.sharesComplete)
    #expect(summary.shares.count == 1)
    #expect(summary.shares.first?.posts.map(\.uri) == [shared.uri])
    #expect(summary.shares.first?.posts.first?.engagement?.likes == 7)
    #expect(summary.shares.first?.articleUrl == article.url)
  }

  @Test func boundsArticleSearchAndReportsFailedSearches() async throws {
    let articles = (0..<8).map { item("article\($0)", url: "https://publisher.example/\($0)") }
    let failed = Reader(failedURLs: [articles[0].url])
    let summary = await BlueskyAnalyticsProvider(reader: failed).summary(author: author, days: 30, articles: articles, at: now)
    #expect(summary.coverage.articlesSearched == 5)
    #expect(!summary.coverage.sharesAvailable)
    #expect(summary.shares.count == 4)
    #expect(!summary.coverage.sharesComplete)
  }

  private func post(_ key: String, did: String = "did:plc:creator", date: String = "2026-11-08T12:00:00Z", likes: Int = 0, reply: Bool = false) -> BlueskyAnalyticsFeed.Post {
    .init(uri: "at://\(did)/app.bsky.feed.post/\(key)", author: .init(did: did), record: .init(text: key, createdAt: date, reply: reply ? .init() : nil), likeCount: likes, replyCount: 0, repostCount: 0, quoteCount: 0)
  }

  private func row(_ post: BlueskyAnalyticsFeed.Post) -> BlueskyAnalyticsFeed.Row { .init(post: post, reason: nil) }

  private func item(_ key: String, url: String) -> AnalyticsItem {
    .init(uri: "at://\(author)/site.standard.document/\(key)", kind: "article", title: key, url: url, publishedAt: "2026-11-08T12:00:00Z", refreshedAt: now)
  }

  private struct Reader: BlueskyAnalyticsReading {
    var pages: [String: BlueskyAnalyticsFeed] = [:]
    var searchPosts: [String: [BlueskyAnalyticsFeed.Post]] = [:]
    var failProfile = false
    var failedCursors: Set<String> = []
    var failedURLs: Set<String> = []

    func profile(author: String) async throws -> BlueskyAnalyticsProfile {
      if failProfile { throw AnalyticsError.providerUnavailable }
      return .init(followers: 10, following: 5, posts: 20)
    }
    func feed(author: String, cursor: String?) async throws -> BlueskyAnalyticsFeed {
      if failedCursors.contains(cursor ?? "") { throw AnalyticsError.providerUnavailable }
      return pages[cursor ?? ""] ?? .init(feed: [], cursor: nil)
    }
    func shares(url: String, since: Date, until: Date) async throws -> [BlueskyAnalyticsFeed.Post] {
      if failedURLs.contains(url) { throw AnalyticsError.providerUnavailable }
      return searchPosts[url] ?? []
    }
  }
}
