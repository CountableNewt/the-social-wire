import Foundation
import Logging
import PostgresNIO
import Testing
@testable import ThinAppViewCore

@Suite("Verified content conditional publication")
struct ContentVerificationCASTests {
  @Test func sqliteProtectsNewerCIDAndAbsence() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent("verification-cas-\(UUID()).sqlite").path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "cas.test"))
    try await exercise(store: store, conditional: store)
  }

  @Test(.enabled(if: ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"] != nil))
  func postgresProtectsNewerCIDAndAbsence() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"])
    let logger = Logger(label: "verification-cas.pg")
    let pool = PostgresClient(configuration: try makePostgresConfig(from: url, logger: logger), backgroundLogger: logger)
    let running = Task { await pool.run() }
    do {
      let store = PostgresThinAppViewStore(pool: pool, logger: logger)
      try await exercise(store: store, conditional: store)
    } catch { running.cancel(); await running.value; throw error }
    running.cancel(); await running.value
  }

  private func exercise(store: any ThinAppViewStore, conditional: any ContentVerificationMetadataStoring) async throws {
    let uri = "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.document/" + UUID().uuidString
    let old = item(uri, cid: "old")
    let one = item(uri, cid: "one")
    let two = item(uri, cid: "two")
    #expect(try await conditional.upsertContentItem(old, expectedCID: "missing") == false)
    #expect(try await conditional.upsertContentItem(old, expectedCID: nil))
    #expect(try await conditional.upsertContentItem(one, expectedCID: nil) == false)
    async let first = conditional.upsertContentItem(one, expectedCID: "old")
    async let second = conditional.upsertContentItem(two, expectedCID: "old")
    let results = try await [first, second]
    #expect(results.filter { $0 }.count == 1)
    let winner = try #require(try await conditional.contentVerificationMetadata(uri: uri))
    #expect(winner.cid == (results[0] ? "one" : "two"))
    #expect(try await conditional.deleteContentItem(uri: uri, expectedCID: "old") == false)
    #expect(try await conditional.deleteContentItem(uri: uri, expectedCID: winner.cid))
    #expect(try await conditional.contentVerificationMetadata(uri: uri) == nil)
    #expect(try await store.fetchContentItem(uri: uri) == nil)
  }

  private func item(_ uri: String, cid: String) -> IndexedContentItem {
    IndexedContentItem(uri: uri, cid: cid, authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", collection: "site.standard.document",
      createdAt: Date(), indexedAt: Date(), publicationSite: "https://publisher.contentwire.net",
      render: ContentRenderFields(title: cid, publishedAt: "2026-11-05T00:00:00Z"), expiresAt: Date().addingTimeInterval(3600))
  }
}
