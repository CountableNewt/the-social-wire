import AsyncHTTPClient
import Foundation
import GatewayCore
import Hummingbird

struct GIFCatalogService: Sendable {
  let apiKey: String
  let httpClient: HTTPClient

  func fetch(endpoint: String, parameters: [URLQueryItem]) async throws -> GIFCatalogPage {
    guard !apiKey.isEmpty else { throw HTTPError(.serviceUnavailable, message: "GIF Search Is Not Configured") }
    var url = URLComponents(string: "https://api.klipy.com/v2/\(endpoint)")!
    url.queryItems = parameters + [URLQueryItem(name: "key", value: apiKey), URLQueryItem(name: "client_key", value: "social-wire"), URLQueryItem(name: "contentfilter", value: "medium"), URLQueryItem(name: "limit", value: "24")]
    let exchange = try await PublicMediaFetcher.open(url: url.string!, httpClient: httpClient, timeout: .seconds(15))
    do {
      guard exchange.response.status.code != 429 else { throw HTTPError(.tooManyRequests, message: "GIF Search Rate Limited. Try Again Shortly.") }
      guard exchange.response.status.code == 200 else { throw HTTPError(.badGateway, message: "GIF service unavailable") }
      let body = try await exchange.response.body.collect(upTo: 2_000_000)
      let page = try JSONDecoder().decode(GIFProviderPage.self, from: Data(body.readableBytesView))
      guard (page.results?.count ?? 0) <= 100, (page.next?.utf8.count ?? 0) <= 512 else { throw HTTPError(.badGateway, message: "Invalid GIF response") }
      try await exchange.client.shutdown()
      return GIFCatalogPage(results: (page.results ?? []).compactMap(GIFMediaPolicy.normalize), next: page.next ?? "")
    } catch {
      try? await exchange.client.shutdown()
      throw error
    }
  }
}
