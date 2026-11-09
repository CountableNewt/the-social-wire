import Foundation

indirect enum RepositoryCBORValue: Sendable, Equatable {
  case map([String: Self])
  case array([Self])
  case string(String)
  case bytes(Data)
  case link(RepositoryCID)
  case unsigned(UInt64)
  case negative(UInt64)
  case double(Double)
  case bool(Bool)
  case null

  subscript(_ key: String) -> Self? {
    if case .map(let value) = self { return value[key] }
    return nil
  }
  var text: String? {
    if case .string(let value) = self { return value }
    return nil
  }
  var data: Data? {
    if case .bytes(let value) = self { return value }
    return nil
  }
  var cid: RepositoryCID? {
    if case .link(let value) = self { return value }
    return nil
  }
  var number: UInt64? {
    if case .unsigned(let value) = self { return value }
    return nil
  }
  var values: [Self]? {
    if case .array(let value) = self { return value }
    return nil
  }

  func jsonObject() -> Any {
    switch self {
    case .map(let value): value.mapValues { $0.jsonObject() }
    case .array(let value): value.map { $0.jsonObject() }
    case .string(let value): value
    case .bytes(let value):
      ["$bytes": value.base64EncodedString().replacingOccurrences(of: "=", with: "")]
    case .link(let value): ["$link": value.string]
    case .unsigned(let value): NSNumber(value: value)
    case .negative(let value): NSNumber(value: -1 - Int64(value))
    case .double(let value): NSNumber(value: value)
    case .bool(let value): value
    case .null: NSNull()
    }
  }
}
