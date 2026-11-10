import AsyncHTTPClient
import Foundation
import NIOFoundationCompat

struct BlueskyAnalyticsHTTPReader: BlueskyAnalyticsReading {
  let http: HTTPClient
  var transport: (@Sendable (URL) async throws -> Data)? = nil

  func profile(author: String) async throws -> BlueskyAnalyticsProfile {
    let value: Profile = try await get("app.bsky.actor.getProfile", query: [.init(name: "actor", value: author)])
    guard value.did == author else { throw AnalyticsError.providerUnavailable }
    return .init(followers: count(value.followersCount), following: count(value.followsCount), posts: count(value.postsCount))
  }

  func feed(author: String, cursor: String?) async throws -> BlueskyAnalyticsFeed {
    var query = [URLQueryItem(name: "actor", value: author), .init(name: "limit", value: "100"),
      .init(name: "filter", value: "posts_with_replies"), .init(name: "includePins", value: "false")]
    if let cursor { query.append(.init(name: "cursor", value: cursor)) }
    return try await get("app.bsky.feed.getAuthorFeed", query: query)
  }

  func shares(url: String, since: Date, until: Date) async throws -> [BlueskyAnalyticsFeed.Post] {
    let format = ISO8601DateFormatter()
    let value: Search = try await get("app.bsky.feed.searchPosts", query: [
      .init(name: "q", value: "*"), .init(name: "url", value: url), .init(name: "sort", value: "latest"),
      .init(name: "since", value: format.string(from: since)), .init(name: "until", value: format.string(from: until)),
      .init(name: "limit", value: "25")])
    return value.posts
  }

  private func count(_ value: Int?) -> Int? { value.flatMap { $0 >= 0 ? $0 : nil } }

  private func get<T: Decodable>(_ method: String, query: [URLQueryItem]) async throws -> T {
    // Public search is served by the uncached AppView; the cached host rejects it.
    let origin = method == "app.bsky.feed.searchPosts" ? "https://api.bsky.app" : "https://public.api.bsky.app"
    var components = URLComponents(string: origin)!
    components.path = "/xrpc/" + method
    components.queryItems = query
    guard let url = components.url else { throw AnalyticsError.providerUnavailable }
    if let transport { return try JSONDecoder().decode(T.self, from: await transport(url)) }
    let response = try await http.execute(HTTPClientRequest(url: url.absoluteString), timeout: .seconds(3))
    guard response.status.code == 200 else { throw AnalyticsError.providerUnavailable }
    let body = try await response.body.collect(upTo: 2 << 20)
    return try JSONDecoder().decode(T.self, from: Data(buffer: body))
  }

  private struct Profile: Decodable {
    var did: String
    var followersCount: Int?
    var followsCount: Int?
    var postsCount: Int?
  }
  private struct Search: Decodable { var posts: [BlueskyAnalyticsFeed.Post] }
}
