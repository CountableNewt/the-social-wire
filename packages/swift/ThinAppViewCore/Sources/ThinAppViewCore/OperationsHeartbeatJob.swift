import Foundation
import Logging
import OperationsCore

public struct OperationsHeartbeatJob: Sendable {
  let store: any OperationsStore
  let service: String
  let environment: String
  let instanceId: String
  let logger: Logger

  public init(
    store: any OperationsStore,
    service: String,
    environment: String,
    instanceId: String,
    logger: Logger
  ) {
    self.store = store
    self.service = service
    self.environment = environment
    self.instanceId = instanceId
    self.logger = logger
  }

  public func runForever() async {
    let startedAt = Date()
    while !Task.isCancelled {
      do {
        let now = Date()
        let stream = try await store.fetchStreamState(source: "jetstream")
        let freshness: OperationsHealthState
        if let committedAt = stream?.lastCommittedAt {
          freshness = now.timeIntervalSince(committedAt) < 300 ? .healthy : .degraded
        } else {
          freshness = .unknown
        }
        let gaps = try await store.listGaps(limit: 100)
        let completeness: OperationsHealthState = gaps.contains { $0.status == .confirmed } ? .degraded : .healthy
        try await store.upsertServiceState(
          OperationsServiceState(
            service: service,
            environment: environment,
            instanceId: instanceId,
            liveness: .healthy,
            readiness: .healthy,
            freshness: freshness,
            completeness: completeness,
            dependencyState: ["operations_database": "ready"],
            version: ProcessInfo.processInfo.environment["FLY_IMAGE_REF"],
            startedAt: startedAt,
            heartbeatAt: now
          )
        )
      } catch {
        logger.warning(
          "Operations heartbeat failed",
          metadata: ["error_type": .string(OperationsRedactor.errorCategory(error))]
        )
      }
      try? await Task.sleep(for: .seconds(15))
    }
  }
}
