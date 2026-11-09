public enum PublicContentDiscoveryError: Error, Equatable, Sendable {
  case invalidScope
  case invalidResponse
  case unavailable
}
