import Foundation
import Logging
import OperationsCore

/// Consumes Jetstream commits in receive order and advances the durable cursor only after indexing succeeds.
actor FirehoseSubscriber {
  private let relayURL: String
  private let indexer: ThinAppViewIndexer
  private let operationsStore: (any OperationsStore)?
  private let replayRewindMicroseconds: Int64
  private let logger: Logger

  init(
    relayURL: String,
    indexer: ThinAppViewIndexer,
    operationsStore: (any OperationsStore)?,
    replayRewindMicroseconds: Int64,
    logger: Logger
  ) {
    self.relayURL = relayURL
    self.indexer = indexer
    self.operationsStore = operationsStore
    self.replayRewindMicroseconds = replayRewindMicroseconds
    self.logger = logger
  }

  func runForever() async {
    while !Task.isCancelled {
      do {
        try await consumeOnce()
      } catch {
        let now = Date()
        try? await operationsStore?.markStreamDisconnected(
          source: "jetstream",
          reason: OperationsRedactor.errorCategory(error),
          at: now
        )
        await recordDisconnectGap(reason: error, at: now)
        logger.warning(
          "Firehose disconnected; reconnecting from durable cursor",
          metadata: ["error_type": .string(OperationsRedactor.errorCategory(error))]
        )
        try? await Task.sleep(for: .seconds(3))
      }
    }
  }

  private func consumeOnce() async throws {
    let streamState = try await operationsStore?.fetchStreamState(source: "jetstream")
    let cursor = JetstreamCursor.resumeCursor(
      committed: streamState?.lastCommittedCursor,
      seededReceived: streamState?.lastReceivedCursor,
      rewindMicroseconds: replayRewindMicroseconds
    )
    let url = try JetstreamCursor.url(relayURL, cursor: cursor)
    try await operationsStore?.markStreamConnected(source: "jetstream", at: Date())

    #if canImport(WebSocketKit)
    try await FirehoseLinuxWebSocket.consume(relayURL: url, logger: logger) { text in
      try await self.handleMessage(text)
    }
    #else
    try await FirehoseSubscriberURLSessionTransport.consume(
      relayURL: url,
      logger: logger,
      isCancelled: { Task.isCancelled }
    ) { text in
      try await self.handleMessage(text)
    }
    #endif
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
      throw error
    }
  }

  private func recordDisconnectGap(reason: Error, at: Date) async {
    guard let operationsStore else { return }
    guard let state = try? await operationsStore.fetchStreamState(source: "jetstream") else { return }
    guard state.lastReceivedCursor != state.lastCommittedCursor || reason is FirehoseQueueOverflowError else { return }
    _ = try? await operationsStore.createGap(
      source: "jetstream",
      startCursor: state.lastCommittedCursor,
      endCursor: state.lastReceivedCursor,
      reason: reason is FirehoseQueueOverflowError ? "message_pump_overflow" : "receive_commit_backlog",
      collections: [],
      detectedAt: at
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
