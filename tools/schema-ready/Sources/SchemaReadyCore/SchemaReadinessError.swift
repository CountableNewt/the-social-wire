public enum SchemaReadinessError: Error, Equatable, Sendable {
  case invalidConfiguration(String)
  case timeout

  /// Fixed diagnostics never interpolate database URLs, credentials or underlying errors.
  public static func startupDiagnostic(for error: any Error) -> String {
    let reason: String
    switch error {
    case Self.invalidConfiguration: reason = "invalid or unsupported configuration"
    case Self.timeout:
      reason = "database connection or required migration receipts unavailable before deadline"
    case is CancellationError: reason = "cancelled"
    default: reason = "unexpected failure"
    }
    return "Schema readiness prevented startup: " + reason + "\n"
  }
}
