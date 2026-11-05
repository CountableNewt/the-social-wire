import Foundation

public struct IngressEvent: Sendable {
  public let sequence: Int64
  public let timeUS: Int64
  public let kind: String
  public let did: String
  public let payload: JSONValue
  public var eventTime: Date { Date(timeIntervalSince1970: Double(timeUS) / 1_000_000) }
  public var collection: String? { payload["commit"]?["collection"]?.string }
  public var operation: String? { payload["commit"]?["operation"]?.string }
  public var revision: String? {
    payload["commit"]?["rev"]?.string ?? payload["sync"]?["rev"]?.string
  }
  public var recordKey: String? { payload["commit"]?["rkey"]?.string }
  public var recordCID: String? { payload["commit"]?["cid"]?.string }

  public init(payload: JSONValue) throws {
    guard let sequence = payload["cursor"]?.integer, sequence > 0,
      let timeUS = payload["time_us"]?.integer,
      let kind = payload["kind"]?.string,
      let did = payload["did"]?.string, !did.isEmpty
    else {
      throw IngressError.invalidEvent("Event is missing identity or its cursor is outside BIGINT")
    }
    guard ["commit", "identity", "account", "sync"].contains(kind), payload[kind] != nil,
      ["commit", "identity", "account", "sync"].filter({
        payload[$0] != nil && payload[$0] != .null
      }).count == 1
    else { throw IngressError.invalidEvent("Event must have exactly one matching payload") }
    self.sequence = sequence
    self.timeUS = timeUS
    self.kind = kind
    self.did = did
    if case .object(var object) = payload, case .object(var commit) = object["commit"] {
      commit.removeValue(forKey: "record_cbor")
      object["commit"] = .object(commit)
      self.payload = .object(object)
    } else {
      self.payload = payload
    }
    if kind == "commit" {
      guard let operation, ["create", "update", "delete"].contains(operation),
        !(collection ?? "").isEmpty, !(recordKey ?? "").isEmpty, !(revision ?? "").isEmpty
      else { throw IngressError.invalidEvent("Commit is missing identity fields") }
      if operation != "delete" {
        guard case .object = self.payload["commit"]?["record"] else {
          throw IngressError.invalidEvent("Commit has no projectable record")
        }
      }
    }
    if kind == "sync" && (revision ?? "").isEmpty {
      throw IngressError.invalidEvent("Sync has no revision")
    }
  }

  public func encodedPayload(normalize: Bool = false) throws -> Data {
    let value = normalize ? try payload.normalizedForPostgres() : payload
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    let data = try encoder.encode(value)
    guard data.count <= 32 << 20 else { throw IngressError.invalidEvent("Event exceeds 32 MiB") }
    return data
  }

  public static func decodeLive(_ data: Data) throws -> IngressEvent? {
    guard data.count <= 32 << 20 else {
      throw IngressError.invalidEvent("Live frame exceeds 32 MiB")
    }
    let frame = try JSONDecoder().decode(JSONValue.self, from: data)
    guard let type = frame["$type"]?.string else {
      throw IngressError.invalidEvent("Missing live envelope type")
    }
    if type == "error" {
      let code = frame["error"]?.string ?? "Unknown provider error"
      if code == "FutureCursor" { throw IngressError.sourceDiscontinuity }
      if code == "CursorTooOld" { throw IngressError.cursorTooOld }
      throw IngressError.provider(String(code.prefix(128)))
    }
    guard type == "message" else { return nil }
    guard case .object(let body) = frame["payload"], let type = body["$type"]?.string else {
      throw IngressError.invalidEvent("Missing live payload")
    }
    let messagePrefix = "network.bsky.jetstream.subscribeEvents#"
    guard type.hasPrefix(messagePrefix) else { return nil }
    let kind = String(type.dropFirst(messagePrefix.count))
    guard ["commit", "identity", "account", "sync"].contains(kind) else { return nil }
    guard let seq = body["seq"]?.integer, let did = body["did"]?.string,
      let timestamp = body["time"]?.string, let timeUS = parseTimeUS(timestamp)
    else { throw IngressError.invalidEvent("Invalid live identity or time") }
    let payload: JSONValue
    if kind == "commit" {
      var commit = body.filter {
        ["operation", "collection", "rkey", "rev", "cid", "record"].contains($0.key)
      }
      if commit["operation"]?.string != "delete" {
        guard case .object(let record) = commit["record"], record["$bytes"] == nil,
          record["$link"] == nil
        else { throw IngressError.invalidEvent("Live record must be a DAG-CBOR map") }
        commit["record"] = try ATProtoRecordJSON.normalize(.object(record))
      } else {
        commit.removeValue(forKey: "record")
      }
      payload = .object(commit)
    } else {
      guard let nested = body[kind], !(nested["did"]?.string ?? "").isEmpty else {
        throw IngressError.invalidEvent("Missing lifecycle payload")
      }
      payload = lifecyclePayload(nested, kind: kind)
    }
    return try IngressEvent(
      payload: .object([
        "cursor": .integer(seq), "time_us": .integer(timeUS), "did": .string(did),
        "kind": .string(kind), kind: payload,
      ]))
  }

  static func lifecyclePayload(_ payload: JSONValue, kind: String) -> JSONValue {
    guard case .object(let body) = payload else { return payload }
    let fields =
      kind == "identity"
      ? ["did", "handle", "seq", "time"]
      : kind == "account"
        ? ["did", "active", "status", "seq", "time"] : ["did", "rev", "seq", "time"]
    return .object(body.filter { fields.contains($0.key) })
  }
  static func parseTimeUS(_ value: String) -> Int64? {
    var secondsText = value
    var micros: Int64 = 0
    if let dot = value.firstIndex(of: ".") {
      let start = value.index(after: dot)
      let digits = value[start...].prefix(while: { $0.isASCII && $0.isNumber })
      guard !digits.isEmpty, digits.count <= 9 else { return nil }
      micros =
        Int64(String(digits.prefix(6)).padding(toLength: 6, withPad: "0", startingAt: 0)) ?? 0
      let end = value.index(start, offsetBy: digits.count)
      secondsText = String(value[..<dot]) + value[end...]
    }
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime]
    guard let date = formatter.date(from: secondsText),
      let seconds = Int64(exactly: date.timeIntervalSince1970)
    else { return nil }
    let (scaled, overflow) = seconds.multipliedReportingOverflow(by: 1_000_000)
    guard !overflow else { return nil }
    let (result, additionOverflow) = scaled.addingReportingOverflow(micros)
    return additionOverflow ? nil : result
  }
}
