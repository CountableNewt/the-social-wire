import AsyncHTTPClient
import Foundation
#if canImport(FoundationNetworking)
  import FoundationNetworking
#endif
import NIOHTTP1
import ThinAppViewCore

/// Prevents scheduling OAuth or PDS discovery from reaching private services.
struct SchedulingPublicHTTPTransport: Sendable {
  let httpClient: HTTPClient

  func execute(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
    guard let url = request.url, let host = url.host, url.scheme == "https",
      url.user == nil, url.password == nil, url.port == nil || url.port == 443,
      request.httpBodyStream == nil
    else { throw URLError(.badURL) }

    let address = try await RepositoryDNSAddressValidator.publicAddress(for: url)
    var configuration = HTTPClient.Configuration()
    configuration.dnsOverride = [host: address]
    configuration.redirectConfiguration = .disallow
    configuration.timeout = .init(connect: .seconds(10), read: .seconds(15))
    let client = HTTPClient(eventLoopGroup: httpClient.eventLoopGroup, configuration: configuration)
    do {
      var upstream = HTTPClientRequest(url: url.absoluteString)
      upstream.method = HTTPMethod(rawValue: request.httpMethod ?? "GET")
      for (name, value) in request.allHTTPHeaderFields ?? [:] {
        upstream.headers.add(name: name, value: value)
      }
      if let body = request.httpBody { upstream.body = .bytes(body) }
      let result = try await client.execute(upstream, timeout: .seconds(20))
      guard !(300...399).contains(result.status.code) else {
        throw URLError(.httpTooManyRedirects)
      }
      let body = try await result.body.collect(upTo: 16 * 1024 * 1024)
      var headers: [String: String] = [:]
      for header in result.headers {
        let name = header.name.lowercased()
        headers[name] = headers[name].map { $0 + ", " + header.value } ?? header.value
      }
      guard let response = HTTPURLResponse(url: url, statusCode: Int(result.status.code),
        httpVersion: nil, headerFields: headers)
      else { throw URLError(.badServerResponse) }
      try await client.shutdown()
      return (Data(buffer: body), response)
    } catch {
      try? await client.shutdown()
      throw error
    }
  }
}
