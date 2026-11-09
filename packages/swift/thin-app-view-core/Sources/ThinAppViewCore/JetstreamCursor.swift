import Foundation

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
