import AsyncHTTPClient
import Foundation
import NIOCore

struct RepositorySnapshotHTTPResponse: Sendable {
  let status: Int
  let retryAfter: String?
  let body: Data
}

protocol RepositorySnapshotHTTPTransport: Sendable {
  func get(url: URL, accept: String, maximumBytes: Int, deadline: ContinuousClock.Instant)
    async throws
    -> RepositorySnapshotHTTPResponse
}

struct RepositorySnapshotPublicHTTPTransport: RepositorySnapshotHTTPTransport {
  let httpClient: HTTPClient
  var userAgent: String? = nil

  func get(url: URL, accept: String, maximumBytes: Int, deadline: ContinuousClock.Instant)
    async throws
    -> RepositorySnapshotHTTPResponse
  {
    guard let host = url.host, url.scheme == "https", url.user == nil, url.password == nil,
      url.port == nil || url.port == 443
    else { throw RepositorySnapshotError.invalid("invalid public URL") }
    let requestDeadline = min(deadline, .now.advanced(by: .seconds(15)))
    let address = try await RepositoryDNSAddressValidator.validatedAddress(
      for: url.absoluteString,
      deadline: min(requestDeadline, .now.advanced(by: .seconds(10))))
    var configuration = HTTPClient.Configuration()
    configuration.dnsOverride = [host: address]
    configuration.redirectConfiguration = .disallow
    configuration.timeout = .init(connect: .seconds(10), read: .seconds(15))
    let client = HTTPClient(eventLoopGroup: httpClient.eventLoopGroup, configuration: configuration)
    do {
      let value = try await withThrowingTaskGroup(of: RepositorySnapshotHTTPResponse.self) {
        group in
        group.addTask {
          var request = HTTPClientRequest(url: url.absoluteString)
          request.headers.add(name: "Accept", value: accept)
          if let userAgent { request.headers.add(name: "User-Agent", value: userAgent) }
          let response = try await client.execute(request, timeout: .seconds(15))
          let body = try await response.body.collect(upTo: maximumBytes)
          return RepositorySnapshotHTTPResponse(
            status: Int(response.status.code),
            retryAfter: response.headers.first(name: "Retry-After"), body: Data(buffer: body))
        }
        group.addTask {
          try await Task.sleep(until: requestDeadline, clock: .continuous)
          throw RepositorySnapshotError.invalid("request deadline exceeded")
        }
        defer { group.cancelAll() }
        guard let result = try await group.next() else { throw CancellationError() }
        return result
      }
      try await client.shutdown()
      return value
    } catch {
      try? await client.shutdown()
      throw error
    }
  }
}
