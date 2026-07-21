import Foundation
import Logging
import Testing

@testable import OperationsCore

@Suite("SQLiteOperationsStore")
struct SQLiteOperationsStoreTests {
  @Test("Received and committed cursors advance independently and never regress")
  func checkpointsDoNotRegress() async throws {
    let url = FileManager.default.temporaryDirectory
      .appendingPathComponent("operations-\(UUID().uuidString).sqlite")
    defer { try? FileManager.default.removeItem(at: url) }
    let store = try SQLiteOperationsStore(path: url.path, logger: Logger(label: "operations.test"))
    let now = Date()

    try await store.markStreamReceived(
      source: "jetstream", cursor: 2_000, eventAt: now, receivedAt: now, queueDepth: 1
    )
    try await store.markStreamCommitted(
      source: "jetstream", cursor: 1_900, eventAt: now, committedAt: now, queueDepth: 0
    )
    try await store.markStreamReceived(
      source: "jetstream", cursor: 1_500, eventAt: now, receivedAt: now, queueDepth: 0
    )

    let state = try #require(await store.fetchStreamState(source: "jetstream"))
    #expect(state.lastReceivedCursor == 2_000)
    #expect(state.lastCommittedCursor == 1_900)
  }

  @Test("Backfill jobs are dry-run first, leaseable, resumable, and auditable")
  func backfillLifecycle() async throws {
    let url = FileManager.default.temporaryDirectory
      .appendingPathComponent("operations-\(UUID().uuidString).sqlite")
    defer { try? FileManager.default.removeItem(at: url) }
    let store = try SQLiteOperationsStore(path: url.path, logger: Logger(label: "operations.test"))
    let request = BackfillDryRunRequest(
      sourceMode: .jetstreamReplay,
      startCursor: 1_000_000,
      endCursor: 6_000_000,
      collections: ["site.standard.document"],
      batchSize: 100,
      rateLimit: 50,
      maxConcurrency: 1
    )
    let estimate = try await store.estimateBackfill(request)
    #expect(estimate.estimatedCount > 0)

    let job = try await store.createBackfill(
      CreateBackfillRequest(
        dryRun: request,
        expectedEstimate: estimate.estimatedCount,
        auditNote: "Repair test gap",
        environmentConfirmation: nil
      ),
      operatorDid: "did:plc:operator",
      at: Date()
    )
    let claimed = try #require(
      await store.claimNextBackfill(
        workerId: "worker-1", leaseUntil: Date().addingTimeInterval(60), at: Date()
      )
    )
    #expect(claimed.id == job.id)
    #expect(claimed.status == .running)

    try await store.checkpointBackfill(
      id: job.id,
      cursor: 3_000_000,
      processed: 20,
      failed: 1,
      reconciled: 19,
      leaseUntil: Date().addingTimeInterval(60),
      at: Date()
    )
    let updated = try #require(await store.fetchBackfill(id: job.id))
    #expect(updated.checkpointCursor == 3_000_000)
    #expect(updated.processedCount == 20)

    try await store.updateBackfillStatus(
      id: job.id,
      status: .failed,
      operatorDid: "system:worker",
      failureReason: "database_timeout",
      at: Date()
    )
    let failed = try #require(await store.fetchBackfill(id: job.id))
    #expect(failed.failureReason == "database_timeout")
  }

  @Test("Metric rollups retain real bucket values and dimensions")
  func metricRollups() async throws {
    let url = FileManager.default.temporaryDirectory
      .appendingPathComponent("operations-\(UUID().uuidString).sqlite")
    defer { try? FileManager.default.removeItem(at: url) }
    let store = try SQLiteOperationsStore(path: url.path, logger: Logger(label: "operations.test"))
    let now = Date()

    try await store.recordMetric(
      OperationsMetricSample(
        name: "socialwire.ingestion.events_total",
        value: 1,
        dimensions: ["collection": "site.standard.document", "operation": "create"],
        recordedAt: now
      )
    )

    let rollups = try await store.listMetricRollups(
      startAt: now.addingTimeInterval(-60),
      endAt: now.addingTimeInterval(60),
      limit: 100
    )
    let rollup = try #require(rollups.first)
    #expect(rollup.metricName == "socialwire.ingestion.events_total")
    #expect(rollup.dimensions["collection"] == "site.standard.document")
    #expect(rollup.sampleCount == 1)
    #expect(rollup.valueSum == 1)
  }
}
