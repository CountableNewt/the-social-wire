import Foundation

public enum DeterministicKeys {
  public static let preferencesRKey = "self"

  public static func generateTID() -> String {
    let ms = UInt64(Date().timeIntervalSince1970 * 1000)
    return String(ms, radix: 32).lowercased()
  }

}
