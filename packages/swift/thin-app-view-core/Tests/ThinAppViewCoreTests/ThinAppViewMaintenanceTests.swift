import Foundation
import GRDB
import Logging
import OperationsCore
import Testing

@testable import ThinAppViewCore

@Suite("Maintenance-only worker")
struct ThinAppViewMaintenanceTests {
  @Test func explicitModeRequiresCoordinator() throws {
    #expect(try ThinAppViewWorkerMode.fromEnvironment([:]) == .ingestion)
    #expect(try ThinAppViewWorkerMode.fromEnvironment(["THIN_APPVIEW_WORKER_MODE": "maintenance"]) == .maintenance)
    #expect(throws: ThinAppViewWorkerMode.ConfigurationError.invalidMode) {
      try ThinAppViewWorkerMode.fromEnvironment(["THIN_APPVIEW_WORKER_MODE": "maintenence"])
    }
    for role in [ThinAppViewWorkerRole.projection, .combined] {
      #expect(throws: ThinAppViewWorkerMode.ConfigurationError.maintenanceRequiresCoordinator) {
        try ThinAppViewWorkerMode.maintenance.validate(role: role)
      }
    }
    try ThinAppViewWorkerMode.maintenance.validate(role: .coordinator)
  }

  @Test func readinessTracksRealSweepsAndDoesNotClaimGlobalCompleteness() async {
    let state = ThinAppViewMaintenanceState(rssEnabled: true)
    let at = Date()
    #expect(await state.probe(at: at).readiness == .degraded)
    await state.started(.retention, maximumDuration: 120, at: at)
    await state.started(.rss, maximumDuration: 120, at: at)
    #expect(await state.probe(at: at).readiness == .degraded)
    await state.completed(.retention, succeeded: true, nextRunAfter: 3600, at: at)
    await state.completed(.rss, succeeded: true, nextRunAfter: 1800, at: at)
    let ready = await state.probe(at: at)
    #expect(ready.readiness == .healthy)
    #expect(ready.completeness == .unknown)
    #expect(ready.dependencyState["ingestion_source"] == "disabled")
    #expect(ready.dependencyState["jetstream_replay"] == "disabled_by_worker_mode")
    await state.completed(.rss, succeeded: false, nextRunAfter: 1800, at: at)
    #expect(await state.probe(at: at).readiness == .degraded)
    await state.started(.rss, maximumDuration: 60, at: at)
    #expect(await state.probe(at: at).readiness == .degraded)
    await state.completed(.rss, succeeded: true, nextRunAfter: 1800, at: at)
    #expect(await state.probe(at: at.addingTimeInterval(1861)).dependencyState["maintenance_rss"] == "stalled")
    await state.started(.rss, maximumDuration: 60, at: at.addingTimeInterval(1800))
    #expect(await state.probe(at: at.addingTimeInterval(1861)).readiness == .degraded)
  }

  @Test func disabledRssIsExplicit() async {
    let state = ThinAppViewMaintenanceState(rssEnabled: false)
    await state.completed(.retention, succeeded: true, nextRunAfter: 3600)
    let probe = await state.probe()
    #expect(probe.readiness == .healthy)
    #expect(probe.dependencyState["maintenance_rss"] == "disabled")
    #expect(!probe.requiredDependencyKeys.contains("maintenance_rss"))
  }

  @Test func startsWithoutV2CheckpointPLCorRecoveryAndJoinsCancellation() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent("maintenance-\(UUID()).sqlite").path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let logger = Logger(label: "maintenance.test")
    let store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let operations = try SQLiteOperationsStore(path: path, environment: "dev",
      backfillFingerprintSecret: "fixture", logger: logger)
    let config = ThinAppViewConfig.fromEnvironment([
      "ENABLE_THIN_APPVIEW": "true", "THIN_APPVIEW_JETSTREAM_MODE": "disabled",
      "THIN_APPVIEW_RSS_FEED_POLL_ENABLED": "false"])
    let operationsConfig = OperationsConfiguration.fromEnvironment([
      "APP_ENV": "dev", "OPERATIONS_INSTANCE_ID": "maintenance-fixture",
      "OPERATIONS_RECOVERY_ENABLED": "true"])
    let db = try DatabaseQueue(path: path)
    try await db.write { db in
      // Any attempt to claim the global inbox fails. Maintenance only expires terminal history.
      try db.execute(sql: """
        CREATE TRIGGER forbid_inbox_claim BEFORE UPDATE OF lease_token ON appview_ingestion_inbox
        BEGIN SELECT RAISE(ABORT, 'maintenance attempted global inbox claim'); END
        """)
    }
    let run = Task {
      try await ThinAppViewWorkerRuntime.run(store: store, config: config, role: .coordinator,
        serviceName: "maintenance-fixture", logger: logger, operationsStore: operations,
        operationsConfig: operationsConfig, mode: .maintenance)
    }
    var published = false
    for _ in 0..<300 {
      if let heartbeat = try await operations.listServiceStates().first(where: {
        $0.service == "maintenance-fixture" && $0.readiness == .healthy
      }) {
        #expect(heartbeat.dependencyState["ingestion_source"] == "disabled")
        #expect(heartbeat.dependencyState["pds_reconciliation"] == "disabled_by_worker_mode")
        published = true
        break
      }
      try await Task.sleep(for: .milliseconds(25))
    }
    run.cancel()
    _ = await run.result
    #expect(published)
    // No global durability checkpoint was manufactured for readiness.
    #expect(try await db.read { try Int.fetchOne($0, sql: "SELECT COUNT(*) FROM appview_jetstream_checkpoints") } == 0)
  }
}
