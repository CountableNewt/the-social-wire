import Foundation

/// Ordered JSON decoding for the small Go request DTO. Foundation's duplicate
/// object-key policy differs by platform; Go's struct/map/slice semantics must
/// be applied before converting embed objects into ordinary dictionaries.
enum MediaProjectJSON {
  static func request(_ data: Data) throws -> (
    mode: String, posts: [(uri: String, embed: [String: Any])]
  ) {
    var parser = Parser(bytes: Array(data))
    let value = try parser.value(depth: 0)
    parser.whitespace()
    guard parser.position == parser.bytes.count, case .object(let fields) = value else {
      throw MediaProjectError.invalidCandidates
    }
    var mode = ""
    var storage: [Candidate] = []
    var count: Int?
    for (name, value) in fields {
      switch field(name) {
      case "mode":
        switch value {
        case .string(let text): mode = text
        case .null: break  // null does not overwrite an existing Go string.
        default: throw MediaProjectError.invalidCandidates
        }
      case "posts":
        switch value {
        case .null:
          storage = []
          count = nil
        case .array(let values):
          guard values.count <= 30 else { throw MediaProjectError.invalidCandidates }
          if values.isEmpty { storage = [] }
          for (index, value) in values.enumerated() {
            if index == storage.count { storage.append(Candidate()) }
            try storage[index].decode(value)
          }
          // Go reuses slice elements, including retained capacity after a
          // shorter duplicate array. An explicit empty array resets it.
          count = values.count
        default: throw MediaProjectError.invalidCandidates
        }
      default: throw MediaProjectError.invalidCandidates
      }
    }
    guard let count else { throw MediaProjectError.invalidCandidates }
    return (mode, storage.prefix(count).map { ($0.uri, $0.embed ?? [:]) })
  }

  private static func field(_ name: String) -> String {
    // Long-s is the additional Unicode SimpleFold alias for these ASCII keys.
    name.replacingOccurrences(of: "ſ", with: "s").lowercased()
  }

  private struct Candidate {
    var uri = ""
    var embed: [String: Any]?
    mutating func decode(_ value: Value) throws {
      if case .null = value { return }
      guard case .object(let fields) = value else { throw MediaProjectError.invalidCandidates }
      for (name, value) in fields {
        switch field(name) {
        case "uri":
          switch value {
          case .string(let text): uri = text
          case .null: break
          default: throw MediaProjectError.invalidCandidates
          }
        case "embed":
          switch value {
          case .null: embed = nil
          case .object(let members):
            if embed == nil { embed = [:] }
            for (key, value) in members { embed?[key] = value.json() }
          default: throw MediaProjectError.invalidCandidates
          }
        default: throw MediaProjectError.invalidCandidates
        }
      }
    }
  }

  private indirect enum Value {
    case object([(String, Value)]), array([Value]), string(String), number(Double), bool(Bool), null
    func json() -> Any {
      switch self {
      case .object(let fields):
        var object: [String: Any] = [:]
        for (key, value) in fields { object[key] = value.json() }
        return object
      case .array(let values): return values.map { $0.json() }
      case .string(let value): return value
      case .number(let value): return value
      case .bool(let value): return value
      case .null: return NSNull()
      }
    }
  }

  private struct Parser {
    let bytes: [UInt8]
    var position = 0
    mutating func whitespace() {
      while position < bytes.count, [9, 10, 13, 32].contains(bytes[position]) { position += 1 }
    }
    mutating func take(_ byte: UInt8) -> Bool {
      whitespace()
      guard position < bytes.count, bytes[position] == byte else { return false }
      position += 1
      return true
    }
    mutating func value(depth: Int) throws -> Value {
      whitespace()
      guard depth <= 512, position < bytes.count else { throw MediaProjectError.invalidCandidates }
      if take(123) {
        var fields: [(String, Value)] = []
        if take(125) { return .object(fields) }
        repeat {
          let key = try string()
          guard take(58) else { throw MediaProjectError.invalidCandidates }
          fields.append((key, try value(depth: depth + 1)))
          if take(125) { return .object(fields) }
        } while take(44)
        throw MediaProjectError.invalidCandidates
      }
      if take(91) {
        var values: [Value] = []
        if take(93) { return .array(values) }
        repeat {
          values.append(try value(depth: depth + 1))
          if take(93) { return .array(values) }
        } while take(44)
        throw MediaProjectError.invalidCandidates
      }
      if bytes[position] == 34 { return .string(try string()) }
      let start = position
      while position < bytes.count, ![9, 10, 13, 32, 44, 93, 125].contains(bytes[position]) {
        position += 1
      }
      let token = String(decoding: bytes[start..<position], as: UTF8.self)
      switch token {
      case "null": return .null
      case "true": return .bool(true)
      case "false": return .bool(false)
      default:
        guard
          token.range(
            of: #"^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$"#, options: .regularExpression)
            != nil,
          let number = Double(token), number.isFinite
        else { throw MediaProjectError.invalidCandidates }
        return .number(number)
      }
    }
    mutating func string() throws -> String {
      whitespace()
      let start = position
      guard take(34) else { throw MediaProjectError.invalidCandidates }
      while position < bytes.count, bytes[position] != 34 {
        position += bytes[position] == 92 ? 2 : 1
      }
      guard position < bytes.count else { throw MediaProjectError.invalidCandidates }
      position += 1
      return try JSONDecoder().decode(String.self, from: Data(bytes[start..<position]))
    }
  }
}
