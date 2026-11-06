import Foundation

enum PublicContentDiscoveryMode: String, Sendable {
  case microcosm
  case off

  static func load(_ env: [String: String]) throws -> Self {
    let raw = (env["PUBLIC_CONTENT_DISCOVERY_MODE"] ?? "microcosm").trimmingCharacters(in: .whitespacesAndNewlines)
    guard let mode = Self(rawValue: raw) else { throw AppViewServiceConfigError.invalidPublicContentDiscoveryMode }
    return mode
  }
}
