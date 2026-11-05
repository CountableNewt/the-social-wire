import AsyncHTTPClient
import Foundation
import GatewayCore
import NIOFoundationCompat

struct AnalyticsHTTPReader: AnalyticsPublicReading {
  let repo: ATProtoAuthenticatedRepoClient
  let http: HTTPClient
  var appViewBase = "https://public.api.bsky.app"

  func records(author: String, collection: String, cursor: String?) async throws -> (records: [AnalyticsPublicRecord], cursor: String?) {
    let page = try await repo.listRecords(auth: nil, repo: author, collection: collection,
      limit: 100, cursor: cursor, reverse: true, requireResolvedRepository: true)
    return (page.records.map { .init(uri: $0.uri, value: $0.value) }, page.cursor)
  }
  func record(uri: String) async throws -> AnalyticsPublicRecord? {
    let subject = try AnalyticsSubject(uri, publication: uri.contains("/site.standard.publication/"))
    guard let record = try await repo.getRecordWithMetadata(auth: nil, repo: subject.did,
      collection: subject.collection, rkey: subject.rkey) else { return nil }
    return .init(uri: record.uri, value: record.value)
  }
  func engagement(uris: [String]) async throws -> AnalyticsPosts {
    guard var components = URLComponents(string: appViewBase), components.scheme == "https",
      components.user == nil, components.password == nil, components.host != nil else { throw AnalyticsError.providerUnavailable }
    components.path = "/xrpc/app.bsky.feed.getPosts"
    components.queryItems = uris.map { .init(name: "uris", value: $0) }
    guard let url = components.url else { throw AnalyticsError.providerUnavailable }
    let response = try await http.execute(HTTPClientRequest(url: url.absoluteString), timeout: .seconds(20))
    guard response.status.code == 200 else { throw AnalyticsError.providerUnavailable }
    let body = try await response.body.collect(upTo: 2 << 20)
    return try JSONDecoder().decode(AnalyticsPosts.self, from: Data(buffer: body))
  }
}
