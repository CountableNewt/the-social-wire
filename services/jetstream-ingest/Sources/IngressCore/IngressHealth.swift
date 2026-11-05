import Foundation

public actor IngressHealth {
  public struct Snapshot: Codable, Sendable {
    public var database = false
    public var lease = false
    public var stream = false
    public var paused = false
    public var lastSeq: Int64 = 0
    public var lastProgressAt: Date?
    public var replayState = "starting"
    public var replaySealedSeq: Int64 = 0
    public var replayBytesDownloaded: Int64 = 0
    public var lastError: String?
    public var ready: Bool { database && lease && stream && !paused }
  }
  private var value = Snapshot()
  public init() {}
  public func snapshot() -> Snapshot { value }
  public func database(_ active: Bool) { value.database = active }
  public func lease(_ active: Bool) { value.lease = active }
  public func stream(_ active: Bool) { value.stream = active }
  public func paused(_ active: Bool) { value.paused = active }
  public func failed(_ message: String) { value.lastError = String(message.prefix(512)) }
  public func progress(sequence: Int64, replay: ReplayProgress) {
    if sequence > value.lastSeq {
      value.lastSeq = sequence
      value.lastProgressAt = Date()
    }
    value.replayState = replay.state
    value.replaySealedSeq = replay.sealed
    value.replayBytesDownloaded = replay.bytes
  }
}

extension IngressHealth {
  public func document(controller: Bool) -> JSONValue {
    let snapshot = value
    var lane: [String: JSONValue] = [
      "ready": .bool(snapshot.ready), "database": .bool(snapshot.database),
      "lease": .bool(snapshot.lease), "stream": .bool(snapshot.stream),
      "paused": .bool(snapshot.paused), "backpressured": .bool(false),
      "snapshotComplete": .bool(false), "lastSeq": .integer(snapshot.lastSeq),
      "admissionRateLimited": .bool(false),
    ]
    if let error = snapshot.lastError { lane["lastError"] = .string(error) }
    if let time = snapshot.lastProgressAt {
      lane["lastProgress"] = .string(ISO8601DateFormatter().string(from: time))
    }
    guard controller else { return .object(lane) }
    return .object([
      "ready": .bool(snapshot.ready), "started": .bool(snapshot.database),
      "lanes": .object(["appview": .object(lane)]),
    ])
  }
}
