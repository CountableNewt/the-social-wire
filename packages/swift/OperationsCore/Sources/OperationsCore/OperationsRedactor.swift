import Crypto
import Foundation

public enum OperationsRedactor {
  private static let prohibitedKeys = [
    "authorization", "dpop", "cookie", "token", "secret", "password", "record", "body",
  ]

  public static func boundedAttributes(_ input: [String: String], maximumValueLength: Int = 256) -> [String: String] {
    var result: [String: String] = [:]
    for (key, value) in input.prefix(32) {
      let lower = key.lowercased()
      guard !prohibitedKeys.contains(where: { lower.contains($0) }) else { continue }
      result[key] = String(value.prefix(maximumValueLength))
    }
    return result
  }

  public static func recordIdentifierHash(_ value: String) -> String {
    let digest = SHA256.hash(data: Data(value.utf8))
    return digest.prefix(12).map { String(format: "%02x", $0) }.joined()
  }
}
