import Testing
@testable import Gateway

@Test func invalidHostedConfigurationReturnsErrorsInsteadOfCrashingOrSelectingLocalMode() {
  #expect(throws: (any Error).self) { try GatewayServiceConfig.fromEnvironment(["APP_ENV": "unknown"]) }
  #expect(throws: (any Error).self) {
    try GatewayServiceConfig.fromEnvironment(["APP_ENV": "prod", "PDS_ATTESTATION_RECEIPT_SECRET": String(repeating: "s", count: 32)])
  }
  for port in ["0", "65536", "invalid", "-1"] {
    #expect(throws: (any Error).self) { try GatewayServiceConfig.fromEnvironment(["PORT": port]) }
  }
  for url in ["https://user:secret@appview.example", "ftp://appview.example", "https://appview.example?key=value", "https://appview.example#fragment"] {
    #expect(throws: (any Error).self) {
      try GatewayServiceConfig.fromEnvironment(["APPVIEW_BASE_URL": url])
    }
  }
}
