import Foundation
import GRDB
import Logging
import ReadStateCore
import Testing

@testable import ThinAppViewCore

struct SQLitePDSReadStateTests {
  @Test func authoritativeStateSurvivesRestartAndLateBackfill() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let logger = Logger(label: "sqlite-pds-test")
    var store = try SQLiteThinAppViewStore(path: path, logger: logger)
    let viewer = "did:plc:testviewer"
    let author = "did:plc:testauthor"
    let publication = "at://\(author)/site.standard.publication/main"
    let at = try ReadStateValidation.date("2026-09-08T12:00:00Z")
    let uri = "at://\(author)/site.standard.document/a"
    func item(_ id: String) -> IndexedContentItem {
      IndexedContentItem(
        uri: id, cid: "cid", authorDid: author, collection: "site.standard.document",
        createdAt: at, indexedAt: at, publicationSite: publication,
        render: ContentRenderFields(title: id, publishedAt: "2026-09-08T12:00:00Z"),
        expiresAt: Date().addingTimeInterval(86400))
    }
    try await store.upsertContentItem(item(uri))
    let empty = ReadStateManifest(generation: "empty", lastSequence: 0, head: nil)
    _ = try await store.activatePDSReadState(
      viewerDid: viewer, manifest: empty, manifestCid: "empty",
      projection: ReadStateProjection(operations: [], lastSequence: 0), expectedLegacyRevision: 0)
    let boundary = ReadStateBoundary(
      scope: ReadStateScope(
        publicationId: publication, authorDid: author, publicationSiteKeys: [publication]),
      createdAt: "2026-09-09T00:00:00Z", entryId: nil)
    let read = ReadStateOperation(
      actionId: "read-all", sequence: 1, state: .read, actedAt: "2026-09-09T00:00:00Z",
      boundaries: [boundary])
    let unread = ReadStateOperation(
      actionId: "unread-one", sequence: 2, state: .unread, actedAt: "2026-09-09T00:00:01Z",
      subjectUris: [uri])
    let manifest = ReadStateManifest(
      generation: "next", lastSequence: 2,
      head: ReadStateReference(
        uri: "at://\(viewer)/app.thesocialwire.readStateChunk/one", cid: "chunk"))
    _ = try await store.activatePDSReadState(
      viewerDid: viewer, manifest: manifest, manifestCid: "next",
      projection: ReadStateProjection(operations: [read, unread], lastSequence: 2),
      expectedLegacyRevision: nil)
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: uri) == false)
    let late = "at://\(author)/site.standard.document/late"
    try await store.upsertContentItem(item(late))
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: late))
    #expect(
      try await store.countUnreadEntries(
        viewerDid: viewer, authorDid: author, publicationAtUri: publication,
        publicationScopeAtUris: [publication], publicationSiteUrls: []) == 1)
    await #expect(throws: (any Error).self) {
      try await store.upsertReadMark(viewerDid: viewer, subjectUri: uri, createdAt: at)
    }
    store = try SQLiteThinAppViewStore(path: path, logger: logger)
    #expect(try await store.pdsReadStateStatus(viewerDid: viewer).manifestCid == "next")
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: late))
    try await store.deleteContentItem(uri: late)
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: late) == false)
    // Re-indexing old content still reapplies the retained canonical boundary.
    try await store.upsertContentItem(item(late))
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: late))
  }

  @Test func baselineExportFencesConcurrentLegacyChanges() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "sqlite-pds-test"))
    let viewer = "did:plc:testviewer"
    let at = Date()
    try await store.upsertReadMark(viewerDid: viewer, subjectUri: "one", createdAt: at)
    try await store.upsertReadMark(viewerDid: viewer, subjectUri: "two", createdAt: at)
    let first = try await store.exportPDSReadStatePage(
      viewerDid: viewer, cursor: nil, expectedLegacyRevision: nil, limit: 1)
    #expect(first.rows.count == 1)
    try await store.markEntryUnread(viewerDid: viewer, subjectUri: "one", createdAt: at)
    await #expect(throws: PDSReadStateStorageError.revisionChanged) {
      try await store.exportPDSReadStatePage(
        viewerDid: viewer, cursor: first.cursor, expectedLegacyRevision: first.legacyRevision,
        limit: 1)
    }
  }
  @Test func migratedLegacyBaselineRetainsScopedUnreadAndRecoversEviction() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "sqlite-baseline"))
    let viewer = "did:plc:viewer"
    let author = "did:plc:author"
    let publication = "at://\(author)/site.standard.publication/main"
    let scope = PublicationUnreadScope(
      publicationId: publication, authorDid: author, publicationAtUri: publication,
      publicationScopeAtUris: [publication], publicationSiteUrls: [])
    let at = try ReadStateValidation.date("2026-09-08T12:00:00Z")
    try await store.upsertPublicationScopes([
      AppViewUnreadCounterSupport.publicationScope(
        viewerDid: viewer, publicationId: publication, authorDid: author,
        publicationAtUri: publication, publicationScopeAtUris: [publication],
        publicationSiteUrls: [], sectionKeys: [])
    ])
    let ids = ["a", "b"].map { "at://\(author)/site.standard.document/\($0)" }
    for uri in ids {
      try await store.upsertContentItem(
        IndexedContentItem(
          uri: uri, cid: "cid", authorDid: author, collection: "site.standard.document",
          createdAt: at, indexedAt: at, publicationSite: publication,
          render: ContentRenderFields(title: uri, publishedAt: "2026-09-08T12:00:00Z"),
          expiresAt: Date().addingTimeInterval(86400)))
    }
    _ = try await store.markAllReadCounters(viewerDid: viewer, scopes: [scope], readAt: at)
    try await store.markEntryUnread(
      viewerDid: viewer, subjectUri: ids[0], createdAt: at.addingTimeInterval(1))
    let page = try await store.exportPDSReadStatePage(
      viewerDid: viewer, cursor: nil, expectedLegacyRevision: nil, limit: 100)
    #expect(page.rows.map(\.kind) == [.boundary, .unread])
    let operations = try ReadStateMigrationPlanner.operations(
      rows: page.rows, migrationId: "fixture")
    let manifest = ReadStateManifest(
      generation: "baseline", lastSequence: Int64(operations.count),
      head: ReadStateReference(
        uri: "at://\(viewer)/app.thesocialwire.readStateChunk/first", cid: "chunk"))
    let projection = try ReadStateProjection(
      operations: operations, lastSequence: Int64(operations.count))
    _ = try await store.activatePDSReadState(
      viewerDid: viewer, manifest: manifest, manifestCid: "first", projection: projection,
      expectedLegacyRevision: page.legacyRevision)
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: ids[0]) == false)
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: ids[1]))
    let unread = try await store.listEntries(
      viewerDid: viewer, authorDid: author, publicationAtUri: publication,
      publicationScopeAtUris: [publication], publicationSiteUrls: [], filter: .unread, cursor: nil,
      limit: 100, readBoundary: nil)
    #expect(unread.entries.map(\.entryId) == [ids[0]])
    let counts = try await store.refreshUnreadCounters(viewerDid: viewer, scopes: [scope])
    #expect(counts.first?.unreadCount == 1)
    #expect(
      try await store.deleteExpiredReadMarks(
        before: Date().addingTimeInterval(86400), batchSize: 100) == 0)
    let evicted = try await store.evictIdlePDSReadState(
      before: Date().addingTimeInterval(86400), at: Date(), batchSize: 1)
    #expect(evicted.viewerDid == viewer)
    #expect(try await store.pdsReadStateStatus(viewerDid: viewer).projectionReady == false)
    _ = try await store.activatePDSReadState(
      viewerDid: viewer, manifest: manifest, manifestCid: "first", projection: projection,
      expectedLegacyRevision: nil)
    #expect(try await store.pdsReadStateStatus(viewerDid: viewer).projectionReady)
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: ids[1]))
  }

  @Test func boundariesPreserveMicrosecondsAndScopeChangesFenceExports() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "sqlite-microseconds"))
    let viewer = "did:plc:viewer"
    let author = "did:plc:author"
    let publication = "at://\(author)/site.standard.publication/main"
    let before = try await store.exportPDSReadStatePage(
      viewerDid: viewer, cursor: nil, expectedLegacyRevision: nil, limit: 100)
    let scope = AppViewUnreadCounterSupport.publicationScope(
      viewerDid: viewer, publicationId: publication, authorDid: author,
      publicationAtUri: publication, publicationScopeAtUris: [publication], publicationSiteUrls: [],
      sectionKeys: [])
    try await store.upsertPublicationScopes([scope])
    #expect(
      try await store.pdsReadStateStatus(viewerDid: viewer).legacyRevision > before.legacyRevision)
    let cutoff = "2026-09-08T12:00:00.123456Z"
    let ids = ["before", "after"].map { "at://\(author)/site.standard.document/\($0)" }
    for (index, uri) in ids.enumerated() {
      let date = try ReadStateValidation.date(
        index == 0 ? "2026-09-08T12:00:00.123455Z" : "2026-09-08T12:00:00.123457Z")
      try await store.upsertContentItem(
        IndexedContentItem(
          uri: uri, cid: "cid", authorDid: author, collection: "site.standard.document",
          createdAt: date, indexedAt: date, publicationSite: publication,
          render: ContentRenderFields(title: uri, publishedAt: cutoff),
          expiresAt: Date().addingTimeInterval(86400)))
    }
    let boundary = ReadStateBoundary(
      scope: ReadStateScope(
        publicationId: publication, authorDid: author, publicationSiteKeys: [publication]),
      createdAt: cutoff, entryId: nil)
    #expect(
      try await store.previewPDSReadStateBoundaries(
        viewerDid: viewer, boundaries: [boundary], subjectUris: ids) == [ids[0]])
    let empty = ReadStateManifest(generation: "empty", lastSequence: 0, head: nil)
    let status = try await store.pdsReadStateStatus(viewerDid: viewer)
    _ = try await store.activatePDSReadState(
      viewerDid: viewer, manifest: empty, manifestCid: "empty",
      projection: ReadStateProjection(operations: [], lastSequence: 0),
      expectedLegacyRevision: status.legacyRevision)
    let read = ReadStateOperation(
      actionId: "microsecond-read", sequence: 1, state: .read, actedAt: cutoff,
      boundaries: [boundary])
    let manifest = ReadStateManifest(
      generation: "next", lastSequence: 1,
      head: ReadStateReference(
        uri: "at://\(viewer)/app.thesocialwire.readStateChunk/next", cid: "chunk"))
    _ = try await store.activatePDSReadState(
      viewerDid: viewer, manifest: manifest, manifestCid: "next",
      projection: ReadStateProjection(operations: [read], lastSequence: 1),
      expectedLegacyRevision: nil)
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: ids[0]))
    #expect(try await store.hasReadMark(viewerDid: viewer, subjectUri: ids[1]) == false)
    try await store.upsertPublicationScopes([scope])
    #expect(
      try await store.pdsReadStateStatus(viewerDid: viewer).legacyRevision == status.legacyRevision)
  }

  @Test func importerProjectionWriterScopeSurvivesContentAndLegacyRows() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).path
    defer {
      for suffix in ["", "-wal", "-shm"] {
        try? FileManager.default.removeItem(atPath: path + suffix)
      }
    }
    let store = try SQLiteThinAppViewStore(
      path: path, logger: Logger(label: "sqlite-import-writer"))
    let active = try await store.db.write { database in
      try database.execute(sql: "UPDATE appview_pds_projection_write SET active=1")
      try database.execute(
        sql:
          "INSERT INTO appview_publication_scopes VALUES('did:plc:owner','publication','did:plc:author',NULL,'[]','[]','[]','[]','2026-11-07T00:00:00Z')"
      )
      try database.execute(
        sql:
          "INSERT INTO content_items VALUES('uri','cid','did:plc:author','site.standard.document','2026-11-07T00:00:00Z','2026-11-07T00:00:00Z',NULL,'{}','2099-01-01T00:00:00Z')"
      )
      try database.execute(sql: "UPDATE content_items SET cid='next' WHERE uri='uri'")
      try database.execute(
        sql: "INSERT INTO read_marks VALUES('did:plc:owner','uri','2026-11-07T00:00:00Z')")
      // No trigger may synthesize an authority row ahead of the source import.
      let authorityCount = try Int.fetchOne(
        database, sql: "SELECT count(*) FROM appview_pds_read_state_authority")
      guard authorityCount == 0 else { throw PDSReadStateStorageError.revisionChanged }
      try database.execute(
        sql:
          "INSERT INTO appview_pds_read_state_authority(viewer_did,legacy_revision) VALUES('did:plc:owner',7)"
      )
      let active = try Int.fetchOne(
        database, sql: "SELECT active FROM appview_pds_projection_write")
      try database.execute(sql: "UPDATE appview_pds_projection_write SET active=0")
      return active
    }
    #expect(active == 1)
    #expect(try await store.pdsReadStateStatus(viewerDid: "did:plc:owner").legacyRevision == 7)
  }

}
