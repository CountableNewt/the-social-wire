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

      guard let current = try await store.fetchBackfill(id: job.id), current.status == .running else { return }
      try await store.checkpointBackfill(
        id: job.id,
        cursor: cursor,
        processed: processed,
        failed: current.failedCount,
        reconciled: job.sourceMode == .pdsReconciliation ? processed : current.reconciledCount,
        leaseUntil: Date().addingTimeInterval(60),
        at: Date()
      )
      try await store.updateBackfillStatus(id: job.id, status: .completed, operatorDid: "system:worker", at: Date())
      if let gapId = job.gapId {
        try await store.updateGap(id: gapId, status: .resolved, operatorDid: "system:worker", at: Date())
      }
    } catch RecoveryControlError.stopped {
      return
    } catch {
      try? await store.updateBackfillStatus(id: job.id, status: .failed, operatorDid: "system:worker", at: Date())
      throw error
    }
  }

  private func shouldContinue(_ id: String) async -> Bool {
    guard let job = try? await store.fetchBackfill(id: id) else { return false }
    return job.status == .running
  }
}

private enum RecoveryControlError: Error { case stopped, replayComplete }

private struct JetstreamReplayResult: Sendable {
  let processed: Int
  let cursor: Int64?
}

private actor JetstreamReplayProgress {
  var processed = 0
  var cursor: Int64?
  func advanced(to cursor: Int64) { processed += 1; self.cursor = cursor }
  func snapshot() -> JetstreamReplayResult { JetstreamReplayResult(processed: processed, cursor: cursor) }
}

private struct JetstreamReplayExecutor: Sendable {
  let relayURL: String
  let indexer: ThinAppViewIndexer
  let store: any OperationsStore
  let job: BackfillJob
  let logger: Logger

  func run() async throws -> JetstreamReplayResult {
    let start = max(0, (job.checkpointCursor ?? job.startCursor ?? 0) - 5_000_000)
    let replayURL = try JetstreamCursor.url(relayURL, cursor: start)
    let progress = JetstreamReplayProgress()
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
    return await progress.snapshot()
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
    if let end = job.endCursor, cursor > end { throw RecoveryControlError.replayComplete }
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
      throw error
    }
  }
}
