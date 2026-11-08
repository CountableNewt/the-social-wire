import AsyncHTTPClient
import Foundation
import Logging
import OperationsCore

extension ThinAppViewWorkerRuntime {
  /// A separate entry path prevents dormant replay and repository recovery adapters from being built.
  static func runMaintenance(
    store: any ThinAppViewStore, config: ThinAppViewConfig, serviceName: String,
    logger: Logger, httpClient: HTTPClient?, projectionCache: (any AppViewProjectionCacheStore)?,
    operationsStore: (any OperationsStore)?, operationsConfig: OperationsConfiguration?
  ) async throws {
    guard let operationsStore, let operationsConfig,
      !config.rssFeedPollEnabled || httpClient != nil
    else { throw ThinAppViewWorkerRuntimeError.missingMaintenanceDependencies }
    let state = ThinAppViewMaintenanceState(rssEnabled: config.rssFeedPollEnabled)
    let telemetry = operationsConfig.enabled
      ? OperationsTelemetryBuffer(store: operationsStore, logger: logger) : nil
    let cleanup = ThinAppViewTtlCleanupJob(
      store: store, projectionCache: projectionCache, config: config,
      tapStorageEnabled: false,
      pdsEviction: PDSReadStateEvictionConfiguration.fromEnvironment(ProcessInfo.processInfo.environment),
      environment: operationsConfig.environment, logger: logger, maintenanceState: state)
    let cacheProbe = config.rssFeedPollEnabled
      ? projectionCache.map { ThinAppViewMaintenanceCacheProbe(cache: $0) } : nil
    let heartbeat = OperationsHeartbeatJob(
      store: operationsStore, service: serviceName, environment: operationsConfig.environment,
      instanceId: operationsConfig.instanceId,
      dependencyProbe: {
        try await store.ping()
        try await cacheProbe?.run()
        return await state.probe(cacheRequired: cacheProbe != nil)
      },
      telemetry: telemetry, logger: logger)
    try await withThrowingTaskGroup(of: Void.self) { group in
      group.addTask { await cleanup.runForever() }
      group.addTask { await heartbeat.runForever() }
      if let telemetry { group.addTask { await telemetry.runForever() } }
      if config.rssFeedPollEnabled, let httpClient {
        let ingestion = ThinAppViewRssIngestion(store: store, httpClient: httpClient,
          config: config, logger: logger, projectionCache: projectionCache)
        let poll = ThinAppViewRssFeedPollJob(store: store, rssIngestion: ingestion,
          config: config, logger: logger)
        group.addTask { await poll.runForever(maintenanceState: state) }
      }
      defer { group.cancelAll() }
      try await group.next()
    }
  }
}
