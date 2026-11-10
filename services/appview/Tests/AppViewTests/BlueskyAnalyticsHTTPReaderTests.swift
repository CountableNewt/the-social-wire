import AsyncHTTPClient
import Foundation
import Testing
@testable import AppView

@Suite struct BlueskyAnalyticsHTTPReaderTests {
  @Test func requestsPublicProfileAndPreservesZeroVersusUnknownCounts() async throws {
    try await withHTTP { http in
      let reader = BlueskyAnalyticsHTTPReader(http: http, transport: { url in
        #expect(url.scheme == "https")
        #expect(url.host == "public.api.bsky.app")
        #expect(url.path == "/xrpc/app.bsky.actor.getProfile")
        #expect(Self.query(url) == ["actor": "did:plc:creator"])
        return Data(#"{"did":"did:plc:creator","followersCount":0,"followsCount":-1}"#.utf8)
      })
      let profile = try await reader.profile(author: "did:plc:creator")
      #expect(profile.followers == 0)
      #expect(profile.following == nil)
      #expect(profile.posts == nil)
    }
  }

  @Test func requestsBoundedUnpinnedFeedAndDecodesRepliesRepostsAndCounters() async throws {
    try await withHTTP { http in
      let reader = BlueskyAnalyticsHTTPReader(http: http, transport: { url in
        #expect(url.host == "public.api.bsky.app")
        #expect(url.path == "/xrpc/app.bsky.feed.getAuthorFeed")
        #expect(Self.query(url) == ["actor": "did:plc:creator", "limit": "100", "filter": "posts_with_replies", "includePins": "false", "cursor": "cursor+/=?"])
        return Data("""
          {"feed":[
            {"post":{"uri":"at://did:plc:creator/app.bsky.feed.post/one","author":{"did":"did:plc:creator"},"record":{"text":"Reply","createdAt":"2026-11-08T12:00:00Z","reply":{"root":{},"parent":{}}},"likeCount":2,"replyCount":3,"repostCount":4,"quoteCount":5}},
            {"post":{"uri":"at://did:plc:other/app.bsky.feed.post/two","author":{"did":"did:plc:other"},"record":{"text":"Reposted","createdAt":"2026-11-08T12:00:00Z"}},"reason":{"$type":"app.bsky.feed.defs#reasonRepost"}}
          ],"cursor":"next"}
          """.utf8)
      })
      let feed = try await reader.feed(author: "did:plc:creator", cursor: "cursor+/=?")
      #expect(feed.cursor == "next")
      #expect(feed.feed.count == 2)
      #expect(feed.feed[0].post.record.reply != nil)
      #expect(feed.feed[0].post.likeCount == 2)
      #expect(feed.feed[0].post.replyCount == 3)
      #expect(feed.feed[0].post.repostCount == 4)
      #expect(feed.feed[0].post.quoteCount == 5)
      #expect(feed.feed[1].reason?.type == "app.bsky.feed.defs#reasonRepost")
      #expect(feed.feed[1].post.record.reply == nil)
      #expect(feed.feed[1].post.likeCount == nil)
    }
  }

  @Test func requestsDateScopedArticleURLSearchWithoutLosingURLQuery() async throws {
    try await withHTTP { http in
      let article = "https://publisher.example/read?a=1&b=two#section"
      let since = ISO8601DateFormatter().date(from: "2026-11-02T12:00:00Z")!
      let until = ISO8601DateFormatter().date(from: "2026-11-09T12:00:00Z")!
      let reader = BlueskyAnalyticsHTTPReader(http: http, transport: { url in
        #expect(url.host == "api.bsky.app")
        #expect(url.path == "/xrpc/app.bsky.feed.searchPosts")
        #expect(Self.query(url) == ["q": "*", "url": article, "sort": "latest", "since": "2026-11-02T12:00:00Z", "until": "2026-11-09T12:00:00Z", "limit": "25"])
        return Data("""
          {"posts":[{"uri":"at://did:plc:reader/app.bsky.feed.post/share","author":{"did":"did:plc:reader"},"record":{"text":"Read this","createdAt":"2026-11-08T12:00:00Z"},"likeCount":0,"replyCount":0,"repostCount":0,"quoteCount":0}]}
          """.utf8)
      })
      let posts = try await reader.shares(url: article, since: since, until: until)
      #expect(posts.count == 1)
      #expect(posts.first?.author.did == "did:plc:reader")
      #expect(posts.first?.likeCount == 0)
    }
  }

  @Test func rejectsMismatchedProfileIdentityMalformedFeedAndSearchPayloads() async throws {
    try await withHTTP { http in
      let foreign = BlueskyAnalyticsHTTPReader(http: http, transport: { _ in
        Data(#"{"did":"did:plc:other","followersCount":500}"#.utf8)
      })
      await #expect(throws: AnalyticsError.providerUnavailable) { try await foreign.profile(author: "did:plc:creator") }
      let malformed = BlueskyAnalyticsHTTPReader(http: http, transport: { _ in Data(#"{"feed":[{"post":{"uri":7}}]}"#.utf8) })
      await #expect(throws: (any Error).self) { try await malformed.feed(author: "did:plc:creator", cursor: nil) }
      let invalidJSON = BlueskyAnalyticsHTTPReader(http: http, transport: { _ in Data("invalid JSON".utf8) })
      await #expect(throws: (any Error).self) { try await invalidJSON.shares(url: "https://publisher.example/article", since: Date(), until: Date()) }
    }
  }

  private static func query(_ url: URL) -> [String: String] {
    Dictionary(uniqueKeysWithValues: (URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []).map { ($0.name, $0.value ?? "") })
  }

  private func withHTTP(_ operation: (HTTPClient) async throws -> Void) async throws {
    let http = HTTPClient(eventLoopGroupProvider: .singleton)
    do { try await operation(http) } catch { try? await http.shutdown(); throw error }
    try await http.shutdown()
  }
}
