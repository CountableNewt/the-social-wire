import Foundation
import Logging
import OperationsCore

/// Consumes Jetstream commits in receive order and advances the durable cursor only after indexing succeeds.
actor FirehoseSubscriber {
  private var endpointPool: JetstreamEndpointPool
  private var endpointStates: [String: JetstreamEndpointState]
  private let indexer: ThinAppViewIndexer
  private let operationsStore: (any OperationsStore)?
  private let telemetry: OperationsTelemetryBuffer?
  private let environment: String
  private let instanceId: String
  private let replayRewindMicroseconds: Int64
  private let logger: Logger
  private var suspectedGapEndCursor: Int64?
  private var pendingReconnectCommand: OperationsWorkerCommand?
  private var pendingReconnectFailures = 0

  init(
    relayURLs: [String],
    indexer: ThinAppViewIndexer,
    operationsStore: (any OperationsStore)?,
    telemetry: OperationsTelemetryBuffer?,
    environment: String,
    instanceId: String,
    replayRewindMicroseconds: Int64,
    logger: Logger
  ) {
    let pool = JetstreamEndpointPool(urls: relayURLs)
    self.endpointPool = pool
    self.endpointStates = Dictionary(
      uniqueKeysWithValues: pool.endpoints.enumerated().map { index, endpoint in
        (
          endpoint.id,
          JetstreamEndpointState(
            id: endpoint.id,
            displayName: endpoint.displayName,
            host: endpoint.host,
            role: index == 0 ? .active : .standby,
            connectionState: .unknown,
            updatedAt: Date()
          )
        )
      }
    )
    self.indexer = indexer
    self.operationsStore = operationsStore
    self.telemetry = telemetry
    self.environment = environment
    self.instanceId = instanceId
    self.replayRewindMicroseconds = replayRewindMicroseconds
    self.logger = logger
  }

  func runForever() async {
    while !Task.isCancelled {
      do {
        try await consumeOnce()
      } catch let request as ManualJetstreamReconnectRequest {
        pendingReconnectCommand = request.command
        pendingReconnectFailures = 0
        logger.notice(
          "Operator requested Jetstream reconnect",
          metadata: ["command_id": .string(request.command.id)]
        )
      } catch {
        let now = Date()
        await markActiveEndpointDisconnected(error: error, at: now)
        await failReconnectCommandIfAllEndpointsFailed(error: error, at: now)
        try? await operationsStore?.markStreamDisconnected(
          source: "jetstream",
          reason: OperationsRedactor.errorCategory(error),
          at: now
        )
        await emitEvent("jetstream.disconnected", attributes: ["error_type": OperationsRedactor.errorCategory(error)])
        await recordDisconnectGap(reason: error, at: now)
        logger.warning(
          "Firehose disconnected; failing over from durable cursor",
          metadata: [
            "error_type": .string(OperationsRedactor.errorCategory(error)),
            "next_endpoint": .string(endpointPool.active.host),
          ]
        )
        try? await Task.sleep(for: .seconds(3))
      }
    }
  }

  private func consumeOnce() async throws {
    let endpoint = endpointPool.active
    await beginConnectionAttempt(endpoint: endpoint, at: Date())
    let streamState = try await operationsStore?.fetchStreamState(source: "jetstream")
    if let gaps = try await operationsStore?.listGaps(limit: 250) {
      suspectedGapEndCursor = gaps
        .filter { $0.source == "jetstream" && $0.status == .suspected }
        .compactMap(\.endCursor)
        .max()
    }
    let cursor = JetstreamCursor.resumeCursor(
      committed: streamState?.lastCommittedCursor,
      seededReceived: streamState?.lastReceivedCursor,
      rewindMicroseconds: replayRewindMicroseconds
    )
    let url = try JetstreamCursor.url(endpoint.url, cursor: cursor)

    let logger = self.logger
    let workerId = instanceId
    let result = try await withThrowingTaskGroup(of: FirehoseCycleResult.self) { group in
      group.addTask {
        #if canImport(WebSocketKit)
        try await FirehoseLinuxWebSocket.consume(
          relayURL: url,
          logger: logger,
          onConnected: { await self.didConnect(endpointId: endpoint.id, at: Date()) }
        ) { text in
          try await self.handleMessage(text)
        }
        #else
        try await FirehoseSubscriberURLSessionTransport.consume(
          relayURL: url,
          logger: logger,
          isCancelled: { Task.isCancelled },
          onConnected: { await self.didConnect(endpointId: endpoint.id, at: Date()) }
        ) { text in
          try await self.handleMessage(text)
        }
        #endif
        return .streamEnded
      }
      if let operationsStore, pendingReconnectCommand == nil {
        group.addTask {
          while !Task.isCancelled {
            if let command = try await operationsStore.claimNextCommand(
              action: .reconnectJetstream,
              workerId: workerId,
              at: Date()
            ) {
              return .reconnect(command)
            }
            try await Task.sleep(for: .seconds(1))
          }
          return .streamEnded
        }
      }
      guard let first = try await group.next() else { return FirehoseCycleResult.streamEnded }
      group.cancelAll()
      return first
    }

    switch result {
    case .streamEnded:
      throw FirehoseConnectionClosedError()
    case .reconnect(let command):
      throw ManualJetstreamReconnectRequest(command: command)
    }
  }

  private func beginConnectionAttempt(endpoint: JetstreamEndpoint, at: Date) async {
    for configured in endpointPool.endpoints {
      let previous = endpointStates[configured.id]
      endpointStates[configured.id] = JetstreamEndpointState(
        id: configured.id,
        displayName: configured.displayName,
        host: configured.host,
        role: configured.id == endpoint.id ? .active : .standby,
        connectionState: configured.id == endpoint.id ? .reconnecting : (previous?.connectionState ?? .unknown),
        lastConnectedAt: previous?.lastConnectedAt,
        lastDisconnectedAt: previous?.lastDisconnectedAt,
        lastError: previous?.lastError,
        connectionAttempts: (previous?.connectionAttempts ?? 0) + (configured.id == endpoint.id ? 1 : 0),
        failoverCount: previous?.failoverCount ?? 0,
        updatedAt: at
      )
    }
    await persistEndpointStates()
  }

  private func didConnect(endpointId: String, at: Date) async {
    guard endpointPool.active.id == endpointId, let previous = endpointStates[endpointId] else { return }
    endpointStates[endpointId] = JetstreamEndpointState(
      id: previous.id, displayName: previous.displayName, host: previous.host, role: .active,
      connectionState: .connected, lastConnectedAt: at,
      lastDisconnectedAt: previous.lastDisconnectedAt, lastError: nil,
      connectionAttempts: previous.connectionAttempts, failoverCount: previous.failoverCount,
      updatedAt: at
    )
    try? await operationsStore?.markStreamConnected(source: "jetstream", at: at)
    await persistEndpointStates()
    await emitEvent("jetstream.connected", attributes: ["endpoint": previous.host])

    if let command = pendingReconnectCommand {
      await assessPostReconnectGap(at: at)
      try? await operationsStore?.completeCommand(
        id: command.id, status: .completed, failureReason: nil, at: at)
      try? await operationsStore?.recordAudit(
        operatorDid: "system:worker", action: "command.completed", targetType: "command",
        targetId: command.id, note: "Jetstream reconnected and durable cursor gap assessment completed.",
        at: at
      )
      pendingReconnectCommand = nil
      pendingReconnectFailures = 0
    }
  }

  private func markActiveEndpointDisconnected(error: Error, at: Date) async {
    let endpoint = endpointPool.active
    let previous = endpointStates[endpoint.id]
    endpointStates[endpoint.id] = JetstreamEndpointState(
      id: endpoint.id, displayName: endpoint.displayName, host: endpoint.host, role: .standby,
      connectionState: .disconnected, lastConnectedAt: previous?.lastConnectedAt,
      lastDisconnectedAt: at, lastError: OperationsRedactor.errorCategory(error),
      connectionAttempts: previous?.connectionAttempts ?? 0,
      failoverCount: (previous?.failoverCount ?? 0) + (endpointPool.endpoints.count > 1 ? 1 : 0),
      updatedAt: at
    )
    _ = endpointPool.rotateAfterFailure()
    await persistEndpointStates()
  }

  private func failReconnectCommandIfAllEndpointsFailed(error: Error, at: Date) async {
    guard let command = pendingReconnectCommand else { return }
    pendingReconnectFailures += 1
    guard pendingReconnectFailures >= endpointPool.endpoints.count else { return }
    let failure = "all_jetstream_endpoints_unavailable"
    try? await operationsStore?.completeCommand(
      id: command.id, status: .failed, failureReason: failure, at: at)
    try? await operationsStore?.recordAudit(
      operatorDid: "system:worker", action: "command.failed", targetType: "command",
      targetId: command.id,
      note: "Reconnect failed after every configured Jetstream endpoint returned \(OperationsRedactor.errorCategory(error)).",
      at: at
    )
    pendingReconnectCommand = nil
    pendingReconnectFailures = 0
  }

  private func persistEndpointStates() async {
    guard let operationsStore else { return }
    for state in endpointStates.values {
      try? await operationsStore.upsertJetstreamEndpoint(state)
    }
  }

  private func assessPostReconnectGap(at: Date) async {
    guard let operationsStore,
      let state = try? await operationsStore.fetchStreamState(source: "jetstream"),
      let candidate = JetstreamGapDetector.postReconnectCandidate(state: state)
    else { return }
    await recordGap(candidate, collections: [], at: at)
  }

  private func handleMessage(_ text: String) async throws {
    guard
      let data = text.data(using: .utf8),
      let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
      (json["kind"] as? String) == "commit",
      let cursor = JetstreamCursor.parse(json["time_us"]),
      let did = json["did"] as? String,
      let commit = json["commit"] as? [String: Any],
      let collection = commit["collection"] as? String,
      let rkey = commit["rkey"] as? String,
      let operation = commit["operation"] as? String
    else { return }

    let now = Date()
    let startedAt = now
    let eventTime = Date(timeIntervalSince1970: Double(cursor) / 1_000_000)
    try await operationsStore?.markStreamReceived(
      source: "jetstream",
      cursor: cursor,
      eventAt: eventTime,
      receivedAt: now,
      queueDepth: 0
    )

    let cid = (commit["cid"] as? String) ?? ""
    let recordObject = commit["record"] ?? [:]
    let recordJSON = (try? JSONSerialization.data(withJSONObject: recordObject)) ?? Data("{}".utf8)

    do {
      try await indexer.handleCommit(
        repoDid: did,
        collection: collection,
        rkey: rkey,
        cid: cid,
        recordJSON: recordJSON,
        operation: operation,
        ingestionSource: "jetstream",
        cursor: String(cursor),
        eventTime: eventTime
      )
      try await operationsStore?.markStreamCommitted(
        source: "jetstream",
        cursor: cursor,
        eventAt: eventTime,
        committedAt: Date(),
        queueDepth: 0
      )
      try await resolveRecoveredGaps(through: cursor, at: Date())
      let duration = Date().timeIntervalSince(startedAt)
      await emitMetric("socialwire.ingestion.events_total", value: 1, dimensions: ["collection": collection, "operation": operation, "ingestion_mode": "live"])
      await emitMetric("socialwire.ingestion.results_total", value: 1, dimensions: ["collection": collection, "operation": operation, "result": "success", "ingestion_mode": "live"])
      await emitMetric("socialwire.ingestion.commit_lag_seconds", value: Date().timeIntervalSince(eventTime), dimensions: ["collection": collection, "ingestion_mode": "live"])
      await emitMetric("socialwire.ingestion.db_write_duration_seconds", value: duration, dimensions: ["collection": collection, "operation": operation, "ingestion_mode": "live"])
      await emitEvent("commit.committed", attributes: ["collection": collection, "operation": operation])
      if Int.random(in: 0..<100) < 5 { await emitSpan(startedAt: startedAt, duration: duration, status: "ok", collection: collection, operation: operation) }
    } catch {
      try? await operationsStore?.recordRecoveryFailure(
        jobId: nil,
        identityHash: OperationsRedactor.hashIdentity("(did)/(collection)/(rkey)"),
        collection: collection,
        operation: operation,
        cursor: cursor,
        errorCategory: OperationsRedactor.errorCategory(error),
        at: Date()
      )
      let duration = Date().timeIntervalSince(startedAt)
      await emitMetric("socialwire.ingestion.results_total", value: 1, dimensions: ["collection": collection, "operation": operation, "result": "error", "error_type": OperationsRedactor.errorCategory(error), "ingestion_mode": "live"])
      await emitEvent("commit.failed", attributes: ["collection": collection, "operation": operation, "error_type": OperationsRedactor.errorCategory(error)])
      await emitSpan(startedAt: startedAt, duration: duration, status: "error", collection: collection, operation: operation)
      await recordCommitFailureGap(collection: collection, cursor: cursor, at: Date())
      throw error
    }
  }

  private func emitMetric(_ name: String, value: Double, dimensions: [String: String]) async {
    var bounded = dimensions
    if let collection = bounded["collection"] { bounded["collection"] = Self.collectionDimension(collection) }
    _ = await telemetry?.enqueue(.metric(.init(name: name, value: value, dimensions: bounded)))
  }

  private func emitEvent(_ name: String, attributes: [String: String] = [:]) async {
    var bounded = attributes
    if let collection = bounded["collection"] { bounded["collection"] = Self.collectionDimension(collection) }
    _ = await telemetry?.enqueue(.event(.init(service: "appview-worker", environment: environment, instanceId: instanceId, name: name, attributes: bounded)))
  }

  private func emitSpan(startedAt: Date, duration: TimeInterval, status: String, collection: String, operation: String) async {
    let trace = TraceContext(sampled: true)
    _ = await telemetry?.enqueue(.span(.init(
      traceId: trace.traceId,
      service: "appview-worker",
      name: "worker.index.commit",
      startedAt: startedAt,
      durationMs: duration * 1_000,
      status: status,
      attributes: ["collection": Self.collectionDimension(collection), "operation": operation, "ingestion_mode": "live"],
      expiresAt: startedAt.addingTimeInterval(status == "error" ? 30 * 86_400 : 7 * 86_400)
    )))
  }

  private static func collectionDimension(_ value: String) -> String {
    let allowlist: Set<String> = [
      "site.standard.document", "site.standard.entry", "site.standard.publication",
      "app.skyreader.feed.subscription", "app.thesocialwire.entryReadState",
    ]
    return allowlist.contains(value) ? value : "other"
  }

  private func recordDisconnectGap(reason: Error, at: Date) async {
    guard let operationsStore else { return }
    guard let state = try? await operationsStore.fetchStreamState(source: "jetstream") else { return }
    guard let candidate = JetstreamGapDetector.candidate(state: state, reason: reason) else { return }
    await recordGap(candidate, collections: [], at: at)
  }

  private func recordCommitFailureGap(collection: String, cursor: Int64, at: Date) async {
    guard let operationsStore,
      let state = try? await operationsStore.fetchStreamState(source: "jetstream"),
      let committed = state.lastCommittedCursor,
      cursor > committed
    else { return }
    await recordGap(
      JetstreamGapCandidate(
        startCursor: committed,
        endCursor: cursor,
        reason: "commit_indexing_failure"
      ),
      collections: [collection],
      at: at
    )
  }

  private func recordGap(
    _ candidate: JetstreamGapCandidate,
    collections: [String],
    at: Date
  ) async {
    guard let operationsStore else { return }
    let existingGaps = (try? await operationsStore.listGaps(limit: 250)) ?? []
    guard !existingGaps.contains(where: { candidate.isCovered(by: $0) }) else { return }
    guard let gap = try? await operationsStore.createGap(
      source: "jetstream",
      startCursor: candidate.startCursor,
      endCursor: candidate.endCursor,
      reason: candidate.reason,
      collections: collections,
      detectedAt: at
    ) else { return }
    suspectedGapEndCursor = max(suspectedGapEndCursor ?? gap.endCursor ?? 0, gap.endCursor ?? 0)
  }

  private func resolveRecoveredGaps(through cursor: Int64, at: Date) async throws {
    guard let target = suspectedGapEndCursor, cursor >= target, let operationsStore else { return }
    let resolvedIds = try await operationsStore.resolveSuspectedGaps(
      source: "jetstream",
      through: cursor,
      at: at
    )
    for id in resolvedIds {
      try await operationsStore.recordAudit(
        operatorDid: "system:worker",
        action: "gap.auto_resolved",
        targetType: "gap",
        targetId: id,
        note: "Live ingestion committed through the suspected cursor range.",
        at: at
      )
    }
    suspectedGapEndCursor = nil
  }
}

struct JetstreamGapCandidate: Equatable, Sendable {
  let startCursor: Int64
  let endCursor: Int64
  let reason: String

  func isCovered(by gap: IngestionGap) -> Bool {
    guard gap.source == "jetstream",
      [.suspected, .confirmed, .backfillQueued, .backfilling].contains(gap.status),
      let existingStart = gap.startCursor,
      let existingEnd = gap.endCursor
    else { return false }
    return existingStart <= startCursor && existingEnd >= endCursor
  }
}

enum JetstreamGapDetector {
  static func candidate(state: IngestionStreamState, reason: Error) -> JetstreamGapCandidate? {
    guard let committed = state.lastCommittedCursor,
      let received = state.lastReceivedCursor,
      received > committed
    else { return nil }
    return JetstreamGapCandidate(
      startCursor: committed,
      endCursor: received,
      reason: reason is FirehoseQueueOverflowError ? "message_pump_overflow" : "receive_commit_backlog"
    )
  }

  static func postReconnectCandidate(state: IngestionStreamState) -> JetstreamGapCandidate? {
    guard let committed = state.lastCommittedCursor,
      let received = state.lastReceivedCursor,
      received > committed
    else { return nil }
    return JetstreamGapCandidate(
      startCursor: committed,
      endCursor: received,
      reason: "operator_reconnect_receive_commit_backlog"
    )
  }
}

public enum JetstreamCursor {
  public static func parse(_ value: Any?) -> Int64? {
    switch value {
    case let raw as Int64: return raw
    case let raw as Int: return Int64(raw)
    case let raw as NSNumber: return raw.int64Value
    case let raw as String: return Int64(raw)
    default: return nil
    }
  }

  public static func resumeCursor(
    committed: Int64?,
    seededReceived: Int64?,
    rewindMicroseconds: Int64
  ) -> Int64? {
    if let committed { return max(0, committed - rewindMicroseconds) }
    if let seededReceived { return max(0, seededReceived - 30_000_000) }
    return nil
  }

  public static func url(_ raw: String, cursor: Int64?) throws -> String {
    guard var components = URLComponents(string: raw) else { throw FirehoseSubscriberError.invalidURL }
    if let cursor {
      var items = components.queryItems ?? []
      items.removeAll { $0.name == "cursor" }
      items.append(URLQueryItem(name: "cursor", value: String(cursor)))
      components.queryItems = items
    }
    guard let url = components.url?.absoluteString else { throw FirehoseSubscriberError.invalidURL }
    return url
  }
}

enum FirehoseSubscriberError: Error, CustomStringConvertible {
  case invalidURL

  var description: String { "Invalid firehose WebSocket URL" }
}

struct FirehoseQueueOverflowError: Error {}
struct FirehoseConnectionClosedError: Error {}

private enum FirehoseCycleResult: Sendable {
  case streamEnded
  case reconnect(OperationsWorkerCommand)
}

private struct ManualJetstreamReconnectRequest: Error, Sendable {
  let command: OperationsWorkerCommand
}
