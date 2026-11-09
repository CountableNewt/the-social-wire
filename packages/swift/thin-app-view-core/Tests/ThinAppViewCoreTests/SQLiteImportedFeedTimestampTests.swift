import Foundation
import GRDB
import Logging
import ReadStateCore
import Testing

@testable import ThinAppViewCore

@Suite("Imported SQLite feed timestamps")
struct SQLiteImportedFeedTimestampTests {
  @Test("Feed cursors retain imported microseconds and accept legacy whole seconds")
  func cursorPrecision() throws {
    for raw in ["2026-04-23T09:18:21.851001Z", "2026-04-23T09:18:21.851002Z", "2026-04-23T09:18:21.1Z", "2026-04-23T09:18:21.12345Z", "2026-04-23T09:18:21Z"] {
      let date = try ReadStateValidation.date(raw)
      let cursor = ThinAppViewCursor.encode(createdAt: date, uri: "at://example/entry/one")
      let decoded = try #require(ThinAppViewCursor.decode(cursor))
      #expect(abs(decoded.createdAt.timeIntervalSince(date)) < 0.0000005)
    }
    #expect(ThinAppViewCursor.decode("2026-04-23T09:18:21Z|legacy")?.uri == "legacy")
  }

  @Test("Sparse RSS scans continue across batches and same-second imported entries")
  func sparseImportedPagination() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent("imported-feed-\(UUID()).sqlite").path
    defer {
      for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) }
    }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "imported-feed-test"))
    let target = "https://target.example/feed"
    let targets: Set<Int> = [120, 121, 240, 241]
    try await store.db.write { db in
      for index in 0..<350 {
        // Match the importer's fixed six-digit UTC representation, including
        // distinct PostgreSQL microseconds within the same second.
        let timestamp = String(format: "2026-04-23T09:18:21.%06dZ", 900000 - index)
        try db.execute(sql: """
          INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at)
          VALUES(?, 'fixture', 'did:web:skyreader.rss', 'rss', ?, ?, ?, ?, '2099-01-01T00:00:00Z')
          """, arguments: ["at://rss/entry/\(index)", timestamp, timestamp,
                            targets.contains(index) ? target : "https://noise.example/feed",
                            "{\"title\":\"Entry \(index)\",\"publishedAt\":\"\(timestamp)\"}"])
      }
      // Existing local databases also contain whole-second rows.
      try db.execute(sql: """
        INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at)
        VALUES('at://rss/entry/legacy','fixture','did:web:skyreader.rss','rss','2026-04-23T09:18:21Z','2026-04-23T09:18:21Z',?,'{"title":"Legacy","publishedAt":"2026-04-23T09:18:21Z"}','2099-01-01T00:00:00Z')
        """, arguments: [target])
    }
    var cursor: String?
    var ids: [String] = []
    for _ in 0..<6 {
      let page = try await store.listEntries(viewerDid: "did:plc:viewer", authorDid: "did:web:skyreader.rss",
        publicationAtUri: nil, publicationScopeAtUris: [], publicationSiteUrls: [target],
        filter: .all, cursor: cursor, limit: 1)
      ids.append(contentsOf: page.entries.map(\.entryId))
      cursor = page.cursor
      if cursor == nil { break }
    }
    #expect(ids == [120, 121, 240, 241].map { "at://rss/entry/\($0)" } + ["at://rss/entry/legacy"])
    #expect(cursor == nil)

    let plan = try await store.db.read { db in
      try Row.fetchAll(db, sql: """
        EXPLAIN QUERY PLAN SELECT uri,render_json,created_at,publication_site FROM content_items INDEXED BY idx_content_items_author_timestamp_uri
        WHERE author_did='did:web:skyreader.rss' AND expires_at>'2026-01-01T00:00:00Z'
        ORDER BY \(SQLiteThinAppViewStore.feedTimestampSQL("created_at")) DESC,uri DESC LIMIT 100
        """).map { (row: Row) -> String in row["detail"] }
    }
    #expect(plan.contains { $0.contains("idx_content_items_author_timestamp_uri") })
    #expect(!plan.contains { $0.contains("TEMP B-TREE") })
    let scopedPlan = try await store.db.read { db in
      try Row.fetchAll(db, sql: """
        EXPLAIN QUERY PLAN SELECT uri,render_json,created_at,publication_site
        FROM content_items INDEXED BY idx_content_items_author_site_timestamp_uri
        WHERE author_did='did:web:skyreader.rss' AND publication_site=?
          AND expires_at>'2026-01-01T00:00:00Z'
        ORDER BY \(SQLiteThinAppViewStore.feedTimestampSQL("created_at")) DESC,uri DESC LIMIT 100
        """, arguments: [target]).map { (row: Row) -> String in row["detail"] }
    }
    #expect(!scopedPlan.contains { $0.contains("TEMP B-TREE") })
  }

  @Test("Built-in timestamp keys retain fractional widths, timezone offsets, and second boundaries")
  func canonicalIndexKeys() throws {
    let database = try DatabaseQueue()
    let expression = SQLiteThinAppViewStore.feedTimestampSQL("created_at")
    for (raw, expected) in [
      ("2026-04-23T09:18:21Z", "2026-04-23T09:18:21.000000000Z"),
      ("2026-04-23T09:18:21.851Z", "2026-04-23T09:18:21.851000000Z"),
      ("2026-04-23T09:18:21.851001Z", "2026-04-23T09:18:21.851001000Z"),
      ("2026-04-23T09:18:21.999999Z", "2026-04-23T09:18:21.999999000Z"),
      ("2026-04-23T11:18:21.123456+02:00", "2026-04-23T09:18:21.123456000Z"),
      ("2026-04-23T09:18:21.123456789Z", "2026-04-23T09:18:21.123456789Z"),
    ] {
      let actual = try database.read { db in
        try String.fetchOne(db, sql: "SELECT \(expression) FROM (SELECT ? AS created_at)", arguments: [raw])
      }
      #expect(actual == expected)
    }
  }

  @Test("Sibling connections can migrate a database containing feed indexes")
  func sharedDatabaseSchemaChanges() throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent("shared-index-\(UUID()).sqlite").path
    defer {
      for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) }
    }
    _ = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "shared-index-test"))
    // OperationsCore and other stores open independent connections. Their
    // schema migrations must not require registering a private collation.
    let plain = try DatabaseQueue(path: path)
    try plain.write { db in
      try db.execute(sql: "CREATE TABLE sibling_service(value TEXT)")
      try db.execute(sql: "ALTER TABLE sibling_service RENAME TO migrated_sibling_service")
      #expect(try String.fetchOne(db, sql: "PRAGMA quick_check") == "ok")
    }
  }

}
