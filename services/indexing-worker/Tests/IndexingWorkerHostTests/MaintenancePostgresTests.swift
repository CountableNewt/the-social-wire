import Foundation
import Logging
import OperationsCore
import PostgresNIO
import Testing
import ThinAppViewCore

@testable import IndexingWorkerHost

@Suite("Maintenance durable authority", .enabled(
  if: ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"] != nil))
struct MaintenancePostgresTests {
  @Test func retainsGlobalCheckpointAndRejectsReplacedOwnerHeartbeat() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"])
    let logger = Logger(label: "maintenance.pg")
    let pool = PostgresClient(configuration: try makePostgresConfig(from: url, logger: logger),
      backgroundLogger: logger)
    let running = Task { await pool.run() }
    defer { running.cancel() }
    let role = "indexing.appview-coordinator"
    let owner = "maintenance-" + UUID().uuidString
    let generation = owner
    let control = PostgresOperationsStore(pool: pool, environment: "dev", logger: logger)
    let now = Date()
    do {
      let grant = try #require(try await control.acquireRoleLease(role: role, ownerID: owner,
        leaseUntil: now.addingTimeInterval(60), at: now))
      let worker = PostgresOperationsStore(pool: pool, environment: "dev",
        coordinatorAuthority: RoleLeaseAuthority(environment: "dev", role: role,
          ownerID: owner, fencingToken: grant.fencingToken), logger: logger)
      _ = try await pool.query("""
        INSERT INTO appview_jetstream_checkpoints(environment,source_generation,source_host,stream_nsid,
          filter_fingerprint,cursor_kind,last_staged_seq,last_applied_seq,replay_state,
          replay_after_seq,replay_bytes_downloaded,replay_retry_count,replay_range_resume_count,updated_at)
        VALUES ('dev',\(generation),'fixture','fixture','fixture','jetstream_v2_seq',500,500,
          'paused_budget',499,5373231331,10202,111,\(now))
        """, logger: logger)
      let before = try await checkpoint(pool: pool, generation: generation)
      let environment = ["DATABASE_URL": url, "APP_ENV": "dev", "ENABLE_THIN_APPVIEW": "true",
        "THIN_APPVIEW_WORKER_MODE": "maintenance", "THIN_APPVIEW_JETSTREAM_MODE": "disabled",
        "THIN_APPVIEW_RSS_FEED_POLL_ENABLED": "false", "OPERATIONS_RECOVERY_ENABLED": "true",
        "OPERATIONS_INSTANCE_ID": owner, "APPVIEW_CACHE_BACKEND": "postgres"]
      let host = Task {
        try await AppViewWorkerHost.run(environment: environment, role: .coordinator,
          serviceName: "coordinator-appview",
          roleLeaseAuthority: RoleLeaseAuthority(environment: "dev", role: role,
            ownerID: owner, fencingToken: grant.fencingToken), logger: logger)
      }
      var ready: OperationsServiceState?
      do {
        for _ in 0..<300 {
          ready = try await control.listServiceStates().first {
            $0.instanceId == owner && $0.readiness == .healthy
          }
          if ready != nil { break }
          try await Task.sleep(for: .milliseconds(25))
        }
      } catch {
        host.cancel()
        _ = await host.result
        throw error
      }
      host.cancel()
      _ = await host.result
      let published = try #require(ready)
      try await WorkerReadinessProbe(databaseProbe: {}, serviceStateProbe: { published },
        mode: .maintenance).run()
      #expect(try await checkpoint(pool: pool, generation: generation) == before)
      _ = try await pool.query("""
        UPDATE operations_role_leases SET acquired_at=clock_timestamp()-INTERVAL '2 seconds',
          lease_expires_at=clock_timestamp()-INTERVAL '1 second'
        WHERE environment='dev' AND role=\(role) AND owner_id=\(owner)
        """, logger: logger)
      _ = try #require(try await control.acquireRoleLease(role: role, ownerID: owner + "-new",
        leaseUntil: Date().addingTimeInterval(60), at: Date()))
      await #expect(throws: (any Error).self) { try await worker.upsertServiceState(published) }
      let stale = try #require(try await control.listServiceStates().first { $0.instanceId == owner })
      #expect(stale.dependencyState["coordinator_authority"] == "inactive")
      await #expect(throws: WorkerReadinessError.maintenanceEvidenceUnavailable) {
        try await WorkerReadinessProbe(databaseProbe: {}, serviceStateProbe: { stale },
          mode: .maintenance).run()
      }
      #expect(try await checkpoint(pool: pool, generation: generation) == before)
    } catch {
      await clean(pool: pool, owner: owner, generation: generation, logger: logger)
      running.cancel(); await running.value
      throw error
    }
    await clean(pool: pool, owner: owner, generation: generation, logger: logger)
    running.cancel(); await running.value
  }

  private func checkpoint(pool: PostgresClient, generation: String) async throws -> String {
    let rows = try await pool.query("SELECT to_jsonb(c)::text FROM appview_jetstream_checkpoints c WHERE environment='dev' AND source_generation=\(generation)", logger: Logger(label: "fixture"))
    for try await row in rows { return try row.decode(String.self) }
    throw FixtureError.missingCheckpoint
  }

  private func clean(pool: PostgresClient, owner: String, generation: String, logger: Logger) async {
    _ = try? await pool.query("DELETE FROM operations_role_leases WHERE environment='dev' AND owner_id IN (\(owner),\(owner + "-new"))", logger: logger)
    _ = try? await pool.query("DELETE FROM operations_service_state WHERE environment='dev' AND instance_id=\(owner)", logger: logger)
    _ = try? await pool.query("DELETE FROM appview_jetstream_checkpoints WHERE environment='dev' AND source_generation=\(generation)", logger: logger)
  }

  private enum FixtureError: Error { case missingCheckpoint }
}
