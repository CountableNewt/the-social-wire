import Foundation
import GatewayCore
import Testing
@testable import AppView

@Suite struct AnalyticsProviderTests {
  @Test func preservesOwnedInventoryUnknownEngagementAndPublicationURLs() async throws {
    let author = "did:plc:creator"
    let post = "at://\(author)/app.bsky.feed.post/one"
    let article = "at://\(author)/site.standard.document/one"
    let site = "at://\(author)/site.standard.publication/main"
    var reader = AnalyticsTestReader()
    reader.collections = [
      "app.bsky.feed.post": [.init(uri: post, value: .init(values: ["$type": "app.bsky.feed.post", "text": "Hello", "createdAt": "2026-10-11T12:00:00Z"]))],
      "site.standard.document": [.init(uri: article, value: .init(values: ["$type": "site.standard.document", "title": "Article", "site": site, "path": "/a/read"]))],
    ]
    reader.recordsByURI[site] = .init(uri: site, value: .init(values: ["$type": "site.standard.publication", "url": "https://publisher.example/publication/"]))
    reader.posts = try JSONDecoder().decode(AnalyticsPosts.self, from: Data("""
      {"posts":[{"uri":"\(post)","author":{"did":"\(author)"},"likeCount":2,"replyCount":1,"repostCount":3,"quoteCount":4}]}
      """.utf8))
    let provider = AnalyticsProvider(reader: reader)
    let result = try await provider.inventory(author: author, at: Date())
    #expect(result.complete && result.items.count == 2)
    #expect(result.items.first?.engagement?.likes == 2)
    #expect(result.items.last?.engagement == nil)
    #expect(result.items.last?.url == "https://publisher.example/publication/a/read")
    let encoded = try #require(JSONSerialization.jsonObject(with: AnalyticsCoding.encoder().encode(result.items.last)) as? [String: Any])
    #expect(encoded["engagement"] is NSNull)
  }

  @Test func rejectsForeignInventoryNegativeCountsAndRepeatedCursors() async throws {
    let author = "did:plc:creator"
    var foreign = AnalyticsTestReader()
    foreign.collections = ["app.bsky.feed.post": [.init(uri: "at://did:plc:other/app.bsky.feed.post/one", value: .init(values: ["$type": "app.bsky.feed.post"]))]]
    await #expect(throws: AnalyticsError.providerUnavailable) { try await AnalyticsProvider(reader: foreign).inventory(author: author, at: Date()) }
    var repeated = AnalyticsTestReader(); repeated.repeatedCursor = true
    await #expect(throws: AnalyticsError.providerUnavailable) { try await AnalyticsProvider(reader: repeated).inventory(author: author, at: Date()) }
    var negative = AnalyticsTestReader()
    let uri = "at://\(author)/app.bsky.feed.post/one"
    negative.collections = ["app.bsky.feed.post": [.init(uri: uri, value: .init(values: ["$type": "app.bsky.feed.post"]))]]
    negative.posts = try JSONDecoder().decode(AnalyticsPosts.self, from: Data("""
      {"posts":[{"uri":"\(uri)","author":{"did":"\(author)"},"likeCount":-1,"replyCount":0,"repostCount":0,"quoteCount":0}]}
      """.utf8))
    await #expect(throws: AnalyticsError.providerUnavailable) { try await AnalyticsProvider(reader: negative).inventory(author: author, at: Date()) }
  }

  @Test func rejectsUnsupportedOrAmbiguousSubjects() throws {
    for value in ["https://publisher.example", "at://did:plc:creator/app.bsky.actor.profile/self", "at://did:plc:creator/app.bsky.feed.post/..", "at://did:plc:creator/app.bsky.feed.post/%2f", "at://did:plc:creator/app.bsky.feed.post/one?token=x", "at://did:plc:creator/app.bsky.feed.post/one/extra"] {
      #expect(throws: AnalyticsError.invalid) { try AnalyticsSubject(value) }
    }
  }
}
