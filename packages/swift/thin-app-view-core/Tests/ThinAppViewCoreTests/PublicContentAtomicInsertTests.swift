import Foundation
import Logging
import PostgresNIO
import Testing
@testable import ThinAppViewCore

@Suite struct PublicContentAtomicInsertTests {
  @Test func sqliteProviderInsertCannotOverwriteConcurrentCanonicalContent() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer { try? FileManager.default.removeItem(atPath: path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "provider-insert.sqlite"))
    try await race(store: store)
  }

  @Test(.enabled(if: ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"] != nil))
  func postgresProviderInsertCannotOverwriteConcurrentCanonicalContent() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"])
    let logger = Logger(label: "provider-insert.postgres")
    let pool = PostgresClient(configuration: try makePostgresConfig(from: url, logger: logger), backgroundLogger: logger)
    let run = Task { await pool.run() }
    do { try await race(store: PostgresThinAppViewStore(pool: pool, logger: logger)) }
    catch { run.cancel(); await run.value; throw error }
    run.cancel(); await run.value
  }

  private func race(store: any ThinAppViewStore & PublicContentInsertStoring) async throws {
    let uri = "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.document/" + UUID().uuidString.lowercased()
    let at = Date()
    func item(title: String) -> IndexedContentItem {
      .init(uri: uri, cid: title, authorDid: "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", collection: "site.standard.document",
        createdAt: at, indexedAt: at, publicationSite: "https://publication.example",
        render: .init(title: title, publishedAt: "2026-11-05T12:00:00Z"), expiresAt: at.addingTimeInterval(86400))
    }
    let provider = item(title: "Provider"), canonical = item(title: "Canonical")
    try await withThrowingTaskGroup(of: Int.self) { group in
      for _ in 0..<16 { group.addTask { try await store.insertContentItemIfAbsent(provider) ? 1 : 0 } }
      group.addTask { try await store.upsertContentItem(canonical); return 0 }
      var inserted = 0
      for try await count in group { inserted += count }
      #expect(inserted <= 1)
    }
    #expect(try await store.fetchContentIdentity(uri: uri)?.cid == "Canonical")
    #expect(try await !store.insertContentItemIfAbsent(provider))
    #expect(try await store.fetchContentItem(uri: uri)?.title == "Canonical")
  }
}
