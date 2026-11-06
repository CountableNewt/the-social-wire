import Foundation
import PostgresNIO
import Testing

@testable import SchemaReadyCore

struct SchemaTLSIntegrationTests {
  @Test(
    .enabled(
      if: ProcessInfo.processInfo.environment["SCHEMA_READY_TLS_TEST_DOCKER"] == "1"
        || ProcessInfo.processInfo.environment["GITHUB_ACTIONS"] == "true"))
  func encryptedSelfSignedPostgresPreservesModeAndReceiptRequirements() async throws {
    let fixture = SchemaTLSDockerFixture()
    defer { fixture.stop() }
    try fixture.start()
    for mode: String? in [nil, "prefer", "allow", "require"] {
      let url = fixture.url(mode: mode)
      try await SchemaReadiness.wait(
        url: url, versions: ["20260815190000", "20261105000001"], timeout: .seconds(5))
      try await assertEncryptedReadOnlySession(url: url)
    }
    for mode in ["verify-ca", "verify-full", "disable"] {
      await #expect(throws: (any Error).self) {
        try await SchemaReadiness.wait(
          url: fixture.url(mode: mode), versions: ["20260815190000"], timeout: .seconds(2))
      }
    }
    await #expect(throws: SchemaReadinessError.timeout) {
      try await SchemaReadiness.wait(
        url: fixture.url(mode: "require"),
        versions: ["19990101000001", "20261105000001"], timeout: .seconds(2))
    }
  }

  private func assertEncryptedReadOnlySession(url: String) async throws {
    let client = PostgresClient(configuration: try SchemaPostgresConfiguration.make(url: url))
    try await withThrowingTaskGroup(of: Void.self) { group in
      group.addTask { await client.run() }
      group.addTask {
        let rows = try await client.query(
          "SELECT ssl, current_setting('default_transaction_read_only') FROM pg_stat_ssl WHERE pid=pg_backend_pid()"
        )
        var observed = false
        for try await row in rows {
          let (ssl, readOnly) = try row.decode((Bool, String).self)
          #expect(ssl && readOnly == "on")
          observed = true
        }
        #expect(observed)
      }
      group.addTask {
        try await Task.sleep(for: .seconds(8))
        throw SchemaReadinessError.timeout
      }
      defer { group.cancelAll() }
      _ = try await group.next()
    }
  }
}
