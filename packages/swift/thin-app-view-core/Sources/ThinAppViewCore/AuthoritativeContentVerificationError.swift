public enum AuthoritativeContentVerificationError: Error, Sendable, Equatable {
  case invalidScope
  case invalidResponse
  case unavailable
  case capacityExceeded
}
