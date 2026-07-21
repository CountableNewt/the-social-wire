import Foundation
import Logging
import OperationsCore

struct ThinAppViewRecoveryJobRunner: Sendable {
  let store: any OperationsStore
  let indexer: ThinAppViewIndexer
  let pdsBackfill: ThinAppViewEnrollBackfill
  let relayURL: String
  let workerId: String
  let logger: Logger

  func runForever() async {
    while !Task.isCancelled {
      do {
        if let job = try await store.claimNextBackfill(
          workerId: workerId,
          leaseUntil: Date().addingTimeInterval(60),
          at: Date()
        ) {
          try await execute(job)
        } else {
          try await Task.sleep(for: .seconds(5))
        }
      } catch {
        logger.error(
          "Recovery job poll failed",
          metadata: ["error_type": .string(OperationsRedactor.errorCategory(error))]
        )
        try? await Task.sleep(for: .seconds(5))
      }
    }
  }

  private func execute(_ job: BackfillJob) async throws {
    do {
      let processed: Int
      let cursor: Int64?
      switch job.sourceMode {
      case .pdsReconciliation:
        processed = try await pdsBackfill.enroll(
          authorDids: job.authorDids,
          collections: job.collections,
          shouldContinue: { await shouldContinue(job.id) }
        )
        cursor = job.checkpointCursor
      case .jetstreamReplay:
        let executor = JetstreamReplayExecutor(
          relayURL: relayURL,
          indexer: indexer,
          store: store,
          job: job,
          logger: logger
        )
        let result = try await executor.run()
        processed = result.processed
        cursor = result.cursor
      }

      guard let current = try await store.fetchBackfill(id: job.id), current.status == .running
      else { return }
      try await store.checkpointBackfill(
        id: job.id,
        cursor: cursor,
        processed: processed,
        failed: current.failedCount,
        reconciled: job.sourceMode == .pdsReconciliation ? processed : current.reconciledCount,
        leaseUntil: Date().addingTimeInterval(60),
        at: Date()
      )
      try await store.updateBackfillStatus(
        id: job.id,
        status: .completed,
        operatorDid: "system:worker",
        failureReason: nil,
        at: Date()
      )
      if let gapId = job.gapId {
        try await store.updateGap(
          id: gapId, status: .resolved, operatorDid: "system:worker", at: Date())
      }
    } catch RecoveryControlError.stopped {
      return
    } catch {
      try? await store.updateBackfillStatus(
        id: job.id,
        status: .failed,
        operatorDid: "system:worker",
        failureReason: OperationsRedactor.errorCategory(error),
        at: Date()
      )
      throw error
    }
  }

  private func shouldContinue(_ id: String) async -> Bool {
    guard let job = try? await store.fetchBackfill(id: id) else { return false }
    return job.status == .running
  }
}

private enum RecoveryControlError: Error { case stopped, replayComplete }
private struct ReplayIncompleteError: Error {}

private struct JetstreamReplayResult: Sendable {
  let processed: Int
  let cursor: Int64?
}

private actor JetstreamReplayProgress {
  var processed: Int
  var cursor: Int64?

  init(processed: Int, cursor: Int64?) {
    self.processed = processed
    self.cursor = cursor
  }

  func advanced(to cursor: Int64) {
    processed += 1
    self.cursor = cursor
  }
  func completed(through cursor: Int64) {
    self.cursor = cursor
  }
  func snapshot() -> JetstreamReplayResult {
    JetstreamReplayResult(processed: processed, cursor: cursor)
  }
}

private struct JetstreamReplayExecutor: Sendable {
  let relayURL: String
  let indexer: ThinAppViewIndexer
  let store: any OperationsStore
  let job: BackfillJob
  let logger: Logger

  func run() async throws -> JetstreamReplayResult {
    let window = JetstreamReplayWindow(
      lowerBound: job.checkpointCursor ?? job.startCursor ?? 0,
      upperBound: job.endCursor
    )
    let replayURL = try JetstreamCursor.url(relayURL, cursor: window.connectionCursor)
    let progress = JetstreamReplayProgress(
      processed: job.processedCount,
      cursor: job.checkpointCursor
    )
    do {
      #if canImport(WebSocketKit)
        try await FirehoseLinuxWebSocket.consume(relayURL: replayURL, logger: logger) { text in
          try await handle(text, progress: progress)
        }
      #else
        try await FirehoseSubscriberURLSessionTransport.consume(
          relayURL: replayURL,
          logger: logger,
          isCancelled: { Task.isCancelled }
        ) { text in
          try await handle(text, progress: progress)
        }
      #endif
    } catch RecoveryControlError.replayComplete {
      return await progress.snapshot()
    }
    throw ReplayIncompleteError()
  }

  private func handle(_ text: String, progress: JetstreamReplayProgress) async throws {
    guard let current = try await store.fetchBackfill(id: job.id), current.status == .running else {
      throw RecoveryControlError.stopped
    }
    guard
      let data = text.data(using: .utf8),
      let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
      (json["kind"] as? String) == "commit",
      let cursor = JetstreamCursor.parse(json["time_us"])
    else { return }
    let window = JetstreamReplayWindow(
      lowerBound: job.checkpointCursor ?? job.startCursor ?? 0,
      upperBound: job.endCursor
    )
    if window.isPastUpperBound(cursor) {
      if let upperBound = window.upperBound {
        await progress.completed(through: upperBound)
      }
      throw RecoveryControlError.replayComplete
    }
    guard window.contains(cursor) else { return }
    guard
      let did = json["did"] as? String,
      let commit = json["commit"] as? [String: Any],
      let collection = commit["collection"] as? String,
      job.collections.contains(collection),
      let rkey = commit["rkey"] as? String,
      let operation = commit["operation"] as? String
    else { return }
    let record = commit["record"] ?? [:]
    let recordJSON = (try? JSONSerialization.data(withJSONObject: record)) ?? Data("{}".utf8)
    do {
      let intervalNanoseconds = Int64(1_000_000_000 / max(1, job.rateLimit))
      try await Task.sleep(for: .nanoseconds(intervalNanoseconds))
      try await indexer.handleCommit(
        repoDid: did,
        collection: collection,
        rkey: rkey,
        cid: commit["cid"] as? String ?? "",
        recordJSON: recordJSON,
        operation: operation,
        ingestionSource: "jetstream_replay",
        cursor: String(cursor),
        eventTime: Date(timeIntervalSince1970: Double(cursor) / 1_000_000)
      )
      await progress.advanced(to: cursor)
      let snapshot = await progress.snapshot()
      if snapshot.processed.isMultiple(of: max(1, job.batchSize)) {
        try await store.checkpointBackfill(
          id: job.id,
          cursor: cursor,
          processed: snapshot.processed,
          failed: job.failedCount,
          reconciled: job.reconciledCount,
          leaseUntil: Date().addingTimeInterval(60),
          at: Date()
        )
      }
    } catch {
      try? await store.recordRecoveryFailure(
        jobId: job.id,
        identityHash: OperationsRedactor.hashIdentity("\(did)/\(collection)/\(rkey)"),
        collection: collection,
        operation: operation,
        cursor: cursor,
        errorCategory: OperationsRedactor.errorCategory(error),
        at: Date()
      )
      let snapshot = await progress.snapshot()
      try? await store.checkpointBackfill(
        id: job.id,
        cursor: cursor,
        processed: snapshot.processed,
        failed: current.failedCount + 1,
        reconciled: current.reconciledCount,
        leaseUntil: Date().addingTimeInterval(60),
        at: Date()
      )
      throw error
    }
  }
}

struct JetstreamReplayWindow: Equatable, Sendable {
  let lowerBound: Int64
  let upperBound: Int64?

  var connectionCursor: Int64 { max(0, lowerBound - 5_000_000) }

  func contains(_ cursor: Int64) -> Bool {
    guard cursor > lowerBound else { return false }
    return upperBound.map { cursor <= $0 } ?? true
  }

  func isPastUpperBound(_ cursor: Int64) -> Bool {
    upperBound.map { cursor > $0 } ?? false
  }
}
