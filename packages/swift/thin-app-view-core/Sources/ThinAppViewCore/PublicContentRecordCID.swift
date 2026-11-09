import CoreFoundation
import Foundation

/// Content-address verification for public ATProto records, bounded separately
/// from the smaller read-state codec. This does not establish repository authority.
enum PublicContentRecordCID {
  static func verify(json: Data, cid: String) throws {
    guard json.count <= 1 << 20 else { throw PublicContentDiscoveryError.invalidResponse }
    let expected = try RepositoryCID(cid)
    guard expected.isDAGCBOR,
      let object = try JSONSerialization.jsonObject(with: json) as? [String: Any] else {
      throw PublicContentDiscoveryError.invalidResponse
    }
    var remaining = 100_000
    let value = try convert(object, depth: 0, remaining: &remaining)
    let encoded = RepositoryDAGCBOR.encode(value)
    guard encoded.count <= 1 << 20, RepositoryCID.hash(encoded) == expected else {
      throw PublicContentDiscoveryError.invalidResponse
    }
  }

  private static func convert(_ value: Any, depth: Int, remaining: inout Int) throws -> RepositoryCBORValue {
    remaining -= 1
    guard depth <= 32, remaining >= 0 else { throw PublicContentDiscoveryError.invalidResponse }
    if let text = value as? String { return .string(text) }
    if let number = value as? NSNumber {
      if CFGetTypeID(number) == CFBooleanGetTypeID() { return .bool(number.boolValue) }
      let value = number.doubleValue
      guard value.isFinite, value.rounded() == value, abs(value) <= 9_007_199_254_740_991 else {
        throw PublicContentDiscoveryError.invalidResponse
      }
      let integer = Int64(value)
      return integer >= 0 ? .unsigned(UInt64(integer)) : .negative(UInt64(-1 - integer))
    }
    if value is NSNull { return .null }
    if let array = value as? [Any] {
      return .array(try array.map { try convert($0, depth: depth + 1, remaining: &remaining) })
    }
    if let object = value as? [String: Any] {
      if object.count == 1, let cid = object["$link"] as? String { return .link(try RepositoryCID(cid)) }
      if object.count == 1, let encoded = object["$bytes"] as? String {
        let padding = String(repeating: "=", count: (4 - encoded.count % 4) % 4)
        guard let bytes = Data(base64Encoded: encoded + padding) else { throw PublicContentDiscoveryError.invalidResponse }
        return .bytes(bytes)
      }
      var result: [String: RepositoryCBORValue] = [:]
      for (key, value) in object { result[key] = try convert(value, depth: depth + 1, remaining: &remaining) }
      return .map(result)
    }
    throw PublicContentDiscoveryError.invalidResponse
  }
}
