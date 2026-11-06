import AsyncHTTPClient
import Foundation
import GatewayCore

struct ArticleEmbedHTTPReader: ArticleEmbedReading {
  let http: HTTPClient
  let repo: ATProtoAuthenticatedRepoClient

  func page(url: String) async throws -> (status: Int, body: Data) {
    var current = url
    for hop in 0...3 {
      let exchange = try await PublicMediaFetcher.open(url: current, httpClient: http,
        timeout: .seconds(12), headers: ["Accept": "text/html"])
      do {
        let status = Int(exchange.response.status.code)
        if [301, 302, 303, 307, 308].contains(status), hop < 3,
          let location = exchange.response.headers.first(name: "location"),
          let next = URL(string: location, relativeTo: URL(string: current))?.absoluteURL.absoluteString {
          try await exchange.client.shutdown()
          current = next
          continue
        }
        let body = try await exchange.response.body.collect(upTo: 1 << 20)
        try await exchange.client.shutdown()
        return (status, Data(buffer: body))
      } catch {
        try? await exchange.client.shutdown()
        throw error
      }
    }
    throw ArticleEmbedError.unavailable
  }

  func record(uri: String) async throws -> AnalyticsPublicRecord? {
    guard let subject = ArticleEmbedResolver.subject(uri) else { return nil }
    guard let value = try await repo.getRecordWithMetadata(auth: nil, repo: subject.did,
      collection: subject.collection, rkey: subject.rkey) else { return nil }
    return .init(uri: value.uri, value: value.value)
  }
}
