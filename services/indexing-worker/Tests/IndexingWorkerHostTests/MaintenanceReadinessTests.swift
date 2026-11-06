import Foundation
import Logging
import OperationsCore
import Testing
import ThinAppViewCore

@testable import IndexingWorkerHost

@Suite("Maintenance coordinator readiness")
struct MaintenanceReadinessTests {
  @Test func refusesProjectionMaintenanceBeforeStartingResources() async {
    await #expect(throws: ThinAppViewWorkerMode.ConfigurationError.maintenanceRequiresCoordinator) {
      try await AppViewWorkerHost.run(environment: ["THIN_APPVIEW_WORKER_MODE": "maintenance"],
        role: .projection, logger: Logger(label: "maintenance.test"))
    }
  }

  @Test func configuredRssCacheCannotSilentlyFallBackAfterInitializationFailure() throws {
    #expect(throws: AppViewWorkerRuntimeError.maintenanceCacheUnavailable) {
      try AppViewWorkerHost.validateMaintenanceCache(mode: .maintenance, rssEnabled: true,
        backend: .redis, available: false)
    }
    try AppViewWorkerHost.validateMaintenanceCache(mode: .maintenance, rssEnabled: true,
      backend: .redis, available: true)
    try AppViewWorkerHost.validateMaintenanceCache(mode: .maintenance, rssEnabled: false,
      backend: .redis, available: false)
    try AppViewWorkerHost.validateMaintenanceCache(mode: .ingestion, rssEnabled: true,
      backend: .redis, available: false)
  }

  @Test func requiresCoordinatorFenceEvenInMaintenance() async {
    await #expect(throws: AppViewWorkerRuntimeError.coordinatorRequiresAuthority) {
      try await AppViewWorkerHost.run(environment: ["THIN_APPVIEW_WORKER_MODE": "maintenance"],
        role: .coordinator, logger: Logger(label: "maintenance.test"))
    }
  }

  @Test func acceptsFreshMaintenanceEvidenceWithoutIngestionCompleteness() async throws {
    let now = Date()
    let state = makeState(now: now)
    let probe = WorkerReadinessProbe(databaseProbe: {}, serviceStateProbe: { state },
      now: { now }, mode: .maintenance)
    try await probe.run()
    await #expect(throws: (any Error).self) {
      try await WorkerReadinessProbe(databaseProbe: {}, serviceStateProbe: { state }, now: { now }).run()
    }
  }

  @Test func rejectsMissingFailedStaleOrWrongModeEvidence() async {
    let now = Date()
    for state in [makeState(now: now.addingTimeInterval(-16)),
      makeState(now: now, overrides: ["maintenance_retention": "failed"]),
      makeState(now: now, overrides: ["maintenance_rss": "starting"]),
      makeState(now: now, overrides: ["worker_mode": "ingestion"]),
      makeState(now: now, overrides: ["jetstream_replay": "enabled"]),
      makeState(now: now, overrides: ["coordinator_authority": "inactive"])] {
      await #expect(throws: (any Error).self) {
        try await WorkerReadinessProbe(databaseProbe: {}, serviceStateProbe: { state },
          now: { now }, mode: .maintenance).run()
      }
    }
  }

  private func makeState(now: Date, overrides: [String: String] = [:]) -> OperationsServiceState {
    OperationsServiceState(service: "coordinator-appview", environment: "dev", instanceId: "fixture",
      liveness: .healthy, readiness: .healthy, freshness: .healthy, completeness: .unknown,
      dependencyState: ["coordinator_authority": "active",
        "coordinator_role": "indexing.appview-coordinator", "worker_mode": "maintenance", "worker_role": "coordinator",
        "ingestion_source": "disabled", "maintenance_retention": "ready", "maintenance_rss": "ready",
        "jetstream_replay": "disabled_by_worker_mode", "pds_reconciliation": "disabled_by_worker_mode"]
        .merging(overrides) { _, new in new },
      version: nil, startedAt: now, heartbeatAt: now)
  }
}
