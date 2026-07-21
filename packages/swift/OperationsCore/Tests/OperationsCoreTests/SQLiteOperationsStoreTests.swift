import Foundation
import Logging
import Testing

@testable import OperationsCore

@Suite("SQLiteOperationsStore")
struct SQLiteOperationsStoreTests {
  @Test("Jetstream endpoint state and reconnect commands are durable")
  func jetstreamRecoveryControlLifecycle() async throws {
    let url = FileManager.default.temporaryDirectory
      .appendingPathComponent("operations-\(UUID().uuidString).sqlite")
    defer { try? FileManager.default.removeItem(at: url) }
    let store = try SQLiteOperationsStore(path: url.path, logger: Logger(label: "operations.test"))
    let now = Date()

    try await store.upsertJetstreamEndpoint(
      JetstreamEndpointState(
        id: "jetstream1.us-east.bsky.network",
        displayName: "Jetstream 1",
        host: "jetstream1.us-east.bsky.network",
        role: .active,
        connectionState: .connected,
        lastConnectedAt: now,
        connectionAttempts: 2,
        failoverCount: 1,
        updatedAt: now
      )
    )
    let endpoint = try #require(await store.listJetstreamEndpoints().first)
    #expect(endpoint.role == .active)
    #expect(endpoint.connectionAttempts == 2)

    let command = try await store.createCommand(
      action: .reconnectJetstream,
      operatorDid: "did:plc:operator",
      auditNote: "Reconnect stalled ingestion",
      at: now
    )
    let claimed = try #require(
      await store.claimNextCommand(
        action: .reconnectJetstream,
        workerId: "worker-1",
        at: now.addingTimeInterval(1)
      )
    )
    #expect(claimed.id == command.id)
    #expect(claimed.status == .running)
    #expect(claimed.claimedBy == "worker-1")

    try await store.completeCommand(
      id: command.id,
      status: .completed,
      failureReason: nil,
      at: now.addingTimeInterval(2)
    )
    let completed = try #require(await store.listCommands(limit: 10).first)
    #expect(completed.status == .completed)
    #expect(completed.completedAt != nil)
  }

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
    #expect(updated.failedCount == 1)
    #expect(updated.reconciledCount == 19)

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

  @Test("Dry run blocks cursor ranges already recovered by live ingestion")
  func recoveredGapIsNotBackfilledAgain() async throws {
    let url = FileManager.default.temporaryDirectory
      .appendingPathComponent("operations-\(UUID().uuidString).sqlite")
    defer { try? FileManager.default.removeItem(at: url) }
    let store = try SQLiteOperationsStore(path: url.path, logger: Logger(label: "operations.test"))
    let now = Date()
    let gap = try await store.createGap(
      source: "jetstream",
      startCursor: 1_000_000,
      endCursor: 2_000_000,
      reason: "receive_commit_backlog",
      collections: [],
      detectedAt: now
    )
    try await store.markStreamCommitted(
      source: "jetstream",
      cursor: 2_000_000,
      eventAt: now,
      committedAt: now,
      queueDepth: 0
    )

    let estimate = try await store.estimateBackfill(
      BackfillDryRunRequest(
        gapId: gap.id,
        sourceMode: .jetstreamReplay,
        startCursor: 1_000_000,
        endCursor: 2_000_000,
        collections: ["site.standard.document"],
        batchSize: 100,
        rateLimit: 50,
        maxConcurrency: 1
      )
    )
    #expect(estimate.estimatedCount == 0)
    #expect(estimate.conflicts.contains { $0.contains("already committed") })

    let resolved = try await store.resolveSuspectedGaps(
      source: "jetstream",
      through: 2_000_000,
      at: now
    )
    #expect(resolved == [gap.id])
    #expect(try await store.listGaps(limit: 10).first?.status == .resolved)
  }

  @Test("Dry run rejects an overlapping active backfill")
  func duplicateBackfillIsRejected() async throws {
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
    let firstEstimate = try await store.estimateBackfill(request)
    _ = try await store.createBackfill(
      CreateBackfillRequest(
        dryRun: request,
        expectedEstimate: firstEstimate.estimatedCount,
        auditNote: nil,
        environmentConfirmation: nil
      ),
      operatorDid: "did:plc:operator",
      at: Date()
    )

    let duplicateEstimate = try await store.estimateBackfill(request)
    #expect(duplicateEstimate.conflicts.contains { $0.contains("active backfill") })
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
