import Foundation
import Testing

@testable import SchemaReadyCore

struct SchemaReadinessTests {
  @Test(.enabled(if: ProcessInfo.processInfo.environment["SCHEMA_READY_TEST_DATABASE_URL"] != nil))
  func requiresAnOlderMissingReceiptEvenWhenNewerReceiptsExist() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["SCHEMA_READY_TEST_DATABASE_URL"])
    try await SchemaReadiness.wait(url: url, versions: ["20260815190000"], timeout: .seconds(5))
    await #expect(throws: SchemaReadinessError.timeout) {
      try await SchemaReadiness.wait(
        url: url, versions: ["19990101000001", "20260815190000"], timeout: .milliseconds(150))
    }
  }

  @Test func refusesAnEmptyManifestBeforeConnecting() async {
    await #expect(throws: (any Error).self) {
      try await SchemaReadiness.wait(url: "postgres://unused@127.0.0.1/unused", versions: [])
    }
  }
}
