import Foundation
import GRDB
import Logging
import ReadStateCore
import Testing

@testable import ThinAppViewCore

@Suite("SQLite selector overlap read state")
struct SQLiteFeedSelectorOverlapTests {
  @Test("Shared entry IDs retain each publication's own read floor without trapping")
  func publicationFlags() async throws {
    let path = FileManager.default.temporaryDirectory.appendingPathComponent("selector-overlap-\(UUID()).sqlite").path
    defer {
      for suffix in ["", "-wal", "-shm"] { try? FileManager.default.removeItem(atPath: path + suffix) }
    }
    let store = try SQLiteThinAppViewStore(path: path, logger: Logger(label: "selector-overlap-test"))
    let viewer = "did:plc:viewer", author = "did:web:skyreader.rss", site = "https://overlap.example/feed"
    let at = try ReadStateValidation.date("2026-04-23T09:18:21.851001Z")
    let scopes = ["read-publication", "unread-publication"].map {
      AppViewUnreadCounterSupport.publicationScope(viewerDid: viewer, publicationId: $0,
        authorDid: author, publicationAtUri: nil, publicationScopeAtUris: [],
        publicationSiteUrls: [site], sectionKeys: [], updatedAt: at)
    }
    try await store.replaceViewerFeedProjection(viewerDid: viewer, scopes: scopes,
      feeds: [.init(viewerDid: viewer, kind: .subscribed, feedId: "", updatedAt: at)],
      memberships: scopes.map { .init(viewerDid: viewer, kind: .subscribed, feedId: "", publicationId: $0.publicationId) })
    let uri = "at://rss/entry/shared"
    try await store.upsertContentItem(.init(uri: uri, cid: "fixture", authorDid: author, collection: "rss",
      createdAt: at, indexedAt: at, publicationSite: site,
      render: .init(title: "Shared", publishedAt: "2026-04-23T09:18:21.851001Z"),
      expiresAt: Date().addingTimeInterval(3600)))
    try await store.db.write { db in
      try db.execute(sql: """
        INSERT INTO appview_publication_read_floors(viewer_did,publication_id,read_floor_at,read_floor_uri,generation,updated_at)
        VALUES(?, 'read-publication', '2026-04-23T09:18:21.851001Z', NULL, 1, '2026-04-23T09:18:21Z')
        """, arguments: [viewer])
    }
    let reader: any ThinAppViewStore = store
    let selector = AppViewFeedSelector(kind: .subscribed)
    let all = try #require(try await reader.listFeedEntries(viewerDid: viewer, selector: selector, filter: .all, cursor: nil, limit: 10))
    #expect(all.response.entries.count == 2)
    #expect(all.response.entries.filter(\.isRead).map(\.publicationId) == ["read-publication"])
    #expect(all.response.entries.filter { !$0.isRead }.map(\.publicationId) == ["unread-publication"])
    for filter in [EntryListFilter.read, .unread] {
      let page = try #require(try await reader.listFeedEntries(viewerDid: viewer, selector: selector, filter: filter, cursor: nil, limit: 10))
      #expect(page.response.entries.count == 1)
      #expect(page.response.entries.first?.isRead == (filter == .read))
    }
    await #expect(throws: ThinAppViewStoreError.self) {
      try await store.readStates(viewerDid: viewer, entries: all.response.entries)
    }
    let read = try #require(all.response.entries.first { $0.isRead })
    #expect(try await store.readStates(viewerDid: viewer, entries: [read, read]) == [uri: true])
  }
}
