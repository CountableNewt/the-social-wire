import Foundation
import Logging

/// Consumes Jetstream / relay WebSocket commits and forwards them to the indexer.
actor FirehoseSubscriber {
  private let relayURL: String
  private let indexer: ThinAppViewIndexer
  private let logger: Logger

  init(
    relayURL: String,
    indexer: ThinAppViewIndexer,
    logger: Logger
  ) {
    self.relayURL = relayURL
    self.indexer = indexer
    self.logger = logger
  }

  func runForever() async {
    while !Task.isCancelled {
      do {
        try await consumeOnce()
      } catch {
        logger.warning("Firehose disconnected; reconnecting", metadata: ["error": .string("\(error)")])
        try? await Task.sleep(for: .seconds(3))
      }
    }
  }

  private func consumeOnce() async throws {
    #if canImport(WebSocketKit)
    try await FirehoseLinuxWebSocket.consume(relayURL: relayURL, logger: logger) { text in
      try await self.handleMessage(text)
    }
    #else
    try await FirehoseSubscriberURLSessionTransport.consume(
      relayURL: relayURL,
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
      let json = try JSONSerialization.jsonObject(with: data) as? [String: Any]
    else { return }

    guard (json["kind"] as? String) == "commit" else { return }
    guard
      let did = json["did"] as? String,
      let commit = json["commit"] as? [String: Any],
      let collection = commit["collection"] as? String,
      let rkey = commit["rkey"] as? String,
      let operation = commit["operation"] as? String
    else { return }

    let cid = (commit["cid"] as? String) ?? ""
    let recordObject = commit["record"] ?? [:]
    let recordJSON = (try? JSONSerialization.data(withJSONObject: recordObject)) ?? Data("{}".utf8)
    let eventTime = Self.eventTime(from: json)
    let cursor = (commit["rev"] as? String)
      ?? Self.stringValue(json["time_us"])
      ?? Self.stringValue(json["seq"])

    try await indexer.handleCommit(
      repoDid: did,
      collection: collection,
      rkey: rkey,
      cid: cid,
      recordJSON: recordJSON,
      operation: operation,
      ingestionSource: "jetstream",
      cursor: cursor,
      eventTime: eventTime
    )
  }

  private static func eventTime(from json: [String: Any]) -> Date? {
    if let raw = json["time"] as? String {
      let fractional = ISO8601DateFormatter()
      fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
      if let date = fractional.date(from: raw) { return date }
      if let date = ISO8601DateFormatter().date(from: raw) { return date }
    }
    if let number = json["time_us"] as? NSNumber {
      return Date(timeIntervalSince1970: number.doubleValue / 1_000_000)
    }
    if let int = json["time_us"] as? Int {
      return Date(timeIntervalSince1970: Double(int) / 1_000_000)
    }
    return nil
  }

  private static func stringValue(_ value: Any?) -> String? {
    switch value {
    case let raw as String:
      return raw
    case let raw as NSNumber:
      return raw.stringValue
    case let raw as Int:
      return String(raw)
    default:
      return nil
    }
  }
}

enum FirehoseSubscriberError: Error, CustomStringConvertible {
  case invalidURL

  var description: String {
    switch self {
    case .invalidURL: "Invalid firehose WebSocket URL"
    }
  }
}
