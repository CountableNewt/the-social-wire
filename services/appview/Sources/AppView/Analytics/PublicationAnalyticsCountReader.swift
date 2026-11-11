import AsyncHTTPClient
import Foundation
import NIOFoundationCompat

struct PublicationAnalyticsCountReader: Sendable {
  let http: HTTPClient
  var transport: (@Sendable (URL) async throws -> Data)? = nil

  func count(target: String, collection: String, path: String) async throws -> Int {
    var components = URLComponents(string: "https://constellation.microcosm.blue/links/count/distinct-dids")!
    components.queryItems = [
      .init(name: "target", value: target),
      .init(name: "collection", value: collection),
      .init(name: "path", value: path),
    ]
    guard let url = components.url else { throw AnalyticsError.invalid }
    let data: Data
    if let transport {
      data = try await transport(url)
    } else {
      var request = HTTPClientRequest(url: url.absoluteString)
      request.headers.add(name: "Accept", value: "application/json")
      request.headers.add(name: "User-Agent", value: "TheSocialWire/1 (+https://thesocialwire.app)")
      let response = try await http.execute(request, timeout: .seconds(2))
      guard response.status == .ok else { throw AnalyticsError.providerUnavailable }
      data = try await Data(buffer: response.body.collect(upTo: 4096))
    }
    let count = try JSONDecoder().decode(Count.self, from: data).total
    guard count >= 0 else { throw AnalyticsError.providerUnavailable }
    return count
  }

  private struct Count: Decodable {
    let total: Int
  }
}
