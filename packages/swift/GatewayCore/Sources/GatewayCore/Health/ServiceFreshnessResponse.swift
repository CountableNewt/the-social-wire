import Foundation
import Hummingbird
import OperationsCore

public struct ServiceFreshnessResponse: Codable, Sendable, ResponseEncodable {
  public let service: String
  public let freshness: OperationsHealthState
  public let completeness: OperationsHealthState
  public let checkedAt: Date

  public static func evaluate(
    service: String,
    store: (any OperationsStore)?,
    at now: Date = Date()
  ) async throws -> ServiceFreshnessResponse {
    guard let store else {
      return ServiceFreshnessResponse(service: service, freshness: .unknown, completeness: .unknown, checkedAt: now)
    }
    let stream = try await store.fetchStreamState(source: "jetstream")
    let gaps = try await store.listGaps(limit: 100)
    let freshness: OperationsHealthState
    if let committedAt = stream?.lastCommittedAt {
      freshness = now.timeIntervalSince(committedAt) <= 300 ? .healthy : .degraded
    } else {
      freshness = .unknown
    }
    let completeness: OperationsHealthState = gaps.contains { $0.status == .confirmed } ? .degraded : .healthy
    return ServiceFreshnessResponse(service: service, freshness: freshness, completeness: completeness, checkedAt: now)
  }
}
