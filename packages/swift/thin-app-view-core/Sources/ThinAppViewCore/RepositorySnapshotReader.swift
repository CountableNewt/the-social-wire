import AsyncHTTPClient
import Foundation

struct RepositorySnapshotReader: Sendable {
  let transport: any RepositorySnapshotHTTPTransport
  let plcBase: String
  var maximumBytes = 32 * 1_024 * 1_024
  var maximumBlocks = 100_000
  var maximumRecords = 20_000
  var maximumRequests = 1_024
  var maximumDepth = 64
  var timeout: Duration = .seconds(120)
  var now: @Sendable () -> Date = { Date() }

  init(httpClient: HTTPClient, plcBase: String) {
    transport = RepositorySnapshotPublicHTTPTransport(httpClient: httpClient)
    self.plcBase = plcBase
  }

  init(transport: any RepositorySnapshotHTTPTransport, plcBase: String = "https://plc.directory") {
    self.transport = transport
    self.plcBase = plcBase
  }

  func read(did: String, collections: Set<String>) async throws -> RepositorySnapshot {
    guard !collections.isEmpty, collections.allSatisfy({ RepositoryMST.validPath($0 + "/record") })
    else {
      throw RepositorySnapshotError.invalid("invalid collection")
    }
    var configuration = self
    configuration.maximumBytes = Self.bound(maximumBytes, ceiling: 32 * 1_024 * 1_024)
    configuration.maximumBlocks = Self.bound(maximumBlocks, ceiling: 100_000)
    configuration.maximumRecords = Self.bound(maximumRecords, ceiling: 20_000)
    configuration.maximumRequests = Self.bound(maximumRequests, ceiling: 1_024)
    configuration.maximumDepth = Self.bound(maximumDepth, ceiling: 64)
    let duration = timeout > .zero && timeout <= .seconds(120) ? timeout : .seconds(120)
    let deadline = ContinuousClock.now.advanced(by: duration)
    let attempt = RepositorySnapshotAttempt(reader: configuration, deadline: deadline)
    return try await withThrowingTaskGroup(of: RepositorySnapshot.self) { group in
      group.addTask { try await attempt.read(did: did, collections: collections) }
      group.addTask {
        try await Task.sleep(until: deadline, clock: .continuous)
        throw RepositorySnapshotError.invalid("snapshot deadline exceeded")
      }
      defer { group.cancelAll() }
      guard let result = try await group.next() else { throw CancellationError() }
      return result
    }
  }

  private static func bound(_ value: Int, ceiling: Int) -> Int {
    value > 0 ? min(value, ceiling) : ceiling
  }
}
