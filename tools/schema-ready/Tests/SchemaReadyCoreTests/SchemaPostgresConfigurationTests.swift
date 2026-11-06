import Foundation
import NIOSSL
import Testing

@testable import SchemaReadyCore

struct SchemaPostgresConfigurationTests {
  @Test func certificateAuthenticationIsIndependentOfEncryptionMode() throws {
    for mode in ["disable", "prefer", "allow", "require"] {
      #expect(
        try SchemaPostgresConfiguration.tlsConfiguration(mode: mode).certificateVerification
          == .none)
    }
    #expect(
      try SchemaPostgresConfiguration.tlsConfiguration(mode: "verify-ca").certificateVerification
        == .noHostnameVerification)
    #expect(
      try SchemaPostgresConfiguration.tlsConfiguration(mode: "verify-full").certificateVerification
        == .fullVerification)
  }

  @Test(arguments: [
    "sslrootcert", "sslcert", "sslkey", "sslpassword", "sslcrl", "sslcrldir",
    "sslmode=require&sslmode",
  ])
  func unsupportedExplicitTLSSettingsFailClosed(parameter: String) {
    #expect(throws: SchemaReadinessError.self) {
      try SchemaPostgresConfiguration.make(
        url: "postgres://viewer@localhost/database?sslmode=require&\(parameter)=secret")
    }
  }

  @Test func diagnosticsNeverDiscloseUnderlyingValues() {
    let secret = "postgres://viewer:secret@database/private"
    for error: any Error in [
      SchemaReadinessError.invalidConfiguration(secret), SchemaReadinessError.timeout,
      NSError(domain: secret, code: 1), CancellationError(),
    ] {
      let message = SchemaReadinessError.startupDiagnostic(for: error)
      #expect(message.hasPrefix("Schema readiness prevented startup:"))
      #expect(!message.contains("secret") && !message.contains("postgres://"))
    }
  }
}
