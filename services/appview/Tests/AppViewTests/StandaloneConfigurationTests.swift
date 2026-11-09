import Testing

@testable import AppView

@Suite struct StandaloneConfigurationTests {
  private func environment(_ appEnvironment: String = "dev") -> [String: String] {
    [
      "APP_ENV": appEnvironment, "BACKEND_MODE": "standalone",
      "SQLITE_DB_PATH": "/data/reader.sqlite",
      "PDS_ATTESTATION_RECEIPT_SECRET": "standalone-test-attestation-secret-at-least-32-bytes",
    ]
  }

  @Test(arguments: ["dev", "prod"])
  func hostedSQLiteKeepsEnvironmentIdentity(value: String) throws {
    let config = try AppViewServiceConfig.fromEnvironment(environment(value))
    #expect(config.core.appEnv.rawValue == value)
    guard case .sqlite(let path) = config.storeBackend else {
      Issue.record("Standalone backend must use SQLite without DATABASE_URL")
      return
    }
    #expect(path == "/data/reader.sqlite")
    #expect(config.standaloneEdge != nil)
  }

  @Test(arguments: ["", "reader.sqlite"])
  func hostedSQLiteRequiresExplicitPersistentPath(value: String) {
    var env = environment()
    env["SQLITE_DB_PATH"] = value
    #expect(throws: StandaloneEdgeConfiguration.Failure.missingPersistentSQLitePath) {
      try AppViewServiceConfig.fromEnvironment(env)
    }
  }

  @Test func neverUsesLocalAuthenticationDefaultsOnHostedSQLite() {
    var env = environment()
    env.removeValue(forKey: "PDS_ATTESTATION_RECEIPT_SECRET")
    #expect(throws: StandaloneEdgeConfiguration.Failure.missingAttestationSecret) {
      try AppViewServiceConfig.fromEnvironment(env)
    }
    env["PDS_ATTESTATION_RECEIPT_SECRET"] = "short"
    #expect(throws: StandaloneEdgeConfiguration.Failure.invalidAttestationSecret) {
      try AppViewServiceConfig.fromEnvironment(env)
    }
  }

  @Test func rejectsAccidentalGlobalIngestion() {
    var env = environment()
    env["GLOBAL_INGESTION_ENABLED"] = "true"
    #expect(throws: StandaloneEdgeConfiguration.Failure.retiredIngestionEnabled) {
      try AppViewServiceConfig.fromEnvironment(env)
    }
  }

  @Test func rejectsUnknownBackendModes() {
    var env = environment()
    env["BACKEND_MODE"] = "standalon"
    #expect(throws: StandaloneEdgeConfiguration.Failure.invalidMode) {
      try AppViewServiceConfig.fromEnvironment(env)
    }
  }
}
