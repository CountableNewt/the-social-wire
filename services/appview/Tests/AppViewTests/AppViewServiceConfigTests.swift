import Testing
@testable import AppView

@Suite struct AppViewServiceConfigTests {
  @Test(arguments: ["development", "production", "DEV", "unknown"])
  func rejectsUnknownEnvironment(value: String) {
    #expect(throws: AppViewServiceConfigError.invalidEnvironment) {
      try AppViewServiceConfig.fromEnvironment(["APP_ENV": value])
    }
  }

  @Test(arguments: ["dev", "prod"])
  func missingHostedDatabaseThrows(value: String) {
    #expect(throws: AppViewServiceConfigError.missingDatabaseURL) {
      try AppViewServiceConfig.fromEnvironment(["APP_ENV": value, "DATABASE_URL": "  "])
    }
  }

  @Test(arguments: ["zero", "0", "-1", "65536"])
  func rejectsInvalidPort(value: String) {
    #expect(throws: AppViewServiceConfigError.invalidPort) {
      try AppViewServiceConfig.fromEnvironment(["PORT": value])
    }
  }

  @Test(arguments: ["ATPROTO_PLC_URL", "SEMBLE_PUBLIC_API_BASE_URL"])
  func rejectsCredentialedOrMalformedServiceURLs(key: String) {
    for url in ["ftp://example.test", "http://user:password@example.test", "https://example.test?token=secret", "not a url"] {
      #expect(throws: AppViewServiceConfigError.invalidServiceURL) {
        try AppViewServiceConfig.fromEnvironment([key: url])
      }
    }
  }

  @Test func normalizesEnvironmentAndKeepsToolsFlagExplicit() throws {
    #expect(try AppViewServiceConfig.fromEnvironment(["APP_ENV": " local "]).core.appEnv == .local)
    #expect(try AppViewServiceConfig.fromEnvironment([:]).toolsEnabled)
    #expect(try AppViewServiceConfig.fromEnvironment(["TOOLS_ENABLED": "false"]).toolsEnabled == false)
  }
}
