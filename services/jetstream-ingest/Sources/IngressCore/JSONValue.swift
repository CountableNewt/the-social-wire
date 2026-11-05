import Foundation

public indirect enum JSONValue: Codable, Sendable, Equatable {
  case object([String: JSONValue])
  case array([JSONValue])
  case string(String)
  case integer(Int64)
  case unsigned(UInt64)
  case double(Double)
  case bool(Bool)
  case null

  public init(from decoder: any Decoder) throws {
    let value = try decoder.singleValueContainer()
    if value.decodeNil() {
      self = .null
    } else if let item = try? value.decode(Bool.self) {
      self = .bool(item)
    } else if let item = try? value.decode(Int64.self) {
      self = .integer(item)
    } else if let item = try? value.decode(UInt64.self) {
      self = .unsigned(item)
    } else if let item = try? value.decode(Double.self) {
      self = .double(item)
    } else if let item = try? value.decode(String.self) {
      self = .string(item)
    } else if let item = try? value.decode([String: JSONValue].self) {
      self = .object(item)
    } else {
      self = .array(try value.decode([JSONValue].self))
    }
  }
  public func encode(to encoder: any Encoder) throws {
    var target = encoder.singleValueContainer()
    switch self {
    case .object(let value): try target.encode(value)
    case .array(let value): try target.encode(value)
    case .string(let value): try target.encode(value)
    case .integer(let value): try target.encode(value)
    case .unsigned(let value): try target.encode(value)
    case .double(let value): try target.encode(value)
    case .bool(let value): try target.encode(value)
    case .null: try target.encodeNil()
    }
  }
  public subscript(_ key: String) -> JSONValue? {
    if case .object(let value) = self { return value[key] }
    return nil
  }
  public var string: String? {
    if case .string(let value) = self { return value }
    return nil
  }
  public var integer: Int64? {
    if case .integer(let value) = self { return value }
    return nil
  }
  public func normalizedForPostgres() throws -> JSONValue {
    switch self {
    case .string(let value): return .string(value.replacingOccurrences(of: "\0", with: "\u{fffd}"))
    case .array(let value): return .array(try value.map { try $0.normalizedForPostgres() })
    case .object(let value):
      var output: [String: JSONValue] = [:]
      for (key, item) in value {
        let normalized = key.replacingOccurrences(of: "\0", with: "\u{fffd}")
        guard output[normalized] == nil else {
          throw IngressError.invalidEvent("Normalizing JSON object key creates a duplicate")
        }
        output[normalized] = try item.normalizedForPostgres()
      }
      return .object(output)
    default: return self
    }
  }
}
