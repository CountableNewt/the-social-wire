import Foundation
import GRDB
import Logging
import Testing

@testable import ThinAppViewCore

@Suite("SQLite combined feed")
struct SQLiteCombinedFeedTests {
  private let viewer = "did:plc:combined-viewer"
  private let author = "did:web:skyreader.rss"

  @Test("Scope aliases preserve feed query boundaries, overlaps, and per-publication deduplication")
  func aliasesAndDuplicates() async throws {
    let path = temporaryPath()
    defer { removeDatabase(path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "combined-test"))
    let first = scope("first", site: "https://journal.test/feed?token=a")
    let overlap = scope("overlap", site: "https://journal.test/feed?token=a")
    let second = scope("second", site: "https://journal.test/feed?token=b")
    let at = PublicationUnreadScope(publicationId: "at", authorDid: author, publicationAtUri: "at://did:web:skyreader.rss/site.standard.publication/journal", publicationScopeAtUris: [], publicationSiteUrls: [])
    try await store.db.write { db in
      try insert(db, id: "new", timestamp: "2026-05-01T12:00:00.900000Z", site: "https://journal.test/feed?token=a#old", article: "https://journal.test/article")
      try insert(db, id: "duplicate", timestamp: "2026-05-01T12:00:00.800000Z", site: "https://journal.test/feed?token=a", article: "https://journal.test/article")
      try insert(db, id: "private-b", timestamp: "2026-05-01T12:00:00.700000Z", site: "https://journal.test/feed?token=b", article: "https://journal.test/second")
      try insert(db, id: "at-legacy", timestamp: "2026-05-01T12:00:00.600000Z", site: "at://did:web:skyreader.rss/com.standard.publication/journal", article: "https://journal.test/at")
      try insert(db, id: "unrelated", timestamp: "2026-05-01T12:00:00.950000Z", site: "https://unrelated.test/feed", article: "https://unrelated.test/article")
    }
    let page = try await store.listFeedEntries(viewerDid: viewer, scopes: [first, overlap, second, at], filter: .all, cursor: nil, limit: 20)
    #expect(page.entries.map(\.entryId) == [uri("new"), uri("new"), uri("private-b"), uri("at-legacy")])
    #expect(page.entries.map(\.publicationId) == ["first", "overlap", "second", "at"])
    #expect(page.cursor == nil)
    let privateOnly = try await store.listFeedEntries(viewerDid: viewer, scopes: [first, scope("missing", site: "https://missing.test/feed")], filter: .all, cursor: nil, limit: 20)
    #expect(privateOnly.entries.map(\.entryId) == [uri("new")])
  }

  @Test("Each overlapping publication keeps its floor and explicit marks outrank unread overrides")
  func readFloorsAndOverrides() async throws {
    let path = temporaryPath()
    defer { removeDatabase(path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "combined-test"))
    let broad = PublicationUnreadScope(publicationId: "broad", authorDid: author, publicationAtUri: nil, publicationScopeAtUris: [], publicationSiteUrls: [])
    let narrow = scope("narrow", site: "https://journal.test/feed")
    try await store.db.write { db in
      for id in ["floor", "override", "explicit", "new"] {
        try insert(db, id: id, timestamp: "2026-05-01T12:00:00.851000Z", site: "https://journal.test/feed", article: "https://journal.test/\(id)")
      }
      try insert(db, id: "nil-site", timestamp: "2026-05-01T12:00:00Z", site: nil, article: "https://journal.test/no-site")
      try db.execute(sql: "INSERT INTO appview_publication_read_floors(viewer_did,publication_id,read_floor_at,read_floor_uri,generation,updated_at) VALUES(?, 'broad', '2026-05-01T12:00:00.851000Z', NULL, 1, '2026-05-02T00:00:00Z')", arguments: [viewer])
      try db.execute(sql: "INSERT INTO read_marks(viewer_did,subject_uri,created_at) VALUES(?,?, '2026-05-02T00:00:00Z')", arguments: [viewer, uri("explicit")])
      for id in ["override", "explicit"] {
        try db.execute(sql: "INSERT INTO appview_unread_overrides(viewer_did,subject_uri,created_at) VALUES(?,?, '2026-05-02T00:00:00Z')", arguments: [viewer, uri(id)])
      }
    }
    for filter in [EntryListFilter.all, .unread, .read] {
      let actual = try await store.listFeedEntries(viewerDid: viewer, scopes: [broad, narrow], filter: filter, cursor: nil, limit: 100)
      let expected = try await reference(store, scopes: [broad, narrow], filter: filter, limit: 100)
      #expect(actual.entries.map { "\($0.publicationId ?? "")|\($0.entryId)" } == expected.map { "\($0.publicationId ?? "")|\($0.entryId)" })
    }
  }

  @Test("Combined cursor retains microseconds and equal-time URI ordering")
  func pagination() async throws {
    let path = temporaryPath()
    defer { removeDatabase(path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "combined-test"))
    let scopes = [scope("a", site: "https://a.test/feed"), scope("b", site: "https://b.test/feed")]
    try await store.db.write { db in
      for (id, time, site) in [("z", "2026-05-01T12:00:00.851002Z", "https://a.test/feed"), ("y", "2026-05-01T12:00:00.851001Z", "https://b.test/feed"), ("x", "2026-05-01T12:00:00.851001Z", "https://a.test/feed"), ("old", "2026-05-01T12:00:00Z", "https://b.test/feed")] {
        try insert(db, id: id, timestamp: time, site: site, article: "https://journal.test/\(id)")
      }
    }
    var cursor: String?
    var ids: [String] = []
    for _ in 0..<5 {
      let page = try await store.listFeedEntries(viewerDid: viewer, scopes: scopes, filter: .all, cursor: cursor, limit: 1)
      ids += page.entries.map(\.entryId)
      cursor = page.cursor
      if cursor == nil { break }
    }
    #expect(ids == ["z", "y", "x", "old"].map(uri))
    #expect(cursor == nil)
  }

  @Test("605 subscribed RSS scopes use one bounded feed scan for 23,000 imported entries")
  func realisticScopeCount() async throws {
    let path = temporaryPath()
    defer { removeDatabase(path) }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "combined-performance-test"))
    let scopes = (0..<605).map { scope("publication-\($0)", site: "https://feed\($0).test/rss") }
    try await store.db.write { db in
      let statement = try db.makeStatement(sql: """
        INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at)
        VALUES(?,'fixture',?,'rss',?,?,?,?, '2099-01-01T00:00:00Z')
        """)
      for index in 0..<23_000 {
        let time = String(format: "2026-05-01T12:00:00.%06dZ", 999999 - index)
        let render = "{\"title\":\"Entry \(index)\",\"publishedAt\":\"\(time)\",\"articleUrl\":\"https://article.test/\(index)\"}"
        try statement.execute(arguments: [uri(String(index)), author, time, time, "https://feed\(index % 605).test/rss", render])
      }
    }
    let start = Date()
    let reader: any ThinAppViewStore = store
    let page = try await reader.listFeedEntries(viewerDid: viewer, scopes: scopes, filter: .all, cursor: nil, limit: 50)
    let duration = Date().timeIntervalSince(start)
    #expect(page.entries.count == 50)
    #expect(page.entries.first?.entryId == uri("0"))
    #expect(page.entries.last?.entryId == uri("49"))
    #expect(page.cursor != nil)
    #expect(duration < 2, "Combined feed took \(duration)s for 605 scopes")

    try await store.db.write { db in
      let statement = try db.makeStatement(sql: """
        INSERT INTO appview_publication_read_floors
          (viewer_did,publication_id,read_floor_at,read_floor_uri,generation,updated_at)
        VALUES(?,?,'2026-05-02T00:00:00Z',NULL,1,'2026-05-02T00:00:00Z')
        """)
      for scope in scopes {
        try statement.execute(arguments: [viewer, scope.publicationId])
      }
    }
    let unreadStart = Date()
    let unread = try await reader.listFeedEntries(viewerDid: viewer, scopes: scopes, filter: .unread, cursor: nil, limit: 50)
    let unreadDuration = Date().timeIntervalSince(unreadStart)
    #expect(unread.entries.isEmpty)
    #expect(unread.cursor == nil)
    #expect(unreadDuration < 2, "Exhausted unread feed took \(unreadDuration)s for 605 scopes")
  }

  private func reference(_ store: SQLiteThinAppViewStore, scopes: [PublicationUnreadScope], filter: EntryListFilter, limit: Int) async throws -> [AppViewEntryListItem] {
    var entries: [AppViewEntryListItem] = []
    for scope in scopes {
      let boundary = filter == .all ? nil : try await store.readBoundary(viewerDid: viewer, publicationId: scope.publicationId)
      let page = try await store.listEntries(viewerDid: viewer, authorDid: scope.authorDid, publicationAtUri: scope.publicationAtUri, publicationScopeAtUris: scope.publicationScopeAtUris, publicationSiteUrls: scope.publicationSiteUrls, filter: filter, cursor: nil, limit: min(100, limit + 1), readBoundary: boundary)
      entries += page.entries.map { $0.withPublicationId(scope.publicationId) }
    }
    return Array(entries.sorted { $0.feedPositionAt == $1.feedPositionAt ? $0.entryId > $1.entryId : $0.feedPositionAt > $1.feedPositionAt }.prefix(limit))
  }

  private func scope(_ id: String, site: String) -> PublicationUnreadScope {
    .init(publicationId: id, authorDid: author, publicationAtUri: nil, publicationScopeAtUris: [], publicationSiteUrls: [site])
  }

  private func removeDatabase(_ path: String) {
    for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) }
  }

  private func temporaryPath() -> String { FileManager.default.temporaryDirectory.appendingPathComponent("combined-\(UUID()).sqlite").path }
  private func uri(_ id: String) -> String { "at://rss/entry/\(id)" }

  private func insert(_ database: Database, id: String, timestamp: String, site: String?, article: String) throws {
    let render = "{\"title\":\"\(id)\",\"publishedAt\":\"\(timestamp)\",\"articleUrl\":\"\(article)\"}"
    try database.execute(sql: """
      INSERT INTO content_items(uri,cid,author_did,collection,created_at,indexed_at,publication_site,render_json,expires_at)
      VALUES(?,'fixture',?,'rss',?,?,?,?, '2099-01-01T00:00:00Z')
      """, arguments: [uri(id), author, timestamp, timestamp, site, render])
  }
}
