import AsyncHTTPClient
import Foundation
import NIOCore
import NIOHTTP1

/// The same pinned, redirect-free public network boundary as signed snapshots.
struct MicrocosmPublicHTTPTransport: PDSHTTPTransport {
  let httpClient: HTTPClient
  func execute(_ request: HTTPClientRequest, timeout: TimeAmount) async throws -> HTTPClientResponse {
    guard let url = URL(string: request.url) else { throw PublicContentDiscoveryError.invalidScope }
    let transport = RepositorySnapshotPublicHTTPTransport(httpClient: httpClient,
      userAgent: request.headers.first(name: "User-Agent"))
    let response = try await transport.get(url: url, accept: "application/json", maximumBytes: 2 << 20,
      deadline: ContinuousClock.now.advanced(by: .nanoseconds(timeout.nanoseconds)))
    return HTTPClientResponse(status: .init(statusCode: response.status), body: .bytes(ByteBuffer(bytes: response.body)))
  }
}
