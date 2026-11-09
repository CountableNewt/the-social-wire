import Foundation
import Logging
import PostgresNIO
import Testing
import ThinAppViewCore

@Suite(.enabled(if: ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"] != nil))
struct FrozenReadMarksPostgresTests {
  @Test func freezesActionsAndSerializesAuthorityTransitions() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["THIN_APPVIEW_TEST_DATABASE_URL"])
    let logger = Logger(label: "frozen-read.postgres")
    let pool = PostgresClient(configuration: try makePostgresConfig(from: url, logger: logger), backgroundLogger: logger)
    let run = Task { await pool.run() }
    do {
      let store = PostgresThinAppViewStore(pool: pool, logger: logger)
      let viewer = "did:plc:frozen-" + UUID().uuidString.lowercased()
      let at = Date(timeIntervalSince1970: 1_700_000_000)
      try await store.markEntryUnread(viewerDid: viewer, subjectUri: "newer", createdAt: at.addingTimeInterval(1))
      try await store.markEntryUnread(viewerDid: viewer, subjectUri: "older", createdAt: at.addingTimeInterval(-1))
      #expect(try await store.applyFrozenReadMarks(viewerDid: viewer,
        subjectUris: ["newer", "older", "older"], createdAt: at) == ["older"])
      #expect(try await !store.hasReadMark(viewerDid: viewer, subjectUri: "newer"))
      try await pool.query("""
        UPDATE appview_pds_read_state_authority SET manifest='{}'::jsonb,manifest_cid='fixture'
        WHERE viewer_did=\(viewer)
        """, logger: logger)
      await #expect(throws: PDSReadStateStorageError.alreadyMigrated) {
        try await store.applyFrozenReadMarks(viewerDid: viewer, subjectUris: ["forbidden"], createdAt: at)
      }
      await #expect(throws: PDSReadStateStorageError.alreadyMigrated) {
        try await store.upsertReadMark(viewerDid: viewer, subjectUri: "forbidden", createdAt: at)
      }
      await #expect(throws: PDSReadStateStorageError.alreadyMigrated) {
        try await store.markEntryUnread(viewerDid: viewer, subjectUri: "older", createdAt: at)
      }
      let forbidden = try await pool.query("SELECT COUNT(*) FROM read_marks WHERE viewer_did=\(viewer) AND subject_uri='forbidden'", logger: logger)
      for try await row in forbidden { #expect(try row.decode(Int.self) == 0) }
    } catch {
      run.cancel(); await run.value; throw error
    }
    run.cancel(); await run.value
  }
}
