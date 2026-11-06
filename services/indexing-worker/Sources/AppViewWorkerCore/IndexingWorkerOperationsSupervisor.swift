import IndexingWorkerCore
import OperationsCore

struct IndexingWorkerOperationsSupervisor: IndexingWorkerLeaseSupervisor {
  let store: any OperationsStore

  func run(
    configuration: IndexingWorkerLeaseConfiguration,
    onEvent: @escaping @Sendable (IndexingWorkerLeaseEvent) async -> Void,
    operation: @escaping @Sendable (IndexingWorkerLeaseAuthority) async throws -> Void
  ) async throws {
    let lease = try RoleLeaseSupervisorConfiguration(
      role: configuration.role, ownerID: configuration.ownerID,
      leaseDuration: configuration.leaseDuration, renewInterval: configuration.renewInterval,
      standbyRetryInterval: configuration.standbyRetryInterval
    )
    let (events, continuation) = AsyncStream<RoleLeaseSupervisorEvent>.makeStream()
    let observer = Task {
      for await event in events {
        if let mapped = Self.map(event) { await onEvent(mapped) }
      }
    }
    let supervisor = RoleLeaseSupervisor(
      store: store, configuration: lease,
      onEvent: { continuation.yield($0) })
    await supervisor.run { ownership in
      try await operation(
        .init(
          environment: ownership.environment, role: ownership.role,
          ownerID: ownership.ownerID, fencingToken: ownership.fencingToken))
    }
    continuation.finish()
    await observer.value
    try Task.checkCancellation()
  }

  private static func map(_ event: RoleLeaseSupervisorEvent) -> IndexingWorkerLeaseEvent? {
    switch event {
    case .acquired: .acquired
    case .contended: .standby
    case .acquisitionFailed: .acquisitionFailed
    case .validationFailed, .authorityExpired: .validationFailed
    case .operationStarted: .operationStarted
    case .operationStopping: .operationStopping
    case .operationStopped: .operationStopped
    case .released: .released
    case .releaseFailed: .releaseFailed
    case .controlAttempt(let observation):
      if observation.failure == nil {
        switch observation.operation {
        case .renew: .renewed
        case .validate: .validated
        default: nil
        }
      } else {
        nil
      }
    default: nil
    }
  }
}
