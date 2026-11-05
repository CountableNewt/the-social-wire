import AsyncHTTPClient
import Foundation
import IndexingWorkerCore
import Logging
import OperationsCore

public enum IndexingWorkerHost {
  public static func run(
    environment: [String: String], config: IndexingWorkerConfig, logger: Logger,
    terminateUnresponsiveProcess: @escaping @Sendable () -> Void
  ) async throws {
    guard let databaseURL = environment["DATABASE_URL"], !databaseURL.isEmpty else {
      throw IndexingWorkerHostError.missingDatabaseURL
    }
    // Only the active AppView lane is being migrated. Refuse a configuration
    // requesting dormant feeds rather than quietly dropping an active workload.
    for key in ["WIRE_FEED_MODE", "FINANCE_FEED_MODE", "SPORTS_FEED_MODE"] {
      if let mode = environment[key], !mode.isEmpty, mode != "off" {
        throw IndexingWorkerHostError.inactiveWorkloadRequested(key)
      }
    }
    let appEnvironment = try OperationsConfiguration.requireEnvironment(environment)
    let control = try IndexingWorkerControlDatabase.make(
      role: config.role,
      databaseURL: databaseURL, environment: environment, appEnvironment: appEnvironment,
      logger: logger)
    let healthClient = HTTPClient(eventLoopGroupProvider: .singleton)
    let supervisor = control.map { IndexingWorkerOperationsSupervisor(store: $0.store) }
    let runtime = try IndexingWorkerRuntime(
      config: config, lanes: [.appView],
      factories: [
        .appView: {
          IndexingWorkerAppViewComponent(
            environment: environment, role: config.role,
            healthPort: config.appViewHealthPort, client: healthClient, logger: logger)
        }
      ], supervisor: supervisor,
      onComponentError: { lane, _ in
        logger.error(
          "Indexing component failed; durable work retained",
          metadata: ["lane": .string(lane.rawValue)])
      }, terminate: { _ in terminateUnresponsiveProcess() })
    do {
      try await withThrowingTaskGroup(of: Void.self) { group in
        if let control {
          group.addTask { await control.pool.run() }
          group.addTask { await control.diagnosticPool.run() }
        }
        group.addTask { try await runtime.run() }
        group.addTask {
          try await IndexingWorkerHealthServer.run(
            role: config.role,
            startupProbe: { try await runtime.startup() },
            readinessProbe: { try await runtime.ready() },
            host: config.host, port: config.port, logger: logger)
        }
        defer { group.cancelAll() }
        try await group.next()
      }
    } catch {
      try? await healthClient.shutdown()
      throw error
    }
    try await healthClient.shutdown()
  }
}
